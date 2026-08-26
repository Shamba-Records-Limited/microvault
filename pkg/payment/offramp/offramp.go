package offramp

import (
	"context"
	"time"
)

// PayoutMethod selects which provider handles a request when multiple are
// wired in via the registry. Empty string is treated as PayoutMethodMobileMoney.
const (
	PayoutMethodMobileMoney = "mobile_money"
	PayoutMethodCashPickup  = "cash_pickup"
)

// ProviderID identifies a concrete off-ramp implementation in the registry.
type ProviderID string

// Known provider IDs. New providers should add their constant here.
const (
	ProviderYellowCard ProviderID = "yellowcard"
	ProviderMoneyGram  ProviderID = "moneygram"
	ProviderFonbnk     ProviderID = "fonbnk"
)

// Provider is the single mandatory capability — every off-ramp must
// register and initiate. Optional behaviour is split into the other
// capability interfaces below; consumers type-assert for what they need.
type Provider interface {
	ID() ProviderID
	Initiate(ctx context.Context, req Request) (*Result, error)
}

// StatusReader looks up an in-flight off-ramp by ProviderRef. ProviderRef
// carries the transaction ID plus provider-scoped extras (e.g. MoneyGram
// needs the child memo to authenticate the SEP-10 session).
type StatusReader interface {
	Status(ctx context.Context, ref ProviderRef) (*Status, error)
}

// Quoter exposes the provider's view of the FX rate for a corridor. The
// quote response may be cached upstream; the orchestration of multi-source
// quoting lives outside this interface.
type Quoter interface {
	Quote(ctx context.Context, q QuoteRequest) (*ExchangeRate, error)
}

// Directory advertises which provider configurations are available for a
// given country (e.g. YC's per-channel info, MG's cash-pickup option).
type Directory interface {
	SupportedProviders(ctx context.Context, countryCode string) ([]ProviderInfo, error)
}

// MobileMoneyDirectory lists the MoMo operators available in a country.
// Only implemented by providers that actually operate on mobile-money rails.
type MobileMoneyDirectory interface {
	Networks(ctx context.Context, countryCode string) ([]MobileMoneyNetwork, error)
}

// BalanceReporter reports a pre-funded balance held with the provider. Not
// implemented by providers that settle per-transaction (e.g. MoneyGram).
type BalanceReporter interface {
	AvailableBalance(ctx context.Context) (float64, error)
}

// ProviderRef identifies an in-flight transaction enough to look it up.
// Extra is provider-scoped; document the expected keys in each provider's
// adapter so callers know what to pass.
type ProviderRef struct {
	ID       string
	Provider ProviderID
	Extra    map[string]any
}

// QuoteRequest carries the inputs a provider needs to quote a corridor.
// Future fields (amount, originating country, service option) can be added
// without breaking the interface.
type QuoteRequest struct {
	Currency string
}

// ProviderOptions is the marker interface implemented by per-provider request
// extras (e.g. yellowcard.Options.SettlementMethod, moneygram.Options.BirthDate).
// Callers attach a concrete implementation to Request.Options; the receiving
// adapter type-asserts to its own concrete type.
type ProviderOptions interface {
	ProviderID() ProviderID
}

// ProviderPayload is the marker interface implemented by per-provider result
// extras (e.g. yellowcard.DirectSettlementPayload, moneygram.CashPickupPayload).
// Adapters set Result.Provider to a concrete implementation; consumers type-
// assert to read provider-specific output.
type ProviderPayload interface {
	ProviderID() ProviderID
}

// TreasuryTransfer abstracts sending USDC from the custodial treasury to an
// external Stellar address. Implemented by Stellar service adapters; injected
// into off-ramp providers that need to settle on-chain.
type TreasuryTransfer interface {
	SendUSDC(ctx context.Context, destination string, memo string, amount int64) (txHash string, err error)
	CheckUSDCTrustline(ctx context.Context, address string) (hasTrustline bool, err error)
}

// Request contains the cross-provider data needed to initiate an off-ramp.
// Provider-specific extras live on Options.
type Request struct {
	LoanID           string
	UserID           string
	RecipientName    string
	AmountUSD        float64
	AmountStroops    int64
	DestinationPhone string
	CountryCode      string
	IdempotencyKey   string
	NetworkCode      string
	NetworkName      string

	// PayoutMethod is consulted by the registry to pick a provider when the
	// caller doesn't pin one via Options. Empty defaults to mobile money.
	PayoutMethod string

	// Options carries per-provider extras (yellowcard.Options,
	// moneygram.Options). nil is acceptable; each adapter documents its
	// expectations for missing/wrong types.
	Options ProviderOptions
}

// Result is what Provider.Initiate returns. Cross-provider summary fields
// stay on the struct; provider-specific output lives in Provider.
type Result struct {
	RequestID        string
	SequenceID       string
	Status           string
	AmountUSD        float64
	AmountLocal      float64
	LocalCurrency    string
	ExchangeRate     float64
	Fee              float64
	FeeLocal         float64
	EstimatedTime    int
	CreatedAt        time.Time
	SettlementMethod string // tag for downstream persistence: "direct"|"fiat"|"cash_pickup"

	// Provider is the typed payload returned by the adapter. Consumers
	// type-assert to the concrete payload type owned by the provider package.
	Provider ProviderPayload
}

// Status contains status information for an in-flight off-ramp.
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

// ExchangeRate is the canonical quote shape returned by Quoter.
type ExchangeRate struct {
	FromCurrency string
	ToCurrency   string
	SellRate     float64
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
