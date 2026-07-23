package africastalking

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/Shamba-Records-Limited/microvault/pkg/mobile/sms"
)

// DefaultTimeout bounds a single send attempt. The Africa's Talking sandbox
// regularly takes well over 10s to answer an authenticated /messaging POST
// even though an unauthenticated probe returns in under a second, so the
// budget is sized for gateway processing rather than network latency.
const DefaultTimeout = 30 * time.Second

// maxSendAttempts caps total tries per message. The v1 messaging API has no
// idempotency key, so a timed-out attempt may still have been delivered and a
// retry can duplicate it; for notification SMS a duplicate is preferable to a
// silent loss.
const maxSendAttempts = 3

// AfricaTalkingSMSAdapter is an implementation of the SMSProvider interface for Africa's Talking SMS service.
type AfricaTalkingSMSAdapter struct {
	httpClient *http.Client
	username   string
	apiKey     string
	baseUrl    string
}

// AfricasTalkingSMSResponse represents the SMS API response
type AfricasTalkingSMSResponse struct {
	SMSMessageData struct {
		Message    string `json:"Message"`
		Recipients []struct {
			StatusCode int    `json:"statusCode"`
			Number     string `json:"number"`
			Status     string `json:"status"`
			Cost       string `json:"cost"`
			MessageID  string `json:"messageId"`
		} `json:"Recipients"`
	} `json:"SMSMessageData"`
}

// NewAfricasTalkingSMSAdapter creates a new Africa's Talking SMS adapter. A
// timeout of zero falls back to DefaultTimeout.
//
// The transport is tuned explicitly rather than using http.DefaultTransport so
// a stalled gateway is distinguishable from an unreachable one: dial and TLS
// failures surface in seconds, while ResponseHeaderTimeout absorbs the slow
// path. Idle connections are pooled and reused to keep a high send volume from
// exhausting outbound ports.
func NewAfricasTalkingSMSAdapter(username string, apiKey string, baseUrl string, timeout time.Duration) *AfricaTalkingSMSAdapter {
	if timeout <= 0 {
		timeout = DefaultTimeout
	}

	transport := &http.Transport{
		DialContext:           (&net.Dialer{Timeout: 5 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
		TLSHandshakeTimeout:   5 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
		ResponseHeaderTimeout: timeout,
		MaxIdleConns:          20,
		MaxIdleConnsPerHost:   10,
		IdleConnTimeout:       90 * time.Second,
		ForceAttemptHTTP2:     true,
	}

	return &AfricaTalkingSMSAdapter{
		httpClient: &http.Client{
			Timeout:   timeout,
			Transport: transport,
		},
		username: username,
		apiKey:   apiKey,
		baseUrl:  baseUrl,
	}
}

// Compile-time check to ensure all methods are implemented.
var _ sms.SMSProvider = (*AfricaTalkingSMSAdapter)(nil)

// SendSMS sends an SMS message using Africa's Talking SMS service.
func (a *AfricaTalkingSMSAdapter) SendSMS(ctx context.Context, req sms.SMSRequest) (sms.SMSResponse, error) {
	// Prepare form data
	data := url.Values{}
	data.Set("username", a.username)

	var recipients string
	if len(req.ToMultiple) > 0 {
		recipients = strings.Join(req.ToMultiple, ",")
	} else if req.To != "" {
		recipients = req.To
	} else {
		return sms.SMSResponse{}, fmt.Errorf("no recipients specified")
	}

	data.Set("to", recipients)
	data.Set("message", req.Message)
	if req.From != "" {
		data.Set("from", req.From)
	}
	encoded := data.Encode()

	var lastErr error
	for attempt := 1; attempt <= maxSendAttempts; attempt++ {
		if attempt > 1 {
			backoff := time.Duration(attempt-1) * time.Second
			select {
			case <-ctx.Done():
				return sms.SMSResponse{}, fmt.Errorf("send SMS abandoned after %d attempt(s): %w", attempt-1, lastErr)
			case <-time.After(backoff):
			}
		}

		resp, err := a.send(ctx, encoded)
		if err == nil {
			if attempt > 1 {
				slog.Info("sms: send succeeded after retry",
					slog.Int("attempt", attempt),
					slog.String("recipients", recipients),
				)
			}
			return resp, nil
		}
		lastErr = err

		if !retryable(err) {
			return sms.SMSResponse{}, err
		}

		slog.Warn("sms: send attempt failed, will retry",
			slog.Int("attempt", attempt),
			slog.Int("max_attempts", maxSendAttempts),
			slog.String("error", err.Error()),
		)
	}

	return sms.SMSResponse{}, fmt.Errorf("send SMS failed after %d attempts: %w", maxSendAttempts, lastErr)
}

// send performs a single POST to /messaging. Errors that warrant another try
// are wrapped in retryableError so the caller can tell them apart from a
// rejection that will fail identically on every attempt.
func (a *AfricaTalkingSMSAdapter) send(ctx context.Context, encoded string) (sms.SMSResponse, error) {
	httpReq, err := http.NewRequestWithContext(
		ctx,
		"POST",
		a.baseUrl+"/messaging",
		strings.NewReader(encoded),
	)
	if err != nil {
		return sms.SMSResponse{}, fmt.Errorf("failed to create request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	httpReq.Header.Set("apiKey", a.apiKey)
	httpReq.Header.Set("Accept", "application/json")

	start := time.Now()
	resp, err := a.httpClient.Do(httpReq)
	if err != nil {
		// The request context being cancelled is the caller giving up, not the
		// gateway failing; retrying it would only burn the remaining budget.
		if ctx.Err() != nil {
			return sms.SMSResponse{}, fmt.Errorf("failed to send SMS: %w", err)
		}
		return sms.SMSResponse{}, retryableError{fmt.Errorf("failed to send SMS after %s: %w", time.Since(start).Round(time.Millisecond), err)}
	}

	defer func() {
		if err := resp.Body.Close(); err != nil {
			slog.Warn("sms: failed to close response body", slog.String("error", err.Error()))
		}
	}()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return sms.SMSResponse{}, retryableError{fmt.Errorf("failed to read response: %w", err)}
	}

	slog.Debug("sms: gateway responded",
		slog.Int("status", resp.StatusCode),
		slog.Duration("latency", time.Since(start).Round(time.Millisecond)),
	)

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		apiErr := fmt.Errorf("SMS API error: %d - %s", resp.StatusCode, string(body))
		// 4xx other than rate limiting is a rejection: bad credentials, an
		// unregistered sender ID, insufficient credit. Retrying cannot fix it.
		if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500 {
			return sms.SMSResponse{}, retryableError{apiErr}
		}
		return sms.SMSResponse{}, apiErr
	}

	var smsResp AfricasTalkingSMSResponse
	if err := json.Unmarshal(body, &smsResp); err != nil {
		return sms.SMSResponse{}, fmt.Errorf("failed to parse response: %w", err)
	}

	return sms.SMSResponse{
		ProviderData: smsResp,
	}, nil
}

// retryableError marks a failure as worth another attempt.
type retryableError struct{ error }

func (e retryableError) Unwrap() error { return e.error }

func retryable(err error) bool {
	var r retryableError
	if errors.As(err, &r) {
		return true
	}
	var netErr net.Error
	return errors.As(err, &netErr) && netErr.Timeout()
}

// SendSingleSMS sends a single SMS message using Africa's Talking API.
func (a *AfricaTalkingSMSAdapter) SendSingleSMS(ctx context.Context, to string, message string, from string) (sms.SMSResponse, error) {
	return a.SendSMS(ctx, sms.SMSRequest{
		To:      to,
		Message: message,
		From:    from,
	})
}

// SendBulkSMS sends a bulk SMS message using Africa's Talking API.
func (a *AfricaTalkingSMSAdapter) SendBulkSMS(ctx context.Context, to []string, message string, from string) (sms.SMSResponse, error) {
	return a.SendSMS(ctx, sms.SMSRequest{
		ToMultiple: to,
		Message:    message,
		From:       from,
	})
}
