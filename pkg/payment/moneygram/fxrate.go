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

// Service-option codes returned by MoneyGram's FX Rate endpoint. The exact
// strings should be confirmed against a real production response and updated
// here once captured — MG's developer portal hides response examples behind
// auth, so these are the canonical guesses based on documentation prose.
const (
	ServiceOptionCashPickup   = "CASH_PICKUP"
	ServiceOptionBankDeposit  = "BANK_DEPOSIT"
	ServiceOptionMobileWallet = "MOBILE_WALLET"
)

// FXRate is a single rate entry returned by GET /fx-rate/v1/rates for one
// (origin, sendCurrency, destination, serviceOption) corridor.
//
// IMPORTANT: per MoneyGram's own documentation, this rate is an *estimation*,
// not a formal quote. It does not bind MoneyGram to honour the rate at the
// time of pickup — the locked rate appears as `amount_out` on the SEP-24
// transaction object after the user completes the interactive webview.
type FXRate struct {
	ServiceOption   string    `json:"serviceOption"`
	SendCurrency    string    `json:"sendCurrency"`
	ReceiveCurrency string    `json:"receiveCurrency"`
	Rate            float64   `json:"rate"`
	FetchedAt       time.Time `json:"-"`
}

// FXRateRequest selects one rate by corridor + service option.
type FXRateRequest struct {
	OriginatingCountry string // ISO-3, e.g. "USA"
	SendCurrency       string // ISO-4217, e.g. "USD"
	DestinationCountry string // ISO-3, e.g. "KEN"
	ServiceOption      string // one of the ServiceOption* constants
}

// FXRateConfig configures the FX Rate REST client.
type FXRateConfig struct {
	BaseURL  string        // e.g. https://api-uat.moneygram.com
	CacheTTL time.Duration // 0 disables caching
}

// FXRateClient calls GET /fx-rate/v1/rates with bearer auth from an
// OAuthClient and caches results per corridor for FXRateConfig.CacheTTL.
type FXRateClient struct {
	cfg        FXRateConfig
	oauth      *OAuthClient
	httpClient *http.Client
	logger     *slog.Logger

	mu    sync.Mutex
	cache map[string]FXRate
}

// NewFXRateClient constructs an FX Rate client. httpClient may be nil;
// logger may be nil. The OAuth client is required and will be used to
// acquire bearer tokens on every request (subject to the OAuth client's
// own caching).
func NewFXRateClient(cfg FXRateConfig, oauth *OAuthClient, httpClient *http.Client, logger *slog.Logger) (*FXRateClient, error) {
	if cfg.BaseURL == "" {
		return nil, fmt.Errorf("moneygram: %w: FX Rate BaseURL is required", stellaranchor.ErrInvalidConfig)
	}
	if oauth == nil {
		return nil, fmt.Errorf("moneygram: %w: FX Rate requires an OAuthClient", stellaranchor.ErrInvalidConfig)
	}
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 10 * time.Second}
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &FXRateClient{
		cfg:        cfg,
		oauth:      oauth,
		httpClient: httpClient,
		logger:     logger,
		cache:      make(map[string]FXRate),
	}, nil
}

// Get returns the rate for the requested corridor + service option.
// Returns ErrServiceOptionUnavailable when MoneyGram's response does not
// include the requested ServiceOption (e.g. cash pickup not offered in
// the destination country).
func (c *FXRateClient) Get(ctx context.Context, req FXRateRequest) (FXRate, error) {
	if err := validateFXRateRequest(req); err != nil {
		return FXRate{}, err
	}

	key := fxCacheKey(req)

	if c.cfg.CacheTTL > 0 {
		c.mu.Lock()
		if cached, ok := c.cache[key]; ok && time.Since(cached.FetchedAt) < c.cfg.CacheTTL {
			c.mu.Unlock()
			return cached, nil
		}
		c.mu.Unlock()
	}

	rates, err := c.fetchAll(ctx, req)
	if err != nil {
		return FXRate{}, err
	}

	for _, r := range rates {
		if r.ServiceOption == req.ServiceOption {
			r.FetchedAt = time.Now()
			if c.cfg.CacheTTL > 0 {
				c.mu.Lock()
				c.cache[key] = r
				c.mu.Unlock()
			}
			return r, nil
		}
	}
	return FXRate{}, fmt.Errorf("moneygram: %w: %s for %sto%s",
		ErrServiceOptionUnavailable, req.ServiceOption, req.OriginatingCountry, req.DestinationCountry)
}

// fxRateResponse is a permissive container — the real response shape is
// hidden behind login on MG's developer portal, so we accept variation and
// will tighten this once we capture a production payload.
type fxRateResponse struct {
	Rates []FXRate `json:"rates"`
}

func (c *FXRateClient) fetchAll(ctx context.Context, req FXRateRequest) ([]FXRate, error) {
	token, err := c.oauth.Token(ctx)
	if err != nil {
		return nil, err
	}

	q := url.Values{}
	q.Set("originatingCountry", req.OriginatingCountry)
	q.Set("sendCurrency", req.SendCurrency)
	q.Set("destinationCountry", req.DestinationCountry)

	endpoint := strings.TrimRight(c.cfg.BaseURL, "/") + "/fx-rate/v1/rates?" + q.Encode()
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("moneygram: build FX Rate request: %w", err)
	}
	httpReq.Header.Set("Accept", "application/json")
	httpReq.Header.Set("access_token", token)

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("moneygram: FX Rate request: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(io.LimitReader(resp.Body, 64*1024))

	if resp.StatusCode == http.StatusUnauthorized {
		c.oauth.Invalidate()
		return nil, fmt.Errorf("moneygram: %w: FX Rate HTTP 401", stellaranchor.ErrUnauthorized)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("moneygram: FX Rate HTTP %d: %s",
			resp.StatusCode, truncate(string(body), 200))
	}

	var parsed fxRateResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, fmt.Errorf("moneygram: decode FX Rate response: %w", err)
	}
	return parsed.Rates, nil
}

func validateFXRateRequest(req FXRateRequest) error {
	if req.OriginatingCountry == "" || req.SendCurrency == "" || req.DestinationCountry == "" || req.ServiceOption == "" {
		return fmt.Errorf("moneygram: %w: FXRateRequest requires OriginatingCountry, SendCurrency, DestinationCountry, ServiceOption",
			stellaranchor.ErrInvalidConfig)
	}
	return nil
}

func fxCacheKey(req FXRateRequest) string {
	return req.OriginatingCountry + ":" + req.SendCurrency + ":" + req.DestinationCountry + ":" + req.ServiceOption
}
