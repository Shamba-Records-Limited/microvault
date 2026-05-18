package offramp

import (
	"context"
	"time"
)

// NoOpService is a placeholder implementation that returns mock data. Useful
// for tests and bootstrap wiring before real providers are configured.
type NoOpService struct{}

var _ Service = (*NoOpService)(nil)

// InitiateOffRamp returns a mock off-ramp result.
func (s *NoOpService) InitiateOffRamp(_ context.Context, req Request) (*Result, error) {
	method := req.SettlementMethod
	if method == "" {
		method = "direct"
	}
	return &Result{
		RequestID:        "mock_" + req.LoanID,
		SequenceID:       req.IdempotencyKey,
		Status:           "pending",
		AmountUSD:        req.AmountUSD,
		AmountLocal:      req.AmountUSD * 153.0,
		LocalCurrency:    "KES",
		ExchangeRate:     153.0,
		EstimatedTime:    5,
		CreatedAt:        time.Now(),
		SettlementMethod: method,
	}, nil
}

// GetOffRampStatus returns a mock status.
func (s *NoOpService) GetOffRampStatus(_ context.Context, requestID string) (*Status, error) {
	return &Status{RequestID: requestID, Status: "pending"}, nil
}

// GetSupportedProviders returns an empty list.
func (s *NoOpService) GetSupportedProviders(_ context.Context, _ string) ([]ProviderInfo, error) {
	return []ProviderInfo{}, nil
}

// GetExchangeRate returns a mock rate.
func (s *NoOpService) GetExchangeRate(_ context.Context, currency string) (*ExchangeRate, error) {
	return &ExchangeRate{
		FromCurrency: "USD",
		ToCurrency:   currency,
		Rate:         153.0,
		UpdatedAt:    time.Now(),
	}, nil
}

// GetMobileMoneyNetworks returns an empty list.
func (s *NoOpService) GetMobileMoneyNetworks(_ context.Context, _ string) ([]MobileMoneyNetwork, error) {
	return []MobileMoneyNetwork{}, nil
}

// GetAvailableBalance returns a mock balance.
func (s *NoOpService) GetAvailableBalance(_ context.Context) (float64, error) {
	return 1000000.0, nil
}
