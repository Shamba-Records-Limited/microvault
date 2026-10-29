package didit

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/samber/oops"

	"github.com/Shamba-Records-Limited/microvault/pkg/telemetry"

	pkgErrors "github.com/Shamba-Records-Limited/microvault/pkg/errors"
)

// Provider is this package's name in compliance.Screening.Provider.
const Provider = "didit"

// maxResponseBytes caps a response body read into memory.
const maxResponseBytes = 4 << 20

const defaultBaseURL = "https://verification.didit.me"

// Config configures a Client.
type Config struct {
	APIKey  string
	BaseURL string
	// Chain is the network screened addresses live on, in our own naming
	// ("stellar"); see chains.
	Chain      string
	Thresholds Thresholds
}

// Client calls Didit's v3 API.
type Client struct {
	httpClient *http.Client
	baseURL    string
	apiKey     string
	chain      string
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
		chain:      cfg.Chain,
		thresholds: cfg.Thresholds,
	}
}

func diditErr(op string) oops.OopsErrorBuilder {
	return oops.In(pkgErrors.DomainCompliance).Tags("didit").With(pkgErrors.AttrOperation, op)
}

func (c *Client) do(ctx context.Context, op, method, path string, body, out any) (json.RawMessage, error) {
	var reader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, diditErr(op).Code(pkgErrors.CodeEncodeFailed).Wrapf(err, "could not encode the request")
		}
		reader = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, reader)
	if err != nil {
		return nil, diditErr(op).Code(pkgErrors.CodeBuildFailed).Wrapf(err, "could not build the request")
	}
	req.Header.Set("x-api-key", c.apiKey)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, diditErr(op).Code(pkgErrors.CodeTransportFailed).Wrapf(err, "request did not complete")
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return nil, diditErr(op).Code(pkgErrors.CodeTransportFailed).Wrapf(err, "could not read the response body")
	}
	if err := classifyStatus(op, resp.StatusCode, raw); err != nil {
		return nil, err
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return nil, diditErr(op).Code(pkgErrors.CodeDecodeFailed).Wrapf(err, "could not decode the response")
	}
	return raw, nil
}

func classifyStatus(op string, status int, body []byte) error {
	switch {
	case status >= 200 && status < 300:
		return nil
	case status == http.StatusBadRequest:
		return diditErr(op).Code(pkgErrors.CodeBuildFailed).With("body", string(body)).
			Errorf("didit rejected the request as malformed")
	case status == http.StatusUnauthorized || status == http.StatusForbidden:
		return diditErr(op).Code(pkgErrors.CodeUnauthorized).With(pkgErrors.AttrStatusCode, status).
			Errorf("didit rejected the API key")
	case status == http.StatusConflict:
		return diditErr(op).Code(pkgErrors.CodeInvalidConfig).With("body", string(body)).
			Errorf("didit has no screening provider configured, or it rejected the connected key")
	case status == http.StatusNotFound:
		return diditErr(op).Code(pkgErrors.CodeNotFound).Errorf("didit has no such record")
	default:
		return diditErr(op).Code(pkgErrors.CodeHTTPError).With(pkgErrors.AttrStatusCode, status).
			With("body", string(body)).Errorf("didit returned an unexpected status")
	}
}
