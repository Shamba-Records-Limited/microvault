package moneygram

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/Shamba-Records-Limited/microvault/pkg/payment/stellaranchor"
)

// OAuthConfig configures the REST OAuth 2.0 client_credentials flow that
// authenticates calls to MoneyGram's REST APIs (FX Rate, etc.). This is
// SEPARATE from SEP-10 — the bearer token returned here is not interchangeable
// with the SEP-10 JWT and must be cached on its own.
type OAuthConfig struct {
	TokenURL     string // e.g. https://api-uat.moneygram.com/oauth2/token
	ClientID     string
	ClientSecret string
	Scope        string // optional; some MG environments require a scope string

	// SafetyMargin is how long before nominal expiry the cached token is
	// considered stale and refreshed. Defaults to 30s when zero.
	SafetyMargin time.Duration
}

// OAuthClient acquires and caches a bearer token for the MoneyGram REST API.
// Safe for concurrent use.
type OAuthClient struct {
	cfg        OAuthConfig
	httpClient *http.Client
	logger     *slog.Logger

	mu      sync.Mutex
	token   string
	expires time.Time
}

// NewOAuthClient constructs an OAuth client. httpClient may be nil
// (http.DefaultClient is used). logger may be nil (slog.Default is used).
func NewOAuthClient(cfg OAuthConfig, httpClient *http.Client, logger *slog.Logger) (*OAuthClient, error) {
	if cfg.TokenURL == "" || cfg.ClientID == "" || cfg.ClientSecret == "" {
		return nil, fmt.Errorf("moneygram: %w: OAuth requires TokenURL, ClientID, ClientSecret", stellaranchor.ErrInvalidConfig)
	}
	if cfg.SafetyMargin <= 0 {
		cfg.SafetyMargin = 30 * time.Second
	}
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 15 * time.Second}
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &OAuthClient{cfg: cfg, httpClient: httpClient, logger: logger}, nil
}

// Token returns a valid bearer token, fetching a new one whenever the cached
// token has expired (or is within SafetyMargin of expiry). The returned
// string is the raw token value to put in an `access_token` header — no
// "Bearer " prefix per MoneyGram's REST API convention.
func (c *OAuthClient) Token(ctx context.Context) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.token != "" && time.Now().Add(c.cfg.SafetyMargin).Before(c.expires) {
		return c.token, nil
	}

	tok, ttl, err := c.fetchToken(ctx)
	if err != nil {
		return "", err
	}
	c.token = tok
	c.expires = time.Now().Add(ttl)
	return tok, nil
}

// Invalidate evicts the cached token, forcing the next Token call to refresh.
// Use after a 401 response from a downstream API.
func (c *OAuthClient) Invalidate() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.token = ""
	c.expires = time.Time{}
}

type oauthTokenResponse struct {
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
	ExpiresIn   int64  `json:"expires_in"` // seconds
	Scope       string `json:"scope,omitempty"`
}

func (c *OAuthClient) fetchToken(ctx context.Context) (string, time.Duration, error) {
	form := url.Values{}
	form.Set("grant_type", "client_credentials")
	form.Set("client_id", c.cfg.ClientID)
	form.Set("client_secret", c.cfg.ClientSecret)
	if c.cfg.Scope != "" {
		form.Set("scope", c.cfg.Scope)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.cfg.TokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return "", 0, fmt.Errorf("moneygram: build OAuth request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", 0, fmt.Errorf("moneygram: OAuth request: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(io.LimitReader(resp.Body, 16*1024))

	if resp.StatusCode == http.StatusUnauthorized {
		return "", 0, fmt.Errorf("moneygram: %w: OAuth token endpoint rejected credentials (HTTP 401): %s",
			stellaranchor.ErrUnauthorized, truncate(string(body), 200))
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", 0, fmt.Errorf("moneygram: OAuth token endpoint HTTP %d: %s",
			resp.StatusCode, truncate(string(body), 200))
	}

	var tr oauthTokenResponse
	if err := json.Unmarshal(body, &tr); err != nil {
		return "", 0, fmt.Errorf("moneygram: decode OAuth response: %w", err)
	}
	if tr.AccessToken == "" {
		return "", 0, fmt.Errorf("moneygram: OAuth response missing access_token")
	}

	ttl := time.Duration(tr.ExpiresIn) * time.Second
	if ttl <= 0 {
		// Per MG docs, default TTL is 1h. Any zero/negative value is a server
		// quirk; treat as 1h to avoid hammering the token endpoint.
		ttl = time.Hour
	}

	c.logger.Debug("moneygram: oauth token refreshed", "ttl_seconds", int(ttl.Seconds()))
	return tr.AccessToken, ttl, nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
