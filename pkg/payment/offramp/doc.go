// Package offramp defines the off-ramp Provider contract that every
// provider in the platform satisfies.
//
// Provider is the single mandatory interface (ID + Initiate). Optional
// capabilities — StatusReader, Quoter, Directory, MobileMoneyDirectory,
// BalanceReporter — are separate small interfaces; consumers type-assert
// for what they need and providers implement only what they genuinely
// support.
//
// Request and Result carry only cross-provider fields. Provider-specific
// extras travel on typed payloads via the ProviderOptions / ProviderPayload
// marker interfaces; the concrete types live in each provider's own package.
//
// Registry resolves a Request to a concrete Provider, first by inspecting
// req.Options.ProviderID(), otherwise by aliasing req.PayoutMethod to a
// registered ProviderID. NoOpProvider in noop.go is a stand-in for tests.
package offramp
