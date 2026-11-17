package sms

import (
	"context"
	"fmt"
	"log/slog"
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
	Recipients   []Recipient
}

// Recipient is the gateway's verdict on one recipient of a send. StatusCode
// uses Africa's Talking's per-recipient codes (1xx accepted, 4xx and 5xx
// rejected); other providers map their verdicts onto them.
type Recipient struct {
	Number     string
	MessageID  string
	Status     string
	StatusCode int
	Cost       string
}

// Accepted reports whether the gateway took the message for delivery.
func (r Recipient) Accepted() bool { return r.StatusCode >= 100 && r.StatusCode < 200 }

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
	slog.Info("Registering SMS provider", slog.String("name", name))
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
