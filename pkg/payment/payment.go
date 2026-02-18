package payment

import (
	"context"
	"fmt"
	"log"
)

// PaymentStatus defines the possible states of a payment.
type PaymentStatus string

// Define constants for payment statuses.
const (
	StatusPending   PaymentStatus = "PENDING"
	StatusSucceeded PaymentStatus = "SUCCEEDED"
	StatusFailed    PaymentStatus = "FAILED"
)

// InitializePaymentRequest contains parameters for initializing a payment.
type InitializePaymentRequest struct {
	Amount int64
	// The caller will put a fonbnk.Options struct or yellowcard.Options struct here.
	ProviderOptions any
}

// PaymentProvider defines the abstract behavior of a payment service.
type PaymentProvider interface {
	// GetRate returns the exchange rate for a specified currency depending on the provider
	GetRate(ctx context.Context, currency string) (int64, error)

	// InitializePayment initiates a payment and returns a transaction ID or error.
	InitializePayment(ctx context.Context, req InitializePaymentRequest) (string, error)

	// ProcessPayment processes a payment for a given transaction ID.
	ProcessPayment(ctx context.Context, transactionID string) (PaymentStatus, error)

	// GetPaymentStatus retrieves the status of a payment.
	GetPaymentStatus(ctx context.Context, transactionID string) (PaymentStatus, error)
}

// Service acts as a registry for all available payment providers.
type Service struct {
	providers map[string]PaymentProvider
}

// NewService creates a new, empty payment service.
func NewService() *Service {
	return &Service{
		providers: make(map[string]PaymentProvider),
	}
}

// RegisterProvider adds a new payment provider to the registry.
func (s *Service) RegisterProvider(name string, provider PaymentProvider) {
	log.Printf("Registering payment provider: %s", name)
	s.providers[name] = provider
}

// Get retrieves a specific provider from the registry by its name.
func (s *Service) Get(name string) (PaymentProvider, error) {
	provider, ok := s.providers[name]
	if !ok {
		return nil, fmt.Errorf("payment provider '%s' not found", name)
	}
	return provider, nil
}
