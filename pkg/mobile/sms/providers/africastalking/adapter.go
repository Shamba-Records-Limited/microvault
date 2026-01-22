package africastalking

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/Shamba-Records-Limited/Microvault/pkg/mobile/sms"
)

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

// NewAfricasTalkingSMSAdapter creates a new Africa's Talking SMS adapter.
func NewAfricasTalkingSMSAdapter(username string, apiKey string, baseUrl string) *AfricaTalkingSMSAdapter {
	return &AfricaTalkingSMSAdapter{
		httpClient: &http.Client{
			Timeout: time.Second * 10,
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

	// Create HTTP request
	httpReq, err := http.NewRequestWithContext(
		ctx,
		"POST",
		a.baseUrl+"/messaging",
		strings.NewReader(data.Encode()),
	)
	if err != nil {
		return sms.SMSResponse{}, fmt.Errorf("failed to create request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	httpReq.Header.Set("apiKey", a.apiKey)
	httpReq.Header.Set("Accept", "application/json")

	// Execute request
	resp, err := a.httpClient.Do(httpReq)
	if err != nil {
		return sms.SMSResponse{}, fmt.Errorf("failed to send SMS: %w", err)
	}

	// Close response body
	defer func() {
		if err := resp.Body.Close(); err != nil {
			log.Printf("failed to close response body: %v", err)
		}
	}()

	// Read response
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return sms.SMSResponse{}, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return sms.SMSResponse{}, fmt.Errorf("SMS API error: %d - %s", resp.StatusCode, string(body))
	}

	// Parse response
	var smsResp AfricasTalkingSMSResponse
	if err := json.Unmarshal(body, &smsResp); err != nil {
		return sms.SMSResponse{}, fmt.Errorf("failed to parse response: %w", err)
	}

	return sms.SMSResponse{
		ProviderData: smsResp,
	}, nil
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
