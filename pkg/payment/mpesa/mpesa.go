package mpesa

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

// Environment selects the Daraja deployment a Client talks to.
type Environment string

// The two Daraja deployments.
const (
	EnvironmentSandbox    Environment = "sandbox"
	EnvironmentProduction Environment = "production"
)

// Daraja hosts.
const (
	sandboxBaseURL    = "https://sandbox.safaricom.co.ke"
	productionBaseURL = "https://api.safaricom.co.ke"
)

// BaseURL is the Daraja host for the environment.
func (e Environment) BaseURL() string {
	if e == EnvironmentProduction {
		return productionBaseURL
	}
	return sandboxBaseURL
}

// IsProduction reports whether e is the live environment.
func (e Environment) IsProduction() bool { return e == EnvironmentProduction }

// Valid reports whether e is a known environment.
func (e Environment) Valid() bool {
	return e == EnvironmentSandbox || e == EnvironmentProduction
}

// HttpClient is the outbound seam. *http.Client satisfies it.
type HttpClient interface {
	Do(req *http.Request) (*http.Response, error)
}

// Config carries everything a Client needs.
//
// Shortcodes are split because collections and disbursement sit on separate
// M-Pesa shortcodes in the general case. Setting both to the same value is
// valid when one shortcode carries both product sets.
type Config struct {
	Environment Environment

	ConsumerKey    string
	ConsumerSecret string

	// CollectionShortcode receives C2B and M-Pesa Express payments.
	CollectionShortcode uint

	// DisbursementShortcode funds payouts and is the PartyA of balance and
	// reversal queries against the payout side.
	DisbursementShortcode uint

	// Passkey signs the M-Pesa Express password. Issued per shortcode.
	Passkey string

	// InitiatorName is the M-PESA API operator username. InitiatorPassword is
	// its plaintext password, encrypted per call into a SecurityCredential.
	InitiatorName     string
	InitiatorPassword string

	// BaseURL overrides Environment.BaseURL. Tests point it at a stub.
	BaseURL string

	// Certificate overrides the embedded Safaricom certificate. Tests supply
	// one whose private half they hold so a SecurityCredential can be decrypted
	// and checked rather than merely inspected.
	Certificate []byte

	// HttpClient defaults to a client with a 30 second timeout.
	HttpClient HttpClient

	// TokenStore defaults to an in-process store. Deployments with more than
	// one replica should supply a shared one: Daraja invalidates the previous
	// access token on every mint, so per-process caches evict each other.
	TokenStore TokenStore

	// Clock defaults to time.Now. Overridable so timestamp derivation and
	// token expiry are testable.
	Clock func() time.Time
}

// Client is a Daraja API client.
type Client struct {
	env                   Environment
	baseURL               string
	consumerKey           string
	consumerSecret        string
	collectionShortcode   uint
	disbursementShortcode uint
	passkey               string
	initiatorName         string
	initiatorPassword     string
	certificate           []byte
	http                  HttpClient
	tokens                TokenStore
	now                   func() time.Time
	mint                  *singleFlight
}

// New builds a Client. It validates configuration eagerly so a
// misconfiguration is a boot failure rather than a failed payment.
func New(cfg Config) (*Client, error) {
	errb := mpesaErr("new")

	if !cfg.Environment.Valid() {
		return nil, errb.
			Code(pkgErrors.CodeMissingDependency).
			With("environment", string(cfg.Environment)).
			Errorf("environment must be sandbox or production")
	}
	if cfg.ConsumerKey == "" || cfg.ConsumerSecret == "" {
		return nil, errb.
			Code(pkgErrors.CodeMissingDependency).
			With(pkgErrors.AttrDependency, "consumer credentials").
			Errorf("consumer key and secret are both required")
	}

	certificate := cfg.Certificate
	if len(certificate) == 0 {
		embedded, err := embeddedCertificate(cfg.Environment)
		if err != nil {
			return nil, err
		}
		certificate = embedded
	}

	c := &Client{
		env:                   cfg.Environment,
		baseURL:               strings.TrimRight(lo.CoalesceOrEmpty(cfg.BaseURL, cfg.Environment.BaseURL()), "/"),
		consumerKey:           cfg.ConsumerKey,
		consumerSecret:        cfg.ConsumerSecret,
		collectionShortcode:   cfg.CollectionShortcode,
		disbursementShortcode: cfg.DisbursementShortcode,
		passkey:               cfg.Passkey,
		initiatorName:         cfg.InitiatorName,
		initiatorPassword:     cfg.InitiatorPassword,
		certificate:           certificate,
		http:                  cfg.HttpClient,
		tokens:                cfg.TokenStore,
		now:                   cfg.Clock,
		mint:                  newSingleFlight(),
	}
	if c.http == nil {
		c.http = &http.Client{Timeout: 30 * time.Second}
	}
	if c.tokens == nil {
		c.tokens = NewMemoryTokenStore()
	}
	if c.now == nil {
		c.now = time.Now
	}
	return c, nil
}

// Environment reports which deployment the client talks to.
func (c *Client) Environment() Environment { return c.env }

// CollectionShortcode reports the configured collection shortcode.
func (c *Client) CollectionShortcode() uint { return c.collectionShortcode }

// DisbursementShortcode reports the configured disbursement shortcode.
func (c *Client) DisbursementShortcode() uint { return c.disbursementShortcode }

// call performs one authenticated JSON request and decodes the response.
//
// A rejected access token is retried exactly once, after eviction. Daraja
// invalidates the previous token whenever a new one is minted, so a token this
// process believes is valid may have been superseded by another replica. One
// retry covers that; a loop would mask a genuine credential failure.
func call[T any](ctx context.Context, c *Client, errb oops.OopsErrorBuilder, method, path string, body any) (*T, error) {
	out, err := attempt[T](ctx, c, errb, method, path, body)
	if err == nil || !isTokenRejected(err) {
		return out, err
	}
	c.evictToken(ctx)
	return attempt[T](ctx, c, errb, method, path, body)
}

func attempt[T any](ctx context.Context, c *Client, errb oops.OopsErrorBuilder, method, path string, body any) (*T, error) {
	token, err := c.AccessToken(ctx)
	if err != nil {
		return nil, err
	}
	return send[T](ctx, c, errb, method, path, body, func(req *http.Request) {
		req.Header.Set("Authorization", "Bearer "+token)
	})
}

// send performs one JSON request without acquiring a token. decorate runs
// after the standard headers are set.
func send[T any](ctx context.Context, c *Client, errb oops.OopsErrorBuilder, method, path string, body any, decorate func(*http.Request)) (*T, error) {
	var reader io.Reader = http.NoBody
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return nil, errb.Code(pkgErrors.CodeEncodeFailed).Wrapf(err, "could not encode the request")
		}
		reader = bytes.NewReader(encoded)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, reader)
	if err != nil {
		return nil, errb.Code(pkgErrors.CodeBuildFailed).Wrapf(err, "could not build the request")
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	if decorate != nil {
		decorate(req)
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
	return &out, nil
}

// oopsBuilder names the builder type so helpers can take one without every
// signature repeating the import path.
type oopsBuilder = oops.OopsErrorBuilder

// mpesaErr is the error builder every failure in this package is built from.
func mpesaErr(op string) oops.OopsErrorBuilder {
	return oops.
		In(pkgErrors.DomainRepaymentCashIn).
		Tags("mpesa", "daraja").
		With(pkgErrors.AttrProvider, "mpesa").
		With(pkgErrors.AttrOperation, op)
}
