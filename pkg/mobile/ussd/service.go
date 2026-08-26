package ussd

import (
	"context"
	"log"

	pkgErrors "github.com/Shamba-Records-Limited/microvault/pkg/errors"
)

// NewUSSDService creates a new USSD service.
func NewUSSDService(handler *USSDHandler) *USSDService {
	return &USSDService{
		providers: make(map[string]USSDProvider),
		handler:   handler,
	}
}

// RegisterProvider registers a USSD provider with the service.
func (s *USSDService) RegisterProvider(name string, provider USSDProvider) {
	log.Printf("Registering USSD provider: %s", name)
	s.providers[name] = provider
}

// GetProvider retrieves a USSD provider by name.
func (s *USSDService) GetProvider(name string) (USSDProvider, error) {
	provider, ok := s.providers[name]
	if !ok {
		return nil, ussdErr("get_provider", nil).With(pkgErrors.AttrProvider, name).Code(pkgErrors.CodeNotFound).Errorf("USSD provider is not registered")
	}
	return provider, nil
}

// GetProviders retrieves all USSD providers.
func (s *USSDService) GetProviders() map[string]USSDProvider {
	return s.providers
}

// DeleteProvider removes a USSD provider from the service.
func (s *USSDService) DeleteProvider(name string) error {
	_, ok := s.providers[name]
	if !ok {
		return ussdErr("set_provider", nil).With(pkgErrors.AttrProvider, name).Code(pkgErrors.CodeNotFound).Errorf("USSD provider is not registered")
	}
	delete(s.providers, name)
	return nil
}

// DeleteAllProviders removes all USSD providers from the service.
func (s *USSDService) DeleteAllProviders() error {
	for name := range s.providers {
		if err := s.DeleteProvider(name); err != nil {
			return err
		}
	}
	return nil
}

// HandleRequest processes a USSD request using the specified provider.
func (s *USSDService) HandleRequest(ctx context.Context, providerName string, data map[string]string) (any, error) {
	// Get the provider
	provider, err := s.GetProvider(providerName)
	if err != nil {
		return nil, err
	}

	// Validate the request
	if err := provider.ValidateRequest(ctx, data); err != nil {
		return nil, ussdErr("handle_request", nil).Code(pkgErrors.CodeIncompleteResponse).Wrapf(err, "gateway request failed validation")
	}

	// Parse the request
	ussdReq, err := provider.ParseRequest(ctx, data)
	if err != nil {
		return nil, ussdErr("handle_request", nil).Code(pkgErrors.CodeDecodeFailed).Wrapf(err, "could not parse the gateway request")
	}

	// Handle the request using the USSD handler
	responseMessage, err := s.handler.HandleRequest(
		ctx,
		ussdReq.SessionID,
		ussdReq.PhoneNumber,
		ussdReq.ServiceCode,
		ussdReq.NetworkCode,
		ussdReq.Input,
	)
	if err != nil {
		return nil, ussdErr("handle_request", nil).Code(pkgErrors.CodeStateWriteFailed).Wrapf(err, "handler could not complete the turn")
	}

	// Parse response type and message
	ussdResp := parseHandlerResponse(responseMessage)

	// Format the response for the provider
	formattedResp, err := provider.FormatResponse(ctx, ussdResp)
	if err != nil {
		return nil, ussdErr("handle_request", nil).Code(pkgErrors.CodeEncodeFailed).Wrapf(err, "could not format the gateway response")
	}

	return formattedResp, nil
}

// parseHandlerResponse parses the handler's response string into a USSDResponse.
func parseHandlerResponse(response string) *USSDResponse {
	// Response format is "CON message" or "END message"
	if len(response) < 4 {
		return &USSDResponse{
			Type:    "END",
			Message: "Error processing request",
		}
	}

	responseType := response[:3]
	message := ""
	if len(response) > 4 {
		message = response[4:] // Skip "CON " or "END "
	}

	return &USSDResponse{
		Type:    responseType,
		Message: message,
	}
}
