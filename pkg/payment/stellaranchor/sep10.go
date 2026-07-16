package stellaranchor

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/stellar/go-stellar-sdk/keypair"
	"github.com/stellar/go-stellar-sdk/txnbuild"
)

// AuthConfig configures the SEP-10 client.
type AuthConfig struct {
	WebAuthEndpoint   string // from TOML
	ServerSigningKey  string // from TOML — used to verify the challenge signature
	NetworkPassphrase string // from TOML, e.g. "Public Global Stellar Network ; September 2015"
	HomeDomain        string // e.g. "stellar.moneygram.com"
	WebAuthDomain     string // typically equal to HomeDomain unless MG runs the auth server on a different host

	// TreasurySecret is the S... seed of the custodial treasury account.
	// Held in memory only — never logged. The corresponding G... address
	// becomes the SEP-10 `account` parameter.
	TreasurySecret string

	// FallbackTokenTTL is used when the issued JWT has no `exp` claim.
	// Defaults to 23h to stay safely under MG's typical 24h validity.
	FallbackTokenTTL time.Duration

	// HTTPTimeout caps the duration of any single SEP-10 HTTP exchange.
	// Defaults to 15s.
	HTTPTimeout time.Duration
}

// AuthClient implements the SEP-10 challenge/co-sign/token flow against a
// MoneyGram anchor in custodial mode (single treasury Stellar account, per-
// user int64 memo).
type AuthClient struct {
	cfg        AuthConfig
	treasury   *keypair.Full
	httpClient *http.Client
	logger     *slog.Logger
}

// NewAuthClient validates the config and constructs a client.
// httpClient may be nil; logger may be nil.
func NewAuthClient(cfg AuthConfig, httpClient *http.Client, logger *slog.Logger) (*AuthClient, error) {
	switch {
	case cfg.WebAuthEndpoint == "":
		return nil, fmt.Errorf("stellaranchor: %w: WebAuthEndpoint required", ErrInvalidConfig)
	case cfg.ServerSigningKey == "":
		return nil, fmt.Errorf("stellaranchor: %w: ServerSigningKey required", ErrInvalidConfig)
	case cfg.NetworkPassphrase == "":
		return nil, fmt.Errorf("stellaranchor: %w: NetworkPassphrase required", ErrInvalidConfig)
	case cfg.HomeDomain == "":
		return nil, fmt.Errorf("stellaranchor: %w: HomeDomain required", ErrInvalidConfig)
	case cfg.TreasurySecret == "":
		return nil, fmt.Errorf("stellaranchor: %w: TreasurySecret required", ErrInvalidConfig)
	}

	kp, err := keypair.ParseFull(cfg.TreasurySecret)
	if err != nil {
		return nil, fmt.Errorf("stellaranchor: %w: parse TreasurySecret: %v", ErrInvalidConfig, err)
	}

	if cfg.FallbackTokenTTL <= 0 {
		cfg.FallbackTokenTTL = 23 * time.Hour
	}
	if cfg.HTTPTimeout <= 0 {
		cfg.HTTPTimeout = 15 * time.Second
	}
	if cfg.WebAuthDomain == "" {
		cfg.WebAuthDomain = cfg.HomeDomain
	}
	if httpClient == nil {
		httpClient = &http.Client{Timeout: cfg.HTTPTimeout}
	}
	if logger == nil {
		logger = slog.Default()
	}

	return &AuthClient{cfg: cfg, treasury: kp, httpClient: httpClient, logger: logger}, nil
}

// TreasuryAddress returns the custodial G... account ID derived from the
// configured treasury secret. Useful for diagnostics and Stellar payments.
func (c *AuthClient) TreasuryAddress() string {
	return c.treasury.Address()
}

// AuthResult is the output of a successful SEP-10 round trip.
type AuthResult struct {
	JWT       string
	ExpiresAt time.Time
}

// Authenticate runs the SEP-10 challenge/co-sign/submit flow for a given
// child-memo and returns the JWT plus its expiry. Callers should cache the
// result via JWTCache rather than calling this on every SEP-24 request.
func (c *AuthClient) Authenticate(ctx context.Context, childMemo int64) (AuthResult, error) {
	if childMemo < 0 {
		return AuthResult{}, fmt.Errorf("stellaranchor: %w: childMemo must be non-negative", ErrInvalidConfig)
	}

	challenge, err := c.fetchChallenge(ctx, childMemo)
	if err != nil {
		return AuthResult{}, err
	}

	signed, err := c.coSignChallenge(challenge)
	if err != nil {
		return AuthResult{}, err
	}

	jwt, err := c.submitChallenge(ctx, signed)
	if err != nil {
		return AuthResult{}, err
	}

	expiresAt := tokenExpiry(jwt, time.Now().Add(c.cfg.FallbackTokenTTL))
	c.logger.Debug("stellaranchor: sep-10 token issued",
		"memo", childMemo,
		"expires_at", expiresAt.Format(time.RFC3339),
	)
	return AuthResult{JWT: jwt, ExpiresAt: expiresAt}, nil
}

func (c *AuthClient) fetchChallenge(ctx context.Context, memo int64) (string, error) {
	q := url.Values{}
	q.Set("account", c.treasury.Address())
	q.Set("memo", strconv.FormatInt(memo, 10))

	resp, err := doHTTPWithRetry(ctx, c.httpClient, func() (*http.Request, error) {
		return http.NewRequestWithContext(ctx, http.MethodGet, c.cfg.WebAuthEndpoint+"?"+q.Encode(), nil)
	})
	if err != nil {
		return "", fmt.Errorf("stellaranchor: SEP-10 challenge request: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("stellaranchor: SEP-10 challenge HTTP %d: %s",
			resp.StatusCode, truncate(string(body), 200))
	}

	var parsed struct {
		Transaction       string `json:"transaction"`
		NetworkPassphrase string `json:"network_passphrase"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return "", fmt.Errorf("stellaranchor: decode challenge: %w", err)
	}
	if parsed.Transaction == "" {
		return "", fmt.Errorf("stellaranchor: challenge response missing 'transaction'")
	}
	if parsed.NetworkPassphrase != "" && parsed.NetworkPassphrase != c.cfg.NetworkPassphrase {
		return "", fmt.Errorf("stellaranchor: challenge network passphrase mismatch (got %q, want %q)",
			parsed.NetworkPassphrase, c.cfg.NetworkPassphrase)
	}
	return parsed.Transaction, nil
}

// coSignChallenge validates the server's signature + transaction shape via
// the SDK's ReadChallengeTx, then adds our treasury signature and returns
// the doubly-signed XDR.
func (c *AuthClient) coSignChallenge(challengeXDR string) (string, error) {
	tx, _, _, _, err := txnbuild.ReadChallengeTx(
		challengeXDR,
		c.cfg.ServerSigningKey,
		c.cfg.NetworkPassphrase,
		c.cfg.WebAuthDomain,
		[]string{c.cfg.HomeDomain},
	)
	if err != nil {
		return "", fmt.Errorf("stellaranchor: validate SEP-10 challenge: %w", err)
	}

	signed, err := tx.Sign(c.cfg.NetworkPassphrase, c.treasury)
	if err != nil {
		return "", fmt.Errorf("stellaranchor: co-sign SEP-10 challenge: %w", err)
	}

	out, err := signed.Base64()
	if err != nil {
		return "", fmt.Errorf("stellaranchor: encode signed challenge: %w", err)
	}
	return out, nil
}

func (c *AuthClient) submitChallenge(ctx context.Context, signedXDR string) (string, error) {
	body, err := json.Marshal(map[string]string{"transaction": signedXDR})
	if err != nil {
		return "", fmt.Errorf("stellaranchor: marshal challenge submission: %w", err)
	}

	resp, err := doHTTPWithRetry(ctx, c.httpClient, func() (*http.Request, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.cfg.WebAuthEndpoint, strings.NewReader(string(body)))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json")
		return req, nil
	})
	if err != nil {
		return "", fmt.Errorf("stellaranchor: SEP-10 token request: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 16*1024))

	if resp.StatusCode == http.StatusUnauthorized {
		return "", fmt.Errorf("stellaranchor: %w: SEP-10 token endpoint rejected challenge", ErrUnauthorized)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("stellaranchor: SEP-10 token HTTP %d: %s",
			resp.StatusCode, truncate(string(respBody), 200))
	}

	var parsed struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return "", fmt.Errorf("stellaranchor: decode token response: %w", err)
	}
	if parsed.Token == "" {
		return "", fmt.Errorf("stellaranchor: token response missing 'token'")
	}
	return parsed.Token, nil
}

// tokenExpiry parses the JWT's `exp` claim without verifying signatures (we
// already trust the issuer — we just submitted it ourselves). Falls back to
// the supplied default if the JWT is malformed or has no `exp`.
func tokenExpiry(jwt string, fallback time.Time) time.Time {
	parts := strings.Split(jwt, ".")
	if len(parts) < 2 {
		return fallback
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return fallback
	}
	var claims struct {
		Exp int64 `json:"exp"`
	}
	if err := json.Unmarshal(payload, &claims); err != nil || claims.Exp == 0 {
		return fallback
	}
	return time.Unix(claims.Exp, 0)
}
