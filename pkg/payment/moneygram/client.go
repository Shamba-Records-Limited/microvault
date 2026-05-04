package moneygram

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"
)

// Config bundles everything required to construct a Client: TOML-derived
// values, treasury secret, and optional REST credentials for the FX Rate API.
//
// Fetch the TOML at startup (FetchTOML), validate it (TOML.Validate), and
// pass the relevant fields here. Doing it that way means TOML rotation
// surfaces as an explicit re-Validate at boot, not as a silent runtime drift.
type Config struct {
	// HomeDomain — e.g. "stellar.moneygram.com".
	HomeDomain string

	// SEP-1 derived. Fill from the validated TOML.
	WebAuthEndpoint   string
	TransferServerURL string
	ServerSigningKey  string
	NetworkPassphrase string
	USDCIssuer        string

	// Custodial treasury seed (S...). Required.
	TreasurySecret string

	// Optional REST API credentials for the FX Rate endpoint. Leave zero-
	// valued to skip — Client.FXRate will be nil and callers must source
	// rates elsewhere.
	REST RESTConfig

	// HTTPClient is shared across SEP-10 / SEP-24 / OAuth / FXRate when
	// non-nil. If nil, each sub-client builds its own with a 15s timeout.
	HTTPClient *http.Client

	// Logger is shared across sub-clients when non-nil.
	Logger *slog.Logger

	// JWTSafetyMargin tunes the JWT cache. Defaults to 60s.
	JWTSafetyMargin time.Duration

	// FXRateCacheTTL is forwarded to FXRateClient. Defaults to 60s.
	FXRateCacheTTL time.Duration
}

// RESTConfig holds the REST API credentials separate from the SEP-10 chain.
// Zero-value disables FX Rate wiring.
type RESTConfig struct {
	BaseURL       string // FX Rate base, e.g. https://api-uat.moneygram.com
	OAuthTokenURL string
	ClientID      string
	ClientSecret  string
	Scope         string
}

// Client is the top-level MoneyGram SDK handle. Sub-clients are exported so
// consumers can reach in for advanced cases; the convenience methods on
// Client cover the common cash-pickup off-ramp flow.
type Client struct {
	Auth     *AuthClient
	JWTCache *JWTCache
	Anchor   *AnchorClient
	OAuth    *OAuthClient   // nil when REST credentials not provided
	FXRate   *FXRateClient  // nil when REST credentials not provided

	cfg Config
}

// New wires every sub-client.
func New(cfg Config) (*Client, error) {
	if cfg.JWTSafetyMargin <= 0 {
		cfg.JWTSafetyMargin = 60 * time.Second
	}
	if cfg.FXRateCacheTTL <= 0 {
		cfg.FXRateCacheTTL = 60 * time.Second
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

	c := &Client{
		Auth:     auth,
		JWTCache: NewJWTCache(auth, cfg.JWTSafetyMargin),
		Anchor:   anchor,
		cfg:      cfg,
	}

	if restConfigured(cfg.REST) {
		oauth, err := NewOAuthClient(OAuthConfig{
			TokenURL:     cfg.REST.OAuthTokenURL,
			ClientID:     cfg.REST.ClientID,
			ClientSecret: cfg.REST.ClientSecret,
			Scope:        cfg.REST.Scope,
		}, cfg.HTTPClient, cfg.Logger)
		if err != nil {
			return nil, err
		}
		fx, err := NewFXRateClient(FXRateConfig{
			BaseURL:  cfg.REST.BaseURL,
			CacheTTL: cfg.FXRateCacheTTL,
		}, oauth, cfg.HTTPClient, cfg.Logger)
		if err != nil {
			return nil, err
		}
		c.OAuth = oauth
		c.FXRate = fx
	}

	return c, nil
}

// TreasuryAddress returns the custodial G... account ID. Use this as the
// SEP-10 account, the source of withdrawal Stellar payments, and the
// Stellar account that needs the USDC trustline.
func (c *Client) TreasuryAddress() string {
	return c.Auth.TreasuryAddress()
}

// USDCIssuer returns the Circle USDC issuer pubkey from Config — useful when
// constructing Stellar Payment operations that need an Asset.
func (c *Client) USDCIssuer() string {
	return c.cfg.USDCIssuer
}

// Token returns a cached SEP-10 JWT for the given child memo, refreshing if
// stale. Equivalent to JWTCache.Get.
func (c *Client) Token(ctx context.Context, childMemo int64) (string, error) {
	return c.JWTCache.Get(ctx, childMemo)
}

// InitiateWithdrawal is a convenience wrapper that fetches a JWT for
// childMemo and calls Anchor.InitiateWithdrawal. On a 401 it evicts the
// cached token and retries once.
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

// GetTransaction is a convenience wrapper around Anchor.GetTransaction with
// the same JWT cache semantics as InitiateWithdrawal.
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

func restConfigured(c RESTConfig) bool {
	return c.BaseURL != "" && c.OAuthTokenURL != "" && c.ClientID != "" && c.ClientSecret != ""
}

func isUnauthorized(err error) bool {
	return errors.Is(err, ErrUnauthorized)
}
