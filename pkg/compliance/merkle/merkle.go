package merkle

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/samber/oops"

	"github.com/Shamba-Records-Limited/microvault/pkg/telemetry"

	pkgErrors "github.com/Shamba-Records-Limited/microvault/pkg/errors"
)

// Provider is this package's name in compliance.Screening.Provider.
const Provider = "merkle"

const (
	// maxResponseBytes caps a response body read into memory.
	maxResponseBytes = 4 << 20
	defaultBaseURL   = "https://api.merklescience.com/api/v4.2"
	// stellarBlockchain is Compass's blockchain code for Stellar.
	stellarBlockchain = "13"
	// typeDeposit marks the address or transaction as a deposit into the
	// vault, the only direction lenders' funds take that we screen.
	typeDeposit = 1
)

// ErrNotSupported is Compass's 400 for a token or address it cannot screen.
var ErrNotSupported = errors.New("merkle: token or address not supported")

// Config configures a Client.
type Config struct {
	APIKey     string
	BaseURL    string
	Thresholds Thresholds
}

// Client is the Compass API client. It holds no state beyond configuration.
type Client struct {
	httpClient *http.Client
	baseURL    string
	apiKey     string
	thresholds Thresholds
}

// NewClient builds a Client.
func NewClient(cfg Config) *Client {
	baseURL := cfg.BaseURL
	if baseURL == "" {
		baseURL = defaultBaseURL
	}
	return &Client{
		httpClient: telemetry.Client(&http.Client{Timeout: 20 * time.Second}),
		baseURL:    strings.TrimSuffix(baseURL, "/"),
		apiKey:     cfg.APIKey,
		thresholds: cfg.Thresholds,
	}
}

func merkleErr(op string) oops.OopsErrorBuilder {
	return oops.In(pkgErrors.DomainCompliance).Tags("merkle").With(pkgErrors.AttrOperation, op)
}

func (c *Client) post(ctx context.Context, op, path string, body, out any) (json.RawMessage, error) {
	b, err := json.Marshal(body)
	if err != nil {
		return nil, merkleErr(op).Code(pkgErrors.CodeEncodeFailed).Wrapf(err, "could not encode the request")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, bytes.NewReader(b))
	if err != nil {
		return nil, merkleErr(op).Code(pkgErrors.CodeBuildFailed).Wrapf(err, "could not build the request")
	}
	req.Header.Set("X-API-KEY", c.apiKey)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, merkleErr(op).Code(pkgErrors.CodeTransportFailed).Wrapf(err, "request did not complete")
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return nil, merkleErr(op).Code(pkgErrors.CodeTransportFailed).Wrapf(err, "could not read the response body")
	}
	if err := classifyStatus(op, resp.StatusCode, raw, resp.Header); err != nil {
		return nil, err
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return nil, merkleErr(op).Code(pkgErrors.CodeDecodeFailed).Wrapf(err, "could not decode the response")
	}
	return raw, nil
}

func classifyStatus(op string, status int, body []byte, header http.Header) error {
	switch status {
	case http.StatusOK, http.StatusCreated:
		return nil
	case http.StatusBadRequest:
		if strings.Contains(strings.ToLower(string(body)), "not supported") {
			return merkleErr(op).Code(pkgErrors.CodeBuildFailed).With("body", string(body)).
				Wrapf(ErrNotSupported, "compass cannot screen this")
		}
		return merkleErr(op).Code(pkgErrors.CodeBuildFailed).With(pkgErrors.AttrStatusCode, status).
			With("body", string(body)).Errorf("compass rejected the request as malformed")
	case http.StatusUnauthorized, http.StatusForbidden:
		return merkleErr(op).Code(pkgErrors.CodeUnauthorized).With(pkgErrors.AttrStatusCode, status).
			Errorf("compass rejected the API key")
	case http.StatusTooManyRequests:
		return merkleErr(op).Code(pkgErrors.CodeHTTPError).With("retry_after", header.Get("Retry-After")).
			Errorf("compass rate limit exceeded")
	default:
		return merkleErr(op).Code(pkgErrors.CodeHTTPError).With(pkgErrors.AttrStatusCode, status).
			With("body", string(body)).Errorf("compass returned an unexpected status")
	}
}
