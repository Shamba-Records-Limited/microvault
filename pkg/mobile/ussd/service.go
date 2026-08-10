package ussd

import (
	"context"
	"fmt"
	"log"
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
		return nil, fmt.Errorf("USSD provider '%s' not found", name)
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
		return fmt.Errorf("USSD provider '%s' not found", name)
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
		return nil, fmt.Errorf("invalid request: %w", err)
	}

	// Parse the request
	ussdReq, err := provider.ParseRequest(ctx, data)
	if err != nil {
		return nil, fmt.Errorf("failed to parse request: %w", err)
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
		return nil, fmt.Errorf("failed to handle request: %w", err)
	}

	// Parse response type and message
	ussdResp := parseHandlerResponse(responseMessage)

	// Format the response for the provider
	formattedResp, err := provider.FormatResponse(ctx, ussdResp)
	if err != nil {
		return nil, fmt.Errorf("failed to format response: %w", err)
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
