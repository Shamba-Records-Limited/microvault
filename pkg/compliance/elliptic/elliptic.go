package elliptic

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"time"

	pkgErrors "github.com/Shamba-Records-Limited/microvault/pkg/errors"
)

const defaultBaseURL = "https://aml-api.elliptic.co/v2"

// Config configures a Client. APIKey and APISecret are required; the
// secret is used exactly as issued (base64), never decoded before this
// package decodes it for signing.
type Config struct {
	APIKey    string
	APISecret string
	// BaseURL overrides defaultBaseURL — tests point this at an
	// httptest.Server.
	BaseURL string
	// Thresholds are the risk-score cut points ScreenAddress applies — see
	// Thresholds's doc comment on why they are config, not a constant.
	Thresholds Thresholds
}

// Client is Elliptic's AML API client. It is only a client: no
// persistence, no notifications, per pkg/payment/README.md's layering
// principles applied to this package too.
type Client struct {
	httpClient *http.Client
	baseURL    string
	thresholds Thresholds
}

// NewClient builds a Client whose requests are signed per sign.go.
func NewClient(cfg Config) *Client {
	baseURL := cfg.BaseURL
	if baseURL == "" {
		baseURL = defaultBaseURL
	}
	return &Client{
		httpClient: &http.Client{
			Timeout: 15 * time.Second,
			Transport: &signingTransport{
				apiKey: cfg.APIKey,
				secret: cfg.APISecret,
				base:   http.DefaultTransport,
			},
		},
		baseURL:    baseURL,
		thresholds: cfg.Thresholds,
	}
}

// signingTransport injects Elliptic's x-access-* headers into every
// request. Elliptic signs the request body (unlike the Fonbnk/YellowCard
// precedent this mirrors, which sign only method+path), so RoundTrip has
// to buffer the body to sign it before forwarding — see the source design
// doc §3 and §11.
type signingTransport struct {
	apiKey string
	secret string
	base   http.RoundTripper
}

func (t *signingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	payload := "{}"
	if req.Body != nil {
		b, err := io.ReadAll(req.Body)
		req.Body.Close()
		if err != nil {
			return nil, err
		}
		if len(b) > 0 {
			payload = string(b)
		}
		req.Body = io.NopCloser(bytes.NewReader(b))
		req.ContentLength = int64(len(b))
	}

	timestampMs := time.Now().UnixMilli()
	signature, err := sign(t.secret, timestampMs, req.Method, req.URL.Path, payload)
	if err != nil {
		return nil, ellipticErr("sign_request").Code(pkgErrors.CodeBuildFailed).
			Wrapf(err, "could not sign the request")
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-access-key", t.apiKey)
	req.Header.Set("x-access-sign", signature)
	req.Header.Set("x-access-timestamp", strconv.FormatInt(timestampMs, 10))

	return t.base.RoundTrip(req)
}

// call performs one request and decodes a successful response into T.
// Every endpoint in this package goes through here so the status-code
// classification (source design doc §4) is applied once.
func call[T any](ctx context.Context, c *Client, op, method, path string, body any) (T, error) {
	var zero T

	var reqBody io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return zero, ellipticErr(op).Code(pkgErrors.CodeEncodeFailed).
				Wrapf(err, "could not encode the request")
		}
		reqBody = bytes.NewReader(b)
	}

	httpReq, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, reqBody)
	if err != nil {
		return zero, ellipticErr(op).Code(pkgErrors.CodeBuildFailed).
			Wrapf(err, "could not build the request")
	}

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return zero, ellipticErr(op).Code(pkgErrors.CodeTransportFailed).
			Wrapf(err, "request did not complete")
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return zero, ellipticErr(op).Code(pkgErrors.CodeTransportFailed).
			Wrapf(err, "could not read the response body")
	}

	if err := classifyStatus(op, resp.StatusCode, respBody, resp.Header); err != nil {
		return zero, err
	}

	var out T
	if err := json.Unmarshal(respBody, &out); err != nil {
		return zero, ellipticErr(op).Code(pkgErrors.CodeDecodeFailed).
			Wrapf(err, "could not decode the response")
	}
	return out, nil
}
