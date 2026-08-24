package stellaranchor

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/samber/oops"
	"github.com/stellar/go-stellar-sdk/keypair"
	"github.com/stellar/go-stellar-sdk/txnbuild"

	pkgErrors "github.com/Shamba-Records-Limited/microvault/pkg/errors"
)

// authErr starts an error builder for SEP-10 work.
func authErr() oops.OopsErrorBuilder {
	return oops.In(errDomain).Tags("sep10", "auth")
}

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
	// Which setting is absent is an attribute, so a boot failure is one error
	// group with a field to read rather than five distinct messages.
	missingCfg := func(field string) error {
		return authErr().
			Code(pkgErrors.CodeMissingDependency).
			With("setting", field).
			Wrapf(ErrInvalidConfig, "required anchor auth setting is missing")
	}

	switch {
	case cfg.WebAuthEndpoint == "":
		return nil, missingCfg("WebAuthEndpoint")
	case cfg.ServerSigningKey == "":
		return nil, missingCfg("ServerSigningKey")
	case cfg.NetworkPassphrase == "":
		return nil, missingCfg("NetworkPassphrase")
	case cfg.HomeDomain == "":
		return nil, missingCfg("HomeDomain")
	case cfg.TreasurySecret == "":
		return nil, missingCfg("TreasurySecret")
	}

	kp, err := keypair.ParseFull(cfg.TreasurySecret)
	if err != nil {
		// The secret itself is never an attribute.
		return nil, authErr().
			Code(pkgErrors.CodeInvalidAddress).
			With("setting", "TreasurySecret").
			Wrapf(ErrInvalidConfig, "treasury secret is not a valid Stellar key")
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
		return AuthResult{}, authErr().
			Code(pkgErrors.CodeInvalidAmount).
			With("child_memo", childMemo).
			Wrapf(ErrInvalidConfig, "child memo must be non-negative")
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
		return http.NewRequestWithContext(ctx, http.MethodGet, c.cfg.WebAuthEndpoint+"?"+q.Encode(), http.NoBody)
	})
	if err != nil {
		return "", authErr().With("child_memo", memo).Code(pkgErrors.CodeTransportFailed).
			Wrapf(err, "SEP-10 challenge request did not complete")
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	if resp.StatusCode != http.StatusOK {
		return "", authErr().
			With("child_memo", memo).
			With(pkgErrors.AttrStatusCode, resp.StatusCode).
			With("body", truncate(string(body), 200)).
			Code(pkgErrors.CodeHTTPError).
			Errorf("anchor returned a non-200 for the SEP-10 challenge")
	}

	var parsed struct {
		Transaction       string `json:"transaction"`
		NetworkPassphrase string `json:"network_passphrase"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return "", authErr().With("child_memo", memo).Code(pkgErrors.CodeDecodeFailed).
			Wrapf(err, "could not decode the SEP-10 challenge response")
	}
	if parsed.Transaction == "" {
		return "", authErr().With("child_memo", memo).Code(pkgErrors.CodeIncompleteResponse).
			Errorf("SEP-10 challenge response has no transaction")
	}
	if parsed.NetworkPassphrase != "" && parsed.NetworkPassphrase != c.cfg.NetworkPassphrase {
		// A passphrase mismatch means the challenge is for a different network
		// — signing it would be signing something we did not intend.
		return "", authErr().
			With("got", parsed.NetworkPassphrase).
			With("want", c.cfg.NetworkPassphrase).
			Code(pkgErrors.CodePermissionDenied).
			Errorf("SEP-10 challenge is for a different Stellar network")
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
		return "", authErr().Code(pkgErrors.CodePermissionDenied).
			Wrapf(err, "SEP-10 challenge failed validation")
	}

	signed, err := tx.Sign(c.cfg.NetworkPassphrase, c.treasury)
	if err != nil {
		return "", authErr().Code(pkgErrors.CodeBuildFailed).
			Wrapf(err, "could not co-sign the SEP-10 challenge")
	}

	out, err := signed.Base64()
	if err != nil {
		return "", authErr().Code(pkgErrors.CodeEncodeFailed).
			Wrapf(err, "could not encode the signed SEP-10 challenge")
	}
	return out, nil
}

func (c *AuthClient) submitChallenge(ctx context.Context, signedXDR string) (string, error) {
	body, err := json.Marshal(map[string]string{"transaction": signedXDR})
	if err != nil {
		return "", authErr().Code(pkgErrors.CodeEncodeFailed).
			Wrapf(err, "could not encode the SEP-10 challenge submission")
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
		return "", authErr().Code(pkgErrors.CodeTransportFailed).
			Wrapf(err, "SEP-10 token request did not complete")
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 16*1024))

	if resp.StatusCode == http.StatusUnauthorized {
		return "", authErr().Code(pkgErrors.CodeUnauthorized).
			Wrapf(ErrUnauthorized, "anchor rejected the signed SEP-10 challenge")
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", authErr().
			With(pkgErrors.AttrStatusCode, resp.StatusCode).
			With("body", truncate(string(respBody), 200)).
			Code(pkgErrors.CodeHTTPError).
			Errorf("anchor returned a non-200 for the SEP-10 token")
	}

	var parsed struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return "", authErr().Code(pkgErrors.CodeDecodeFailed).
			Wrapf(err, "could not decode the SEP-10 token response")
	}
	if parsed.Token == "" {
		return "", authErr().Code(pkgErrors.CodeIncompleteResponse).
			Errorf("SEP-10 token response has no token")
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
