package moneygram

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/Shamba-Records-Limited/microvault/pkg/payment/stellaranchor"
)

// Config bundles everything required to construct a Client: TOML-derived
// values, treasury secret, and optional REST credentials for the FX Rate API.
//
// Fetch the TOML at startup (stellaranchor.FetchTOML), validate it
// (TOML.Validate), and pass the relevant fields here. Doing it that way means
// TOML rotation surfaces as an explicit re-Validate at boot, not as a silent
// runtime drift.
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

// Client is the top-level MoneyGram SDK handle. It embeds the generic
// stellaranchor.Client (SEP-1/9/10/24 + JWT cache) and layers on MG-specific
// REST OAuth and FX Rate clients.
type Client struct {
	*stellaranchor.Client
	OAuth  *OAuthClient  // nil when REST credentials not provided
	FXRate *FXRateClient // nil when REST credentials not provided

	cfg Config
}

// New wires every sub-client. The embedded stellaranchor.Client is always
// constructed; OAuth + FXRate are only wired when REST credentials are set.
func New(cfg Config) (*Client, error) {
	if cfg.FXRateCacheTTL <= 0 {
		cfg.FXRateCacheTTL = 60 * time.Second
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}

	anchor, err := stellaranchor.New(stellaranchor.Config{
		HomeDomain:        cfg.HomeDomain,
		WebAuthEndpoint:   cfg.WebAuthEndpoint,
		TransferServerURL: cfg.TransferServerURL,
		ServerSigningKey:  cfg.ServerSigningKey,
		NetworkPassphrase: cfg.NetworkPassphrase,
		USDCIssuer:        cfg.USDCIssuer,
		TreasurySecret:    cfg.TreasurySecret,
		HTTPClient:        cfg.HTTPClient,
		Logger:            cfg.Logger,
		JWTSafetyMargin:   cfg.JWTSafetyMargin,
	})
	if err != nil {
		return nil, err
	}

	c := &Client{Client: anchor, cfg: cfg}

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

// NewFXOrchestrator returns an FX orchestrator with this client's FXRate
// sub-client (if available) as the primary source. fallback may be nil when
// MG REST credentials are configured and the consumer wants MG-only quoting.
//
// Returns an error if both sources are nil — callers should check
// HasFXRate() first if they want to skip wiring entirely.
func (c *Client) NewFXOrchestrator(fallback FallbackRateSource, cfg FXOrchestratorConfig) (*FXOrchestrator, error) {
	return NewFXOrchestrator(c.FXRate, fallback, cfg, c.cfg.Logger)
}

// HasFXRate reports whether the Client has a usable FX Rate sub-client (i.e.
// REST credentials were configured and the OAuth + FXRate clients constructed).
// Mirrors HasRESTCredentials at the config layer.
func (c *Client) HasFXRate() bool {
	return c.FXRate != nil
}

func restConfigured(c RESTConfig) bool {
	return c.BaseURL != "" && c.OAuthTokenURL != "" && c.ClientID != "" && c.ClientSecret != ""
}
