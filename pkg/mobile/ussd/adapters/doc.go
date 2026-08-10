// Package adapters holds the USSD-to-domain glue: each file implements a
// narrow port interface declared elsewhere against the concrete user,
// account, off-ramp, and Stellar services.
//
// UserServiceAdapter satisfies ussd.UserService. It wraps user.Service,
// account.Service, and stellar.Service to register a user, derive their
// Stellar child account via BIP44/148, and return the user-with-accounts
// view the USSD handler expects.
//
// The off-ramp adapters — MoneyGramOffRampAdapter and YellowCardOffRampAdapter
// — satisfy offramp.Provider (the payment contract declared in
// pkg/payment/offramp). They translate a channel-agnostic offramp.Request into
// a provider-specific call and return an offramp.Result with a typed payload.
// They are registered in the offramp.Registry at boot and consumed by the
// credit module's LoanServiceAdapter, which is what actually satisfies
// ussd.LoanService. See pkg/payment/README.md for the contract and the
// registry-based routing.
//
// StellarTreasuryTransfer satisfies offramp.TreasuryTransfer against the
// Stellar service's SendUSDC path. It is consumed by the YellowCard adapter
// (for direct-settlement mode) and by the MoneyGram poller
// (pkg/services/mgpoller).
//
// The handlers in the parent ussd package never import these adapters'
// concrete types — they program against the port interfaces. Wiring happens
// once at boot in cmd/main.go.
package adapters
