package offramp

import (
	"context"
	"time"
)

// PayoutMethod selects which provider handles a request when multiple are
// wired in via the routing service.
//
//   - PayoutMethodMobileMoney → YellowCard (default for back-compat)
//   - PayoutMethodCashPickup  → MoneyGram
//
// Empty string is treated as PayoutMethodMobileMoney by the router so
// existing call paths that don't set the field continue to disburse via YC.
const (
	PayoutMethodMobileMoney = "mobile_money"
	PayoutMethodCashPickup  = "cash_pickup"
)

// Service is the composite off-ramp contract every provider satisfies today.
// Phase 2 will split this into capability interfaces (Provider, Quoter,
// StatusReader, Directory, MobileMoneyDirectory, BalanceReporter); for now
// the fat interface keeps the relocation mechanical.
type Service interface {
	InitiateOffRamp(ctx context.Context, req Request) (*Result, error)
	GetOffRampStatus(ctx context.Context, requestID string) (*Status, error)
	GetSupportedProviders(ctx context.Context, countryCode string) ([]ProviderInfo, error)
	GetExchangeRate(ctx context.Context, currency string) (*ExchangeRate, error)
	GetMobileMoneyNetworks(ctx context.Context, countryCode string) ([]MobileMoneyNetwork, error)
	GetAvailableBalance(ctx context.Context) (float64, error)
}

// TreasuryTransfer abstracts sending USDC from the custodial treasury to an
// external Stellar address. Implemented by Stellar service adapters; injected
// into off-ramp providers that need to settle on-chain.
type TreasuryTransfer interface {
	SendUSDC(ctx context.Context, destination string, memo string, amount int64) (txHash string, err error)
}

// Request contains all data needed to initiate an off-ramp. Provider-specific
// extras live alongside the common fields for now (phase 3 will move them
// into a typed Options payload).
type Request struct {
	LoanID           string
	UserID           string
	RecipientName    string
	AmountUSD        float64
	AmountStroops    int64 // USDC in stroops (USDC * 10^7) for direct settlement
	DestinationPhone string
	CountryCode      string
	IdempotencyKey   string
	NetworkCode      string
	NetworkName      string
	SettlementMethod string // "direct" (default) or "fiat" — YC-specific

	// PayoutMethod selects the provider (see PayoutMethod* constants). Empty
	// defaults to mobile money. Unknown values produce an error from the router.
	PayoutMethod string

	// Cash-pickup-only (MoneyGram). Ignored by mobile-money providers.
	BirthDate         string // ISO-8601 (YYYY-MM-DD); SEP-9 KYC prefill
	ChildAccountIndex uint32 // per-user Stellar derivation index for SEP-10 memo
}

// Result is what InitiateOffRamp returns. Provider-specific fields stay on
// this struct in phase 1; phase 3 moves them into a typed payload.
type Result struct {
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
	SettlementMethod string // "direct"/"fiat" (YC) or "cash_pickup" (MG)

	// YellowCard direct-settlement fields. Empty for MoneyGram.
	StellarAddress string
	StellarMemo    string
	StellarTxHash  string

	// MoneyGram cash-pickup fields. Empty for YellowCard.
	InteractiveURL    string
	ExternalReference string
	ChildAccountMemo  int64
}

// Status contains status information for an off-ramp.
type Status struct {
	RequestID     string
	SequenceID    string
	Status        string
	AmountLocal   float64
	LocalCurrency string
	CompletedAt   *time.Time
	FailureReason *string
}

// ProviderInfo describes an available off-ramp provider for a country.
type ProviderInfo struct {
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
