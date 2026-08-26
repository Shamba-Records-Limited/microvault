package moneygram

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/samber/oops"

	pkgErrors "github.com/Shamba-Records-Limited/microvault/pkg/errors"
	"github.com/Shamba-Records-Limited/microvault/pkg/payment/stellaranchor"
)

// fxErr starts an error builder for MoneyGram's FX rate surface. The corridor
// and service option are the attributes worth carrying: a rate failure is
// almost always diagnosed by which corridor was asked for.
func fxErr() oops.OopsErrorBuilder {
	return oops.In(pkgErrors.DomainMoneyGram).Tags("fx")
}

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
		return nil, fxErr().With("setting", "BaseURL").Code(pkgErrors.CodeMissingDependency).
			Wrapf(stellaranchor.ErrInvalidConfig, "FX rate client is missing a required setting")
	}
	if oauth == nil {
		return nil, fxErr().With(pkgErrors.AttrDependency, "oauth_client").Code(pkgErrors.CodeMissingDependency).
			Wrapf(stellaranchor.ErrInvalidConfig, "FX rate client is missing a required dependency")
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
	// The corridor is the diagnosis here: cash pickup is simply not offered in
	// some destination countries, and that is a configuration answer rather
	// than a fault.
	return FXRate{}, fxErr().
		With("service_option", req.ServiceOption).
		With("corridor", req.OriginatingCountry+"->"+req.DestinationCountry).
		Code(pkgErrors.CodeRateUnavailable).
		Wrapf(ErrServiceOptionUnavailable, "MoneyGram does not offer this service option on this corridor")
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
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, http.NoBody)
	if err != nil {
		return nil, fxErr().Code(pkgErrors.CodeBuildFailed).Wrapf(err, "could not build the FX rate request")
	}
	httpReq.Header.Set("Accept", "application/json")
	httpReq.Header.Set("access_token", token)

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fxErr().Code(pkgErrors.CodeTransportFailed).Wrapf(err, "FX rate request did not complete")
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(io.LimitReader(resp.Body, 64*1024))

	if resp.StatusCode == http.StatusUnauthorized {
		c.oauth.Invalidate()
		return nil, fxErr().Code(pkgErrors.CodeUnauthorized).
			Wrapf(stellaranchor.ErrUnauthorized, "MoneyGram rejected the FX rate bearer token")
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fxErr().
			With(pkgErrors.AttrStatusCode, resp.StatusCode).
			With("body", truncate(string(body), 200)).
			Code(pkgErrors.CodeHTTPError).
			Errorf("FX rate endpoint returned a non-2xx")
	}

	var parsed fxRateResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, fxErr().Code(pkgErrors.CodeDecodeFailed).Wrapf(err, "could not decode the FX rate response")
	}
	return parsed.Rates, nil
}

func validateFXRateRequest(req FXRateRequest) error {
	if req.OriginatingCountry == "" || req.SendCurrency == "" || req.DestinationCountry == "" || req.ServiceOption == "" {
		return fxErr().Code(pkgErrors.CodeMissingAccount).
			Wrapf(stellaranchor.ErrInvalidConfig,
				"FX rate request needs an originating country, send currency, destination country and service option")
	}
	return nil
}

func fxCacheKey(req FXRateRequest) string {
	return req.OriginatingCountry + ":" + req.SendCurrency + ":" + req.DestinationCountry + ":" + req.ServiceOption
}
