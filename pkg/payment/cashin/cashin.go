package cashin

import (
	"context"
	"time"
)

// ProviderID identifies a concrete collection implementation in the registry.
type ProviderID string

// Known provider IDs. New providers should add their constant here.
const (
	ProviderMpesa     ProviderID = "mpesa"
	ProviderMoneyGram ProviderID = "moneygram"
)

// CollectionMethod selects how the payer is reached. Empty is treated as
// CollectionMethodPayBill.
type CollectionMethod string

const (
	// CollectionMethodPayBill is the borrower paying the paybill on their own
	// initiative, reconciled via C2B confirmation and Pull.
	CollectionMethodPayBill CollectionMethod = "paybill"

	// CollectionMethodPrompt is an M-Pesa Express STK prompt pushed to the
	// payer's handset.
	CollectionMethodPrompt CollectionMethod = "prompt"

	// CollectionMethodCash is a MoneyGram cash deposit: the borrower completes
	// KYC in an interactive webview and pays cash at an agent.
	CollectionMethodCash CollectionMethod = "cash"
)

// Collector is the single mandatory capability: open a collection against a
// loan. Optional behaviour is split into the capability interfaces below;
// consumers type-assert for what they need.
//
// The registry works in terms of Collector alone. Provider is retained as an
// alias for symmetry with offramp.Provider.
type Provider = Collector

type Collector interface {
	ID() ProviderID
	Collect(ctx context.Context, req Request) (*Result, error)
}

// Prompter pushes a payment request to the payer's handset. M-Pesa Express
// implements it; MoneyGram cannot.
type Prompter interface {
	Prompt(ctx context.Context, req PromptRequest) (*PromptResult, error)
}

// StatusReader resolves a collection's current state on demand.
type StatusReader interface {
	Status(ctx context.Context, ref ProviderRef) (*Status, error)
}

// Reconciler lists settled collections over a window, for rails that can be
// swept independently of their notifications.
type Reconciler interface {
	Reconcile(ctx context.Context, from, to time.Time) ([]Settlement, error)
}

// Reverser returns a collection to the payer.
type Reverser interface {
	Reverse(ctx context.Context, ref ProviderRef, amount int64, reason string) (*ReversalResult, error)
}

// PayerVerifier checks that a payer's number belongs to a claimed identity.
type PayerVerifier interface {
	VerifyPayer(ctx context.Context, msisdn, idType, idNumber string) (bool, error)
}

// Request contains the cross-provider data needed to open a collection.
// Provider-specific extras live on Options.
type Request struct {
	LoanID      string
	AmountMinor int64

	// Payer is the payer's phone number. Optional for rails that never reach
	// the payer directly (M-Pesa's passive paybill); required by rails that
	// do (MoneyGram sends the KYC webview link by SMS).
	Payer string

	// CollectionMethod is consulted by the registry when the caller does not
	// pin a provider via Options.
	CollectionMethod CollectionMethod

	// Options carries per-provider extras (mpesa.Options). nil is acceptable;
	// each adapter documents its expectations.
	Options ProviderOptions
}

// PromptRequest carries the inputs to push a payment request to the payer's
// handset.
type PromptRequest struct {
	LoanID           string
	Payer            string
	AmountKES        int64
	AccountReference string
	CallbackURL      string
}

// ProviderRef identifies an in-flight transaction enough to look it up.
// Extra is provider-scoped.
type ProviderRef struct {
	ID       string
	Provider ProviderID
	Extra    map[string]any
}

// Result is what Collector returns. Cross-provider summary fields stay on the
// struct; provider-specific output lives in Provider.
type Result struct {
	LoanID    string
	Reference string
	Amount    int64
	At        time.Time

	// Provider is the typed payload returned by the adapter. Consumers
	// type-assert to the concrete payload type owned by the provider package.
	Provider ProviderPayload
}

// PromptResult is what Prompter returns — identifiers for the pushed prompt,
// not a payment. A prompt that was accepted is not a payment.
type PromptResult struct {
	LoanID    string
	Reference string

	// Provider carries provider-specific identifiers (e.g. M-Pesa CheckoutRequestID).
	Provider ProviderPayload
}

// Status contains status information for an in-flight collection.
type Status struct {
	Reference string
	Succeeded bool
	Amount    int64
	At        time.Time

	// Provider carries provider-specific status detail.
	Provider ProviderPayload
}

// Settlement is one settled collection returned by a Reconciler.
type Settlement struct {
	Reference string
	Amount    int64
	At        time.Time

	// Provider.payload carries provider-specific settlement detail.
	Provider ProviderPayload
}

// ReversalResult reports the outcome of a reversal.
type ReversalResult struct {
	Status    string
	Amount    int64
	Reference string

	// Provider carries provider-specific reversal detail.
	Provider ProviderPayload
}

// ProviderOptions is the marker interface implemented by per-provider request
// extras. Callers attach a concrete implementation to Request.Options.
type ProviderOptions interface {
	ProviderID() ProviderID
}

// ProviderPayload is the marker interface implemented by per-provider result
// extras. Adapters set the respective Provider field to a concrete
// implementation; consumers type-assert to read provider-specific output.
type ProviderPayload interface {
	ProviderID() ProviderID
}
