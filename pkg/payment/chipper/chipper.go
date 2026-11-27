package chipper

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/samber/lo"
	"github.com/samber/oops"

	"github.com/Shamba-Records-Limited/microvault/pkg/telemetry"

	pkgErrors "github.com/Shamba-Records-Limited/microvault/pkg/errors"
)

// ProviderName identifies Chipper in logs and error attributes.
const ProviderName = "chipper"

// APIVersion is the version pinned on every request.
const APIVersion = "2026-02-20"

// Base URLs of the two stacks.
const (
	SandboxBaseURL    = "https://sandbox-api.platform.chipper.ai"
	ProductionBaseURL = "https://api.platform.chipper.ai"
)

const (
	testKeyPrefix = "sk_test_"
	liveKeyPrefix = "sk_live_"
)

const maxResponseBytes = 8 << 20

// HTTPClient is the subset of *http.Client the package uses.
type HTTPClient interface {
	Do(req *http.Request) (*http.Response, error)
}

// Config configures a Client. BaseURL defaults to SandboxBaseURL.
type Config struct {
	APIKey     string
	BaseURL    string
	HTTPClient HTTPClient
}

// Client calls the Chipper Platform API.
type Client struct {
	apiKey  string
	baseURL string
	http    HTTPClient
}

// New validates cfg and builds a Client. A key from one stack pointed at the
// other stack's URL is rejected, since every call would fail with 401.
func New(cfg Config) (*Client, error) {
	errb := chipperErr("new")

	if !strings.HasPrefix(cfg.APIKey, testKeyPrefix) && !strings.HasPrefix(cfg.APIKey, liveKeyPrefix) {
		return nil, errb.Code(pkgErrors.CodeInvalidConfig).
			With(pkgErrors.AttrDependency, "api key").
			Errorf("api key must start with %s or %s", testKeyPrefix, liveKeyPrefix)
	}

	baseURL := strings.TrimRight(lo.CoalesceOrEmpty(cfg.BaseURL, SandboxBaseURL), "/")
	if err := checkBaseURL(baseURL); err != nil {
		return nil, errb.Code(pkgErrors.CodeInvalidConfig).With("base_url", baseURL).Wrapf(err, "base url is not usable")
	}
	live := strings.HasPrefix(cfg.APIKey, liveKeyPrefix)
	switch {
	case live && baseURL == SandboxBaseURL:
		return nil, errb.Code(pkgErrors.CodeInvalidConfig).Errorf("live api key used against the sandbox url")
	case !live && baseURL == ProductionBaseURL:
		return nil, errb.Code(pkgErrors.CodeInvalidConfig).Errorf("test api key used against the production url")
	}

	c := &Client{apiKey: cfg.APIKey, baseURL: baseURL, http: cfg.HTTPClient}
	if c.http == nil {
		c.http = telemetry.Client(&http.Client{Timeout: 30 * time.Second})
	}
	return c, nil
}

// IsLive reports whether the client holds a production key.
func (c *Client) IsLive() bool { return strings.HasPrefix(c.apiKey, liveKeyPrefix) }

// checkBaseURL requires https, except for loopback hosts used by tests.
func checkBaseURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return err
	}
	if u.Host == "" {
		return errInvalidBaseURL
	}
	if u.Scheme == "https" {
		return nil
	}
	host := u.Hostname()
	if ip := net.ParseIP(host); u.Scheme == "http" && (host == "localhost" || (ip != nil && ip.IsLoopback())) {
		return nil
	}
	return errInvalidBaseURL
}

// Page is one page of a list endpoint.
type Page[T any] struct {
	Data       []T     `json:"data"`
	HasMore    bool    `json:"hasMore"`
	NextCursor *string `json:"nextCursor"`
}

// call sends one request and decodes a 2xx body into T, returning the status
// so callers can tell a create (201) from an idempotent replay (200).
func call[T any](ctx context.Context, c *Client, errb oops.OopsErrorBuilder, method, path string, query url.Values, body any) (result *T, status int, err error) {
	var reader io.Reader = http.NoBody
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return nil, 0, errb.Code(pkgErrors.CodeEncodeFailed).Wrapf(err, "could not encode the request")
		}
		reader = bytes.NewReader(encoded)
	}

	endpoint := c.baseURL + path
	if len(query) > 0 {
		endpoint += "?" + query.Encode()
	}

	req, err := http.NewRequestWithContext(ctx, method, endpoint, reader)
	if err != nil {
		return nil, 0, errb.Code(pkgErrors.CodeBuildFailed).Wrapf(err, "could not build the request")
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("chipper-version", APIVersion)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, 0, errb.Code(pkgErrors.CodeTransportFailed).Wrapf(err, "request did not complete")
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return nil, resp.StatusCode, parseError(errb, resp)
	}

	var out T
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxResponseBytes)).Decode(&out); err != nil {
		return nil, resp.StatusCode, errb.Code(pkgErrors.CodeDecodeFailed).
			With("request_id", resp.Header.Get("x-request-id")).
			Wrapf(err, "could not decode the response")
	}
	return &out, resp.StatusCode, nil
}

// pageQuery adds the shared paging parameters.
func pageQuery(q url.Values, limit int, cursor string) url.Values {
	if limit > 0 {
		q.Set("limit", strconv.Itoa(limit))
	}
	if cursor != "" {
		q.Set("cursor", cursor)
	}
	return q
}
