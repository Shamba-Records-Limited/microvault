// Package mobilesasa implements the USSD provider adapter for Mobile Sasa's
// callback mode. The step contract matches Africa's Talking, except that the
// network arrives as a digit and the dialled code carries the subscriber's
// input; both are normalised here.
package mobilesasa

import (
	"context"
	"fmt"
	"strings"

	"github.com/samber/oops"

	pkgErrors "github.com/Shamba-Records-Limited/microvault/pkg/errors"
	"github.com/Shamba-Records-Limited/microvault/pkg/mobile/mobilesasa"
	"github.com/Shamba-Records-Limited/microvault/pkg/mobile/ussd"
	"github.com/Shamba-Records-Limited/microvault/pkg/phone"
)

// ProviderName is the name the adapter is registered under; it is the
// {provider} segment of the USSD callback URL.
const ProviderName = "mobilesasa"

func gatewayErr(op string) oops.OopsErrorBuilder {
	return oops.In(pkgErrors.DomainUSSD).
		Tags("ussd", "gateway").
		With(pkgErrors.AttrProvider, ProviderName).
		With(pkgErrors.AttrOperation, op)
}

// Adapter implements ussd.USSDProvider for Mobile Sasa.
type Adapter struct{}

// New builds the adapter.
func New() *Adapter { return &Adapter{} }

var _ ussd.USSDProvider = (*Adapter)(nil)

// ParseRequest reads one session step. Only the last *-separated segment of
// text is the current input.
func (a *Adapter) ParseRequest(_ context.Context, data map[string]string) (*ussd.USSDRequest, error) {
	sessionID := data["sessionId"]
	if sessionID == "" {
		return nil, gatewayErr("parse_request").
			Code(pkgErrors.CodeIncompleteResponse).
			With("field", "sessionId").
			Errorf("gateway request is missing a required field")
	}
	phoneNumber := strings.TrimPrefix(data["phoneNumber"], "+")
	if phoneNumber == "" {
		return nil, gatewayErr("parse_request").
			Code(pkgErrors.CodeMissingPhoneNumber).
			With("session_id", sessionID).
			Errorf("gateway request is missing a phone number")
	}

	text := data["text"]
	input := ""
	if text != "" {
		parts := strings.Split(text, "*")
		input = parts[len(parts)-1]
	}
	networkCode := mobilesasa.MobileNetworkCode(data["networkCode"])
	if networkCode == "" {
		networkCode = data["networkCode"]
	}

	return &ussd.USSDRequest{
		SessionID:    sessionID,
		PhoneNumber:  phoneNumber,
		Input:        input,
		ServiceCode:  mobilesasa.USSDServiceCode(data["serviceCode"], text),
		NetworkCode:  networkCode,
		ProviderData: data,
	}, nil
}

// FormatResponse renders "CON message" or "END message".
func (a *Adapter) FormatResponse(_ context.Context, response *ussd.USSDResponse) (any, error) {
	if response == nil {
		return nil, gatewayErr("format_response").
			Code(pkgErrors.CodeMissingDependency).
			Errorf("response cannot be nil")
	}
	if response.Type != "CON" && response.Type != "END" {
		return nil, gatewayErr("format_response").
			Code(pkgErrors.CodeIncompleteResponse).
			With("response_type", response.Type).
			Errorf("response type must be CON or END")
	}
	return fmt.Sprintf("%s %s", response.Type, response.Message), nil
}

// GetProviderName returns the provider name.
func (a *Adapter) GetProviderName() string { return ProviderName }

// ValidateRequest checks the fields every step carries.
func (a *Adapter) ValidateRequest(_ context.Context, data map[string]string) error {
	for _, field := range []string{"sessionId", "phoneNumber", "serviceCode"} {
		if data[field] == "" {
			return gatewayErr("validate_request").
				Code(pkgErrors.CodeIncompleteResponse).
				With("field", field).
				With("session_id", data["sessionId"]).
				Errorf("gateway request is missing a required field")
		}
	}
	if phone.KenyaE164(data["phoneNumber"]) == "" {
		return gatewayErr("validate_request").
			Code(pkgErrors.CodeInvalidAddress).
			With("phone_number", phone.Redact(data["phoneNumber"])).
			Errorf("phone number is not in a recognised format")
	}
	return nil
}
