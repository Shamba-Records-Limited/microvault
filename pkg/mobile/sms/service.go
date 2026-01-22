package sms

import (
	"context"
	"fmt"
	"log"
)

// SMSRequest represents an SMS message request.
type SMSRequest struct {
	To              string
	ToMultiple      []string
	Message         string
	From            string
	ProviderOptions map[string]any // Provider-specific options
}

// SMSResponse represents an SMS message response.
type SMSResponse struct {
	ProviderData any // Provider-specific response data
}

// SMSProvider is an interface for sending SMS messages.
type SMSProvider interface {
	// SendSMS sends an SMS message using the provided context and request.
	SendSMS(ctx context.Context, req SMSRequest) (SMSResponse, error)

	// SendSingleSMS sends a single SMS message using the provided context, recipient, message, and sender.
	SendSingleSMS(ctx context.Context, to string, message string, from string) (SMSResponse, error)

	// SendBulkSMS sends a bulk SMS message using the provided context, recipients, message, and sender.
	SendBulkSMS(ctx context.Context, to []string, message string, from string) (SMSResponse, error)
}

// SMSService is a service for managing SMS providers.
type SMSService struct {
	providers map[string]SMSProvider
}

// NewSMSService creates a new SMS service.
func NewSMSService() *SMSService {
	return &SMSService{
		providers: make(map[string]SMSProvider),
	}
}

// RegisterProvider registers an SMS provider with the service.
func (s *SMSService) RegisterProvider(name string, provider SMSProvider) {
	log.Printf("Registering SMS provider: %s", name)
	s.providers[name] = provider
}

// GetProvider retrieves an SMS provider by name.
func (s *SMSService) GetProvider(name string) (SMSProvider, error) {
	provider, ok := s.providers[name]
	if !ok {
		return nil, fmt.Errorf("payment provider '%s' not found", name)
	}
	return provider, nil
}

// GetProviders retrieves all registered SMS providers.
func (s *SMSService) GetProviders() map[string]SMSProvider {
	return s.providers
}

// DeleteProvider deletes a registered SMS provider by name.
func (s *SMSService) DeleteProvider(name string) error {
	if _, ok := s.providers[name]; !ok {
		return fmt.Errorf("payment provider '%s' not found", name)
	}
	delete(s.providers, name)
	return nil
}

// DeleteProviders deletes all registered SMS providers.
func (s *SMSService) DeleteProviders() error {
	for name := range s.providers {
		if err := s.DeleteProvider(name); err != nil {
			return err
		}
	}
	return nil
}
