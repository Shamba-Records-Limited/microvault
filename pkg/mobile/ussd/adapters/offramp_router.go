// Package adapters provides core implementation for routing off-ramp
// requests across multiple OffRampService providers based on PayoutMethod.
package adapters

import (
	"context"
	"fmt"

	"github.com/Shamba-Records-Limited/microvault/pkg/payment/offramp"
)

// RoutingOffRampService dispatches OffRampRequests to the underlying provider
// selected by OffRampRequest.PayoutMethod. It implements OffRampService so the
// existing single-provider injection point in the loan service adapter stays
// untouched.
//
// Routing rules:
//   - PayoutMethodMobileMoney → MobileMoney provider (e.g. YellowCard)
//   - PayoutMethodCashPickup  → CashPickup provider (e.g. MoneyGram)
//   - empty PayoutMethod      → MobileMoney provider (back-compat)
//
// For status / rate / network / balance queries that don't carry a
// PayoutMethod, the router prefers the MobileMoney provider; consumers needing
// MoneyGram-specific data should reach into the underlying adapter directly.
//
// At least one of MobileMoney or CashPickup must be set. If both are nil,
// constructor returns an error.
type RoutingOffRampService struct {
	MobileMoney offramp.Service
	CashPickup  offramp.Service
}

// NewRoutingOffRampService validates and returns a routing wrapper.
// At least one underlying provider must be non-nil.
func NewRoutingOffRampService(mobileMoney, cashPickup offramp.Service) (*RoutingOffRampService, error) {
	if mobileMoney == nil && cashPickup == nil {
		return nil, fmt.Errorf("offramp router: at least one of MobileMoney or CashPickup must be set")
	}
	return &RoutingOffRampService{
		MobileMoney: mobileMoney,
		CashPickup:  cashPickup,
	}, nil
}

var _ offramp.Service = (*RoutingOffRampService)(nil)

// pickProvider resolves the provider for a given PayoutMethod. Empty string
// is treated as PayoutMethodMobileMoney for back-compat with USSD code paths
// that don't set the field. Unknown values are an error.
func (r *RoutingOffRampService) pickProvider(method string) (offramp.Service, error) {
	switch method {
	case "", offramp.PayoutMethodMobileMoney:
		if r.MobileMoney == nil {
			return nil, fmt.Errorf("offramp router: mobile money provider not configured")
		}
		return r.MobileMoney, nil
	case offramp.PayoutMethodCashPickup:
		if r.CashPickup == nil {
			return nil, fmt.Errorf("offramp router: cash pickup provider not configured")
		}
		return r.CashPickup, nil
	default:
		return nil, fmt.Errorf("offramp router: unknown payout method %q", method)
	}
}

// queryProvider returns the provider for read-only queries that don't carry
// a PayoutMethod (status, exchange rate, networks, balance). Prefers
// MobileMoney; falls back to CashPickup if MobileMoney isn't configured.
func (r *RoutingOffRampService) queryProvider() offramp.Service {
	if r.MobileMoney != nil {
		return r.MobileMoney
	}
	return r.CashPickup
}

// InitiateOffRamp dispatches based on req.PayoutMethod.
func (r *RoutingOffRampService) InitiateOffRamp(ctx context.Context, req offramp.Request) (*offramp.Result, error) {
	provider, err := r.pickProvider(req.PayoutMethod)
	if err != nil {
		return nil, err
	}
	return provider.InitiateOffRamp(ctx, req)
}

// GetOffRampStatus delegates to the query provider. Status lookups by
// requestID alone cannot route — the caller must hit the right provider
// directly (or the poller in microvault-credit, which knows the provider
// from loans.ramp_provider).
func (r *RoutingOffRampService) GetOffRampStatus(ctx context.Context, requestID string) (*offramp.Status, error) {
	return r.queryProvider().GetOffRampStatus(ctx, requestID)
}

// GetSupportedProviders concatenates results from both underlying providers.
// Used by USSD to render the off-ramp menu.
func (r *RoutingOffRampService) GetSupportedProviders(ctx context.Context, countryCode string) ([]offramp.ProviderInfo, error) {
	var providers []offramp.ProviderInfo
	if r.MobileMoney != nil {
		mm, err := r.MobileMoney.GetSupportedProviders(ctx, countryCode)
		if err != nil {
			return nil, fmt.Errorf("mobile money providers: %w", err)
		}
		providers = append(providers, mm...)
	}
	if r.CashPickup != nil {
		cp, err := r.CashPickup.GetSupportedProviders(ctx, countryCode)
		if err != nil {
			// Cash pickup failure shouldn't break mobile-money menu rendering.
			// Log and continue would be ideal, but the interface returns error;
			// preserve that contract — caller decides.
			return nil, fmt.Errorf("cash pickup providers: %w", err)
		}
		providers = append(providers, cp...)
	}
	return providers, nil
}

// GetExchangeRate delegates to the query provider (mobile money preferred).
// MoneyGram-specific FX rates should be fetched by consumers via the SDK
// directly so they can compose the YC fallback per §7 of the integration plan.
func (r *RoutingOffRampService) GetExchangeRate(ctx context.Context, currency string) (*offramp.ExchangeRate, error) {
	return r.queryProvider().GetExchangeRate(ctx, currency)
}

// GetMobileMoneyNetworks delegates to the mobile money provider only —
// cash pickup providers always return empty.
func (r *RoutingOffRampService) GetMobileMoneyNetworks(ctx context.Context, countryCode string) ([]offramp.MobileMoneyNetwork, error) {
	if r.MobileMoney == nil {
		return []offramp.MobileMoneyNetwork{}, nil
	}
	return r.MobileMoney.GetMobileMoneyNetworks(ctx, countryCode)
}

// GetAvailableBalance delegates to the mobile money provider — cash pickup
// is funded per-transaction, not from a pre-funded balance.
func (r *RoutingOffRampService) GetAvailableBalance(ctx context.Context) (float64, error) {
	if r.MobileMoney == nil {
		return 0, nil
	}
	return r.MobileMoney.GetAvailableBalance(ctx)
}
