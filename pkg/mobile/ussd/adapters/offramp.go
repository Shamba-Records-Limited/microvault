// Package adapters provides core off-ramp interfaces and types for USSD integrations with off-ramp providers.
package adapters

import (
	"context"
	"time"
)

// OffRampService defines the interface for off-ramping funds from crypto to fiat.
type OffRampService interface {
	// InitiateOffRamp starts the off-ramp process after validating balance.
	InitiateOffRamp(ctx context.Context, req OffRampRequest) (*OffRampResult, error)

	// GetOffRampStatus checks the status of an off-ramp transaction.
	GetOffRampStatus(ctx context.Context, requestID string) (*OffRampStatus, error)

	// GetSupportedProviders returns available off-ramp providers for a region.
	GetSupportedProviders(ctx context.Context, countryCode string) ([]OffRampProvider, error)

	// GetExchangeRate returns the current exchange rate for a currency.
	GetExchangeRate(ctx context.Context, currency string) (*ExchangeRate, error)

	// GetMobileMoneyNetworks returns available MoMo networks for a country.
	GetMobileMoneyNetworks(ctx context.Context, countryCode string) ([]MobileMoneyNetwork, error)

	// GetAvailableBalance returns the available USD balance for disbursements.
	GetAvailableBalance(ctx context.Context) (float64, error)
}

// TreasuryTransfer defines the interface for sending USDC from treasury to an external address.
// This decouples the off-ramp adapter from the Stellar service for testability.
type TreasuryTransfer interface {
	// SendUSDC sends USDC from the treasury wallet to a destination Stellar address with a memo.
	// Returns the transaction hash on success.
	SendUSDC(ctx context.Context, destination string, memo string, amount int64) (txHash string, err error)
}

// OffRampRequest contains all data needed to initiate an off-ramp.
type OffRampRequest struct {
	LoanID           string
	UserID           string
	RecipientName    string
	AmountUSD        float64
	AmountStroops    int64 // Amount in stroops (USDC * 10^7) for direct settlement crypto transfer
	DestinationPhone string
	CountryCode      string
	IdempotencyKey   string
	NetworkCode      string
	NetworkName      string
	SettlementMethod string // "direct" (default) or "fiat"
}

// OffRampResult contains the result of initiating an off-ramp.
type OffRampResult struct {
	RequestID        string
	SequenceID       string
	Status           string
	AmountUSD        float64
	AmountLocal      float64
	LocalCurrency    string
	ExchangeRate     float64
	Fee              float64 // Total fee in USD
	FeeLocal         float64 // Total fee in local currency
	EstimatedTime    int
	CreatedAt        time.Time
	SettlementMethod string // "direct" or "fiat" — which mode was actually used
	StellarAddress   string // Stellar address (direct settlement only)
	StellarMemo      string // Stellar memo/walletTag (direct settlement only)
	StellarTxHash    string // USDC transfer tx hash (direct settlement only)
}

// OffRampStatus contains status information for an off-ramp.
type OffRampStatus struct {
	RequestID     string
	SequenceID    string
	Status        string
	AmountLocal   float64
	LocalCurrency string
	CompletedAt   *time.Time
	FailureReason *string
}

// OffRampProvider represents an available off-ramp provider.
type OffRampProvider struct {
	ID                      string
	Name                    string
	SupportedMethods        []string
	MinAmount               float64
	MaxAmount               float64
	Currency                string
	Status                  string
	FeeUSD                  float64
	FeeLocal                float64
	EstimatedSettlementTime int
}

// ExchangeRate contains current exchange rate info.
type ExchangeRate struct {
	FromCurrency string
	ToCurrency   string
	Rate         float64
	BuyRate      float64
	RateID       string
	Locale       string
	UpdatedAt    time.Time
}

// MobileMoneyNetwork represents a MoMo operator.
type MobileMoneyNetwork struct {
	ID     string
	Name   string
	Code   string
	Status string
}

// NoOpOffRampService is a placeholder implementation that returns mock data.
type NoOpOffRampService struct{}

var _ OffRampService = (*NoOpOffRampService)(nil)

// InitiateOffRamp returns a mock off-ramp result.
func (s *NoOpOffRampService) InitiateOffRamp(ctx context.Context, req OffRampRequest) (*OffRampResult, error) {
	method := req.SettlementMethod
	if method == "" {
		method = "direct"
	}
	return &OffRampResult{
		RequestID:        "mock_" + req.LoanID,
		SequenceID:       req.IdempotencyKey,
		Status:           "pending",
		AmountUSD:        req.AmountUSD,
		AmountLocal:      req.AmountUSD * 153.0,
		LocalCurrency:    "KES",
		ExchangeRate:     153.0,
		Fee:              0,
		EstimatedTime:    5,
		CreatedAt:        time.Now(),
		SettlementMethod: method,
	}, nil
}

// GetOffRampStatus returns a mock status.
func (s *NoOpOffRampService) GetOffRampStatus(ctx context.Context, requestID string) (*OffRampStatus, error) {
	return &OffRampStatus{
		RequestID: requestID,
		Status:    "pending",
	}, nil
}

// GetSupportedProviders returns an empty list.
func (s *NoOpOffRampService) GetSupportedProviders(ctx context.Context, countryCode string) ([]OffRampProvider, error) {
	return []OffRampProvider{}, nil
}

// GetExchangeRate returns a mock rate.
func (s *NoOpOffRampService) GetExchangeRate(ctx context.Context, currency string) (*ExchangeRate, error) {
	return &ExchangeRate{
		FromCurrency: "USD",
		ToCurrency:   currency,
		Rate:         153.0,
		UpdatedAt:    time.Now(),
	}, nil
}

// GetMobileMoneyNetworks returns an empty list.
func (s *NoOpOffRampService) GetMobileMoneyNetworks(ctx context.Context, countryCode string) ([]MobileMoneyNetwork, error) {
	return []MobileMoneyNetwork{}, nil
}

// GetAvailableBalance returns a mock balance.
func (s *NoOpOffRampService) GetAvailableBalance(ctx context.Context) (float64, error) {
	return 1000000.0, nil
}
