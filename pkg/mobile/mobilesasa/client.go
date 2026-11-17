package mobilesasa

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/Shamba-Records-Limited/microvault/pkg/telemetry"
)

// DefaultBaseURL is the production API host; the gateway has no sandbox.
const DefaultBaseURL = "https://api.mobilesasa.com"

// DefaultTimeout bounds one send.
const DefaultTimeout = 30 * time.Second

// MaxCallbackURL is the longest per-send callback URL the gateway accepts.
const MaxCallbackURL = 255

// Client sends SMS through the frozen v1/v2 API.
type Client struct {
	baseURL string
	token   string
	http    *http.Client
}

// NewClient builds a client. An empty baseURL uses DefaultBaseURL and a zero
// timeout uses DefaultTimeout.
func NewClient(baseURL, token string, timeout time.Duration) *Client {
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	transport := &http.Transport{
		DialContext:           (&net.Dialer{Timeout: 5 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
		TLSHandshakeTimeout:   5 * time.Second,
		ResponseHeaderTimeout: timeout,
		MaxIdleConns:          20,
		MaxIdleConnsPerHost:   10,
		IdleConnTimeout:       90 * time.Second,
		ForceAttemptHTTP2:     true,
	}
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		token:   token,
		http:    telemetry.Client(&http.Client{Timeout: timeout, Transport: transport}),
	}
}

// SendRequest is one SMS to one recipient. A TrackingID sends on v2, which
// echoes it in delivery reports and returns the parts billed.
type SendRequest struct {
	SenderID    string
	Phone       string
	Message     string
	TrackingID  string
	CallbackURL string
}

// SendResult is the gateway's acceptance of a send.
type SendResult struct {
	MessageID string
	Parts     int
}

// APIError is a send the gateway answered but did not accept.
type APIError struct {
	HTTPStatus int
	Code       string
	Message    string
}

func (e *APIError) Error() string {
	if e.Code != "" {
		return fmt.Sprintf("mobilesasa: %s (%s)", e.Message, e.Code)
	}
	return fmt.Sprintf("mobilesasa: HTTP %d: %s", e.HTTPStatus, e.Message)
}

// Throttled reports whether the gateway refused the request before
// processing it, so sending again cannot duplicate the message.
func (e *APIError) Throttled() bool { return e.HTTPStatus == http.StatusTooManyRequests }

// Account reports whether the failure is the account or token rather than
// the message, so every other send will fail the same way.
func (e *APIError) Account() bool {
	switch e.Code {
	case CodeUnauthorized, CodeForbidden:
		return true
	}
	return e.Code == "" && (e.HTTPStatus == http.StatusUnauthorized || e.HTTPStatus == http.StatusForbidden)
}

type sendBody struct {
	SenderID    string `json:"senderID"`
	Phone       string `json:"phone"`
	Message     string `json:"message"`
	TrackingID  string `json:"trackingId,omitempty"`
	CallbackURL string `json:"callbackUrl,omitempty"`
}

type envelope struct {
	Status       bool    `json:"status"`
	ResponseCode string  `json:"responseCode"`
	Message      string  `json:"message"`
	MessageID    string  `json:"messageId"`
	Cost         float64 `json:"cost"`
}

// Send submits one message. A nil error means the gateway queued it, not
// that it was delivered.
func (c *Client) Send(ctx context.Context, req SendRequest) (SendResult, error) {
	path := "/v1/send/message"
	if req.TrackingID != "" {
		path = "/v2/send/message"
	}
	payload, err := json.Marshal(sendBody(req))
	if err != nil {
		return SendResult{}, fmt.Errorf("mobilesasa: encode send: %w", err)
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, bytes.NewReader(payload))
	if err != nil {
		return SendResult{}, fmt.Errorf("mobilesasa: build send: %w", err)
	}
	httpReq.Header.Set("Authorization", "Bearer "+c.token)
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json")

	resp, err := c.http.Do(httpReq)
	if err != nil {
		return SendResult{}, fmt.Errorf("mobilesasa: send: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return SendResult{}, fmt.Errorf("mobilesasa: read send response: %w", err)
	}

	var env envelope
	if jsonErr := json.Unmarshal(body, &env); jsonErr != nil || env.ResponseCode == "" {
		return SendResult{}, &APIError{HTTPStatus: resp.StatusCode, Message: snippet(body)}
	}
	if !env.Status || env.ResponseCode != CodeAccepted {
		return SendResult{}, &APIError{HTTPStatus: resp.StatusCode, Code: env.ResponseCode, Message: env.Message}
	}
	if env.MessageID == "" {
		return SendResult{}, errors.New("mobilesasa: accepted send carried no messageId")
	}
	return SendResult{MessageID: env.MessageID, Parts: int(env.Cost)}, nil
}

func snippet(body []byte) string {
	s := strings.TrimSpace(string(body))
	if len(s) > 200 {
		s = s[:200]
	}
	return s
}
