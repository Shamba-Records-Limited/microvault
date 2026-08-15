package stellaranchor

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"
)

// Config bundles the SEP-1 derived values plus the custodial treasury secret
// needed to talk to a Stellar anchor. Fetch the TOML at startup via
// FetchTOML, validate it via TOML.Validate, then pass the relevant fields
// here — surfacing rotation as an explicit re-Validate at boot rather than
// silent runtime drift.
type Config struct {
	HomeDomain        string // e.g. "stellar.moneygram.com"
	WebAuthEndpoint   string // SEP-1 WEB_AUTH_ENDPOINT
	TransferServerURL string // SEP-1 TRANSFER_SERVER_SEP0024
	ServerSigningKey  string // SEP-1 SIGNING_KEY
	NetworkPassphrase string // SEP-1 NETWORK_PASSPHRASE
	USDCIssuer        string // expected USDC issuer (validated against TOML upstream)
	TreasurySecret    string // S... seed of the custodial treasury account

	HTTPClient *http.Client
	Logger     *slog.Logger

	// JWTSafetyMargin tunes the JWT cache. Defaults to 60s.
	JWTSafetyMargin time.Duration
}

// Client is the top-level Stellar anchor handle: SEP-10 auth + SEP-24 anchor
// + JWT cache, glued together. Anchor-specific SDKs (e.g. moneygram) compose
// this client with their own REST/OAuth/FX extras.
type Client struct {
	Auth     *AuthClient
	Anchor   *AnchorClient
	JWTCache *JWTCache

	cfg Config
}

// New wires the sub-clients. Returns an error if AuthClient or AnchorClient
// validation fails.
func New(cfg Config) (*Client, error) {
	if cfg.JWTSafetyMargin <= 0 {
		cfg.JWTSafetyMargin = 60 * time.Second
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}

	auth, err := NewAuthClient(AuthConfig{
		WebAuthEndpoint:   cfg.WebAuthEndpoint,
		ServerSigningKey:  cfg.ServerSigningKey,
		NetworkPassphrase: cfg.NetworkPassphrase,
		HomeDomain:        cfg.HomeDomain,
		TreasurySecret:    cfg.TreasurySecret,
	}, cfg.HTTPClient, cfg.Logger)
	if err != nil {
		return nil, err
	}

	anchor, err := NewAnchorClient(AnchorConfig{TransferServerURL: cfg.TransferServerURL}, cfg.HTTPClient, cfg.Logger)
	if err != nil {
		return nil, err
	}

	return &Client{
		Auth:     auth,
		Anchor:   anchor,
		JWTCache: NewJWTCache(auth, cfg.JWTSafetyMargin),
		cfg:      cfg,
	}, nil
}

// TreasuryAddress returns the custodial G... account ID.
func (c *Client) TreasuryAddress() string {
	return c.Auth.TreasuryAddress()
}

// USDCIssuer returns the USDC issuer pubkey from Config.
func (c *Client) USDCIssuer() string {
	return c.cfg.USDCIssuer
}

// Token returns a cached SEP-10 JWT for the given child memo, refreshing
// when stale.
func (c *Client) Token(ctx context.Context, childMemo int64) (string, error) {
	return c.JWTCache.Get(ctx, childMemo)
}

// InitiateWithdrawal fetches a JWT for childMemo and calls
// Anchor.InitiateWithdrawal. On a 401 it evicts the cached token and
// retries once.
func (c *Client) InitiateWithdrawal(ctx context.Context, childMemo int64, req WithdrawRequest) (*WithdrawResponse, error) {
	jwt, err := c.JWTCache.Get(ctx, childMemo)
	if err != nil {
		return nil, err
	}

	resp, err := c.Anchor.InitiateWithdrawal(ctx, jwt, req)
	if isUnauthorized(err) {
		c.JWTCache.Invalidate(childMemo)
		jwt, err = c.JWTCache.Get(ctx, childMemo)
		if err != nil {
			return nil, err
		}
		return c.Anchor.InitiateWithdrawal(ctx, jwt, req)
	}
	return resp, err
}

// GetTransaction wraps Anchor.GetTransaction with the same JWT cache and
// 401-retry semantics as InitiateWithdrawal.
func (c *Client) GetTransaction(ctx context.Context, childMemo int64, txID string) (*Transaction, error) {
	jwt, err := c.JWTCache.Get(ctx, childMemo)
	if err != nil {
		return nil, err
	}

	tx, err := c.Anchor.GetTransaction(ctx, jwt, txID)
	if isUnauthorized(err) {
		c.JWTCache.Invalidate(childMemo)
		jwt, err = c.JWTCache.Get(ctx, childMemo)
		if err != nil {
			return nil, err
		}
		return c.Anchor.GetTransaction(ctx, jwt, txID)
	}
	return tx, err
}

func isUnauthorized(err error) bool {
	return errors.Is(err, ErrUnauthorized)
}
