package offramp

import (
	"context"
	"time"
)

// NoOpProvider is a minimal Provider stub for tests and bootstrap wiring.
// It returns a mock Result and nothing else — callers that need richer
// behaviour should depend on a real provider or a hand-rolled fake.
type NoOpProvider struct{}

var _ Provider = (*NoOpProvider)(nil)

// ID returns a sentinel provider ID.
func (NoOpProvider) ID() ProviderID { return ProviderID("noop") }

// Initiate returns a mock off-ramp result.
func (NoOpProvider) Initiate(_ context.Context, req Request) (*Result, error) {
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
		SettlementMethod: "direct",
	}, nil
}
