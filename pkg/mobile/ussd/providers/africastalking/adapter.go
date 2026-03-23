// Package africastalking implements the USSD provider adapter for AfricasTalking.
package africastalking

import (
	"context"
	"fmt"
	"strings"

	"github.com/Shamba-Records-Limited/microvault/pkg/mobile/ussd"
)

// AfricasTalkingUSSDAdapter implements the USSDProvider interface for Africa's Talking.
type AfricasTalkingUSSDAdapter struct {
	apiKey   string
	username string
}

// NewAfricasTalkingUSSDAdapter creates a new Africa's Talking USSD adapter.
func NewAfricasTalkingUSSDAdapter(username, apiKey string) *AfricasTalkingUSSDAdapter {
	return &AfricasTalkingUSSDAdapter{
		apiKey:   apiKey,
		username: username,
	}
}

// Compile-time check to ensure all methods are implemented.
var _ ussd.USSDProvider = (*AfricasTalkingUSSDAdapter)(nil)

// ParseRequest parses Africa's Talking USSD request parameters.
// Africa's Talking sends: sessionId, serviceCode, phoneNumber, text, networkCode
// Note: The "text" field accumulates all user inputs separated by "*"
// e.g., "input1*input2*input3" - we extract only the last segment as current input
func (a *AfricasTalkingUSSDAdapter) ParseRequest(ctx context.Context, data map[string]string) (*ussd.USSDRequest, error) {
	sessionID := data["sessionId"]
	if sessionID == "" {
		return nil, fmt.Errorf("missing sessionId")
	}

	phoneNumber := data["phoneNumber"]
	if phoneNumber == "" {
		return nil, fmt.Errorf("missing phoneNumber")
	}

	// Normalize phone number - remove leading +
	phoneNumber = strings.TrimPrefix(phoneNumber, "+")

	// Extract only the last input from accumulated text
	// Africa's Talking accumulates inputs as "input1*input2*input3"
	text := data["text"]
	currentInput := ""
	if text != "" {
		parts := strings.Split(text, "*")
		currentInput = parts[len(parts)-1] // Get only the last segment
	}

	return &ussd.USSDRequest{
		SessionID:    sessionID,
		PhoneNumber:  phoneNumber,
		Input:        currentInput,
		ServiceCode:  data["serviceCode"],
		NetworkCode:  data["networkCode"],
		ProviderData: data,
	}, nil
}

// FormatResponse formats the response for Africa's Talking.
// Africa's Talking expects: "CON message" or "END message"
func (a *AfricasTalkingUSSDAdapter) FormatResponse(ctx context.Context, response *ussd.USSDResponse) (any, error) {
	if response == nil {
		return nil, fmt.Errorf("response cannot be nil")
	}

	// Validate response type
	if response.Type != "CON" && response.Type != "END" {
		return nil, fmt.Errorf("invalid response type: %s", response.Type)
	}

	// Format: "CON message" or "END message"
	return fmt.Sprintf("%s %s", response.Type, response.Message), nil
}

// GetProviderName returns the provider name.
func (a *AfricasTalkingUSSDAdapter) GetProviderName() string {
	return "africastalking"
}

// ValidateRequest validates the Africa's Talking request.
func (a *AfricasTalkingUSSDAdapter) ValidateRequest(ctx context.Context, data map[string]string) error {
	requiredFields := []string{"sessionId", "phoneNumber", "serviceCode"}

	for _, field := range requiredFields {
		if data[field] == "" {
			return fmt.Errorf("missing required field: %s", field)
		}
	}

	// Validate phone number format (basic validation)
	phoneNumber := data["phoneNumber"]
	if !strings.HasPrefix(phoneNumber, "+") && !strings.HasPrefix(phoneNumber, "254") {
		return fmt.Errorf("invalid phone number format: %s", phoneNumber)
	}

	return nil
}
