package cashin

import (
	"context"
)

// NoOpProvider is a minimal Provider stub for tests and bootstrap wiring.
// It returns a mock Result and nothing else — callers that need richer
// behaviour should depend on a real provider or a hand-rolled fake.
type NoOpProvider struct{}

var _ Provider = (*NoOpProvider)(nil)

// ID returns a sentinel provider ID.
func (NoOpProvider) ID() ProviderID { return ProviderID("noop") }

// Collect returns an empty mock result.
func (NoOpProvider) Collect(_ context.Context, req Request) (*Result, error) {
	return &Result{}, nil
}
