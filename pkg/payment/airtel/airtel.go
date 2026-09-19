package airtel

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/samber/lo"
	"github.com/samber/oops"

	pkgErrors "github.com/Shamba-Records-Limited/microvault/pkg/errors"
)

// Environment selects the Airtel deployment a Client talks to.
type Environment string

// The two deployments.
const (
	EnvironmentStaging    Environment = "staging"
	EnvironmentProduction Environment = "production"
)

// Kenya op-co hosts. The catalogue is not served from one host — Favourite
// Service sits on airtel.africa and RemX on airtel.ke — but every endpoint
// this package implements is a Kenya op-co endpoint.
const (
	stagingBaseURL    = "https://openapiuat.airtelkenya.com"
	productionBaseURL = "https://openapi.airtelkenya.com"
)

// BaseURL is the Airtel host for the environment.
func (e Environment) BaseURL() string {
	if e == EnvironmentProduction {
		return productionBaseURL
	}
	return stagingBaseURL
}

// IsProduction reports whether e is the live environment.
func (e Environment) IsProduction() bool { return e == EnvironmentProduction }

// Valid reports whether e is a known environment.
func (e Environment) Valid() bool {
	return e == EnvironmentStaging || e == EnvironmentProduction
}

// HttpClient is the outbound seam. *http.Client satisfies it.
type HttpClient interface {
	Do(req *http.Request) (*http.Response, error)
}

// Config carries everything a Client needs.
type Config struct {
	Environment Environment

	// ClientID and ClientSecret are the consumer key and secret from the
	// application's Keys section. They are the sole identity of the
	// application and are issued per environment.
	ClientID     string
	ClientSecret string

	// Country and Currency populate the X-Country and X-Currency headers.
	// Defaults are KE and KES.
	Country  string
	Currency string

	// SigningEnabled turns on Collection v2 message signing for Payment and
	// Refund. It must match the per-application toggle under Settings →
	// Security: signing a payload the gateway does not expect is as broken as
	// not signing one it does.
	SigningEnabled bool

	// CallbackHMACKey is the private key from Application Settings, used to
	// recompute a callback's hash. Empty disables verification, which is
	// correct only when callback authentication is off.
	CallbackHMACKey string

	// BaseURL overrides Environment.BaseURL. Tests point it at a stub.
	BaseURL string

	// HttpClient defaults to a client with a 30 second timeout.
	HttpClient HttpClient

	// TokenStore defaults to an in-process store. Airtel tokens live 180
	// seconds, so a deployment with several replicas mints far more often
	// than Daraja does; a shared store makes that one mint per cluster.
	TokenStore TokenStore

	// KeyStore caches the RSA public key fetched from EncryptionKeys. It
	// defaults to an in-process store and is only consulted when signing is
	// enabled.
	KeyStore KeyStore

	// Clock defaults to time.Now.
	Clock func() time.Time
}

// Client is an Airtel Open API client.
type Client struct {
	env          Environment
	baseURL      string
	clientID     string
	clientSecret string
	country      string
	currency     string
	signing      bool
	hmacKey      string
	http         HttpClient
	tokens       TokenStore
	keys         KeyStore
	now          func() time.Time
	mint         *singleFlight
}

// Default country and currency for the Kenya op-co.
const (
	defaultCountry  = "KE"
	defaultCurrency = "KES"
)

// New builds a Client. It validates configuration eagerly so a
// misconfiguration is a boot failure rather than a failed payment.
func New(cfg Config) (*Client, error) {
	errb := airtelErr("new")

	if !cfg.Environment.Valid() {
		return nil, errb.
			Code(pkgErrors.CodeMissingDependency).
			With("environment", string(cfg.Environment)).
			Errorf("environment must be staging or production")
	}
	if cfg.ClientID == "" || cfg.ClientSecret == "" {
		return nil, errb.
			Code(pkgErrors.CodeMissingDependency).
			With(pkgErrors.AttrDependency, "consumer credentials").
			Errorf("client id and secret are both required")
	}

	c := &Client{
		env:          cfg.Environment,
		baseURL:      strings.TrimRight(lo.CoalesceOrEmpty(cfg.BaseURL, cfg.Environment.BaseURL()), "/"),
		clientID:     cfg.ClientID,
		clientSecret: cfg.ClientSecret,
		country:      lo.CoalesceOrEmpty(cfg.Country, defaultCountry),
		currency:     lo.CoalesceOrEmpty(cfg.Currency, defaultCurrency),
		signing:      cfg.SigningEnabled,
		hmacKey:      cfg.CallbackHMACKey,
		http:         cfg.HttpClient,
		tokens:       cfg.TokenStore,
		keys:         cfg.KeyStore,
		now:          cfg.Clock,
		mint:         newSingleFlight(),
	}
	if c.http == nil {
		c.http = &http.Client{Timeout: 30 * time.Second}
	}
	if c.tokens == nil {
		c.tokens = NewMemoryTokenStore()
	}
	if c.keys == nil {
		c.keys = NewMemoryKeyStore()
	}
	if c.now == nil {
		c.now = time.Now
	}
	return c, nil
}

// Environment reports which deployment the client talks to.
func (c *Client) Environment() Environment { return c.env }

// Country reports the configured transaction country.
func (c *Client) Country() string { return c.country }

// Currency reports the configured transaction currency.
func (c *Client) Currency() string { return c.currency }

// SigningEnabled reports whether Collection v2 message signing is on.
func (c *Client) SigningEnabled() bool { return c.signing }

// headerStyle is the casing an endpoint wants for its country and currency
// headers. HTTP headers are case-insensitive by spec, but Airtel's gateways
// have been observed to care and the catalogue uses five different spellings —
// Transactions Summary takes the lowercase pair while Collection takes the
// uppercase one, in the same account, against the same host.
type headerStyle uint8

const (
	// headerUpper sends X-Country and X-Currency.
	headerUpper headerStyle = iota

	// headerLower sends x-country and x-currency.
	headerLower

	// headerNone sends neither.
	headerNone
)

// endpoint is one method, path and header contract. Bundling them keeps the
// casing beside the path it belongs to rather than at the call site, where a
// copied line silently carries the wrong spelling.
type endpoint struct {
	method  string
	path    string
	headers headerStyle

	// signed marks an endpoint that participates in Collection v2 message
	// signing when the client has signing enabled.
	signed bool

	// outcomeBearing marks an endpoint whose unsuccessful envelope is the
	// answer rather than a rejection. An enquiry that reports TF has done its
	// job: the transaction failed, and the caller needs that outcome to close
	// the loan. Returning an error there would discard the status, the
	// message and the response code, leaving the caller with nothing to act
	// on but the fact that asking went wrong — which it did not.
	outcomeBearing bool
}

// call performs one authenticated JSON request and decodes the response.
//
// A rejected access token is retried exactly once, after eviction. Airtel does
// not document invalidating the previous token on a mint the way Daraja does,
// but a 180 second life means a token can expire between the cache check and
// the wire, and one retry covers that without masking a credential failure.
func call[T any](ctx context.Context, c *Client, errb oops.OopsErrorBuilder, ep endpoint, path string, body any) (*T, error) {
	out, err := attempt[T](ctx, c, errb, ep, path, body)
	if err == nil || !isTokenRejected(err) {
		return out, err
	}
	c.evictToken(ctx)
	return attempt[T](ctx, c, errb, ep, path, body)
}

func attempt[T any](ctx context.Context, c *Client, errb oops.OopsErrorBuilder, ep endpoint, path string, body any) (*T, error) {
	token, err := c.AccessToken(ctx)
	if err != nil {
		return nil, err
	}

	var sign func(*http.Request, []byte) error
	if ep.signed && c.signing {
		sign = func(req *http.Request, payload []byte) error {
			return c.signRequest(ctx, req, payload)
		}
	}

	return send[T](ctx, c, errb, ep, path, body, func(req *http.Request, payload []byte) error {
		req.Header.Set("Authorization", "Bearer "+token)
		c.setLocaleHeaders(req, ep.headers)
		if sign != nil {
			return sign(req, payload)
		}
		return nil
	})
}

// setLocaleHeaders applies the endpoint's country and currency spelling.
func (c *Client) setLocaleHeaders(req *http.Request, style headerStyle) {
	switch style {
	case headerUpper:
		req.Header.Set("X-Country", c.country)
		req.Header.Set("X-Currency", c.currency)
	case headerLower:
		req.Header.Set("x-country", c.country)
		req.Header.Set("x-currency", c.currency)
	case headerNone:
	}
}

// send performs one JSON request without acquiring a token. decorate runs
// after the standard headers are set and receives the marshalled body, which
// message signing needs and nothing else does.
func send[T any](ctx context.Context, c *Client, errb oops.OopsErrorBuilder, ep endpoint, path string, body any, decorate func(*http.Request, []byte) error) (*T, error) {
	var payload []byte
	var reader io.Reader = http.NoBody
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return nil, errb.Code(pkgErrors.CodeEncodeFailed).Wrapf(err, "could not encode the request")
		}
		payload = encoded
		reader = bytes.NewReader(encoded)
	}

	req, err := http.NewRequestWithContext(ctx, ep.method, c.baseURL+path, reader)
	if err != nil {
		return nil, errb.Code(pkgErrors.CodeBuildFailed).Wrapf(err, "could not build the request")
	}
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if decorate != nil {
		if err := decorate(req, payload); err != nil {
			return nil, err
		}
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, errb.Code(pkgErrors.CodeTransportFailed).Wrapf(err, "request did not complete")
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, errb.Code(pkgErrors.CodeDecodeFailed).Wrapf(err, "could not read the response")
	}

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return nil, parseError(errb, resp.StatusCode, raw)
	}

	var out T
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, errb.
			Code(pkgErrors.CodeDecodeFailed).
			With(pkgErrors.AttrStatusCode, resp.StatusCode).
			Wrapf(err, "could not decode the response")
	}

	if enveloped, ok := any(&out).(interface{ envelope() Status }); ok && !ep.outcomeBearing {
		if err := checkEnvelope(errb, resp.StatusCode, enveloped.envelope()); err != nil {
			return nil, err
		}
	}
	return &out, nil
}

// airtelErr is the error builder every failure in this package is built from.
func airtelErr(op string) oops.OopsErrorBuilder {
	return oops.
		In(pkgErrors.DomainRepaymentCashIn).
		Tags("airtel", "openapi").
		With(pkgErrors.AttrProvider, "airtel").
		With(pkgErrors.AttrOperation, op)
}
