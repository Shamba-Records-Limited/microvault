package mpesa

// OutcomeKind is the class of an asynchronous result, so an operator can act on
// it without reading Safaricom's description.
type OutcomeKind string

// The outcome kinds.
const (
	// OutcomeSuccess is ResultCode "0" and nothing else.
	OutcomeSuccess OutcomeKind = "success"

	// OutcomeTransient is a failure that may succeed if retried after a wait —
	// throttling, overload, maintenance. It is never a reason to change the
	// request.
	OutcomeTransient OutcomeKind = "transient"

	// OutcomeConfig is a request we built wrong or sent to the wrong product —
	// a bad parameter, a missing field, a shortcode without the product. It
	// never succeeds by retrying unchanged.
	OutcomeConfig OutcomeKind = "config"

	// OutcomePermission is a missing initiator role or product assignment on
	// the M-PESA Org portal. It is granted out of band and never resolves by
	// retrying.
	OutcomePermission OutcomeKind = "permission"

	// OutcomeCredential is a wrong, unresolvable, or locked initiator
	// credential. It stops every Initiator-bearing endpoint at once.
	OutcomeCredential OutcomeKind = "credential"

	// OutcomeOperational is a real condition on our accounts — insufficient
	// float, an inactive shortcode, a transaction already reversed.
	OutcomeOperational OutcomeKind = "operational"
)

// AsyncOutcome is what an asynchronous result code means to an operator. It is
// the operator-side analogue of ExpressOutcome: enough context to act, without
// reading Safaricom's prose.
type AsyncOutcome struct {
	Kind OutcomeKind

	// Retryable reports whether the identical request may succeed if resent.
	// For a request with a client-supplied idempotency key this means reusing
	// the same key, never minting a fresh one.
	Retryable bool

	// Message is a short operator-facing summary.
	Message string
}

// ResultFamily identifies which API produced an asynchronous result. Result
// codes are a per-family namespace — 2001 is a credential failure on Reversal
// and a wrong customer PIN on M-Pesa Express — so classifying requires knowing
// which endpoint the result came from.
type ResultFamily string

// The result families with package-level outcome maps.
const (
	FamilyReversal ResultFamily = "reversal"
	FamilyStatus   ResultFamily = "transaction_status"
	FamilyBalance  ResultFamily = "account_balance"
)

// initiatorOutcomes are the ApiResult codes shared by every Initiator-bearing
// endpoint — Reversal, Transaction Status, Account Balance, and the B2C/B2B
// disbursement commands. They are Safaricom's own initiator and
// request-processing codes, not endpoint-specific.
var initiatorOutcomes = map[string]AsyncOutcome{
	"15":          {Kind: OutcomeConfig, Retryable: false, Message: "Duplicate originator conversation ID. This request was already sent; resolve the original, do not resend."},
	"17":          {Kind: OutcomeTransient, Retryable: true, Message: "Daraja internal failure. Retry."},
	"18":          {Kind: OutcomeCredential, Retryable: false, Message: "Initiator credential check failed: wrong password or a credential encrypted with the wrong certificate."},
	"19":          {Kind: OutcomeTransient, Retryable: true, Message: "Message sequencing failure. Retry."},
	"20":          {Kind: OutcomeCredential, Retryable: false, Message: "Initiator username not found on M-PESA. Check the operator name."},
	"21":          {Kind: OutcomePermission, Retryable: false, Message: "Initiator lacks the role for this API on the M-PESA Org portal. Grant it out of band."},
	"22":          {Kind: OutcomePermission, Retryable: false, Message: "Initiator is not active for this receiving party. Check the operator assignment."},
	"24":          {Kind: OutcomeConfig, Retryable: false, Message: "Missing mandatory fields. The request was malformed."},
	"25":          {Kind: OutcomeConfig, Retryable: false, Message: "Invalid request parameters. A field failed validation."},
	"26":          {Kind: OutcomeTransient, Retryable: true, Message: "Traffic blocking condition in place. Back off and retry."},
	"29":          {Kind: OutcomeConfig, Retryable: false, Message: "Invalid command ID."},
	"100000000":   {Kind: OutcomeTransient, Retryable: true, Message: "Request cached, awaiting resend. Retry."},
	"100000001":   {Kind: OutcomeTransient, Retryable: true, Message: "Daraja system overload. Back off and retry."},
	"100000002":   {Kind: OutcomeTransient, Retryable: true, Message: "Throttled. Back off and retry."},
	"100000004":   {Kind: OutcomeTransient, Retryable: true, Message: "Daraja internal server error. Retry."},
	"100000005":   {Kind: OutcomeConfig, Retryable: false, Message: "Invalid input value."},
	"100000007":   {Kind: OutcomeTransient, Retryable: true, Message: "Daraja service status abnormal. Retry."},
	"100000009":   {Kind: OutcomeTransient, Retryable: true, Message: "Daraja API status abnormal. Retry."},
	"100000010":   {Kind: OutcomePermission, Retryable: false, Message: "Insufficient permissions for this request."},
	"100000011":   {Kind: OutcomeTransient, Retryable: true, Message: "Request rate limit exceeded. Back off and retry."},
	"00.002.1001": {Kind: OutcomeTransient, Retryable: true, Message: "Daraja is under maintenance. Retry later."},
}

// reversalOutcomes are the codes specific to a Reversal result, checked before
// the shared initiator set.
var reversalOutcomes = map[string]AsyncOutcome{
	"1":       {Kind: OutcomeOperational, Retryable: false, Message: "The shortcode lacks the funds to reverse. Top up the float, then retry."},
	"11":      {Kind: OutcomeOperational, Retryable: false, Message: "The shortcode account is not active."},
	"21":      {Kind: OutcomePermission, Retryable: false, Message: "Initiator lacks the Org Reversals Initiator role. Grant it on the Org portal."},
	"2001":    {Kind: OutcomeCredential, Retryable: false, Message: "Initiator information is invalid."},
	"2006":    {Kind: OutcomeOperational, Retryable: false, Message: "The shortcode account status does not allow this transaction."},
	"2028":    {Kind: OutcomePermission, Retryable: false, Message: "The shortcode has no permission to perform reversals. Product not assigned."},
	"8006":    {Kind: OutcomeCredential, Retryable: false, Message: "The initiator credential is locked. A Business Administrator must unlock it."},
	"R000001": {Kind: OutcomeOperational, Retryable: false, Message: "This transaction has already been reversed. Do not resend."},
	"R000002": {Kind: OutcomeConfig, Retryable: false, Message: "The original transaction ID is invalid or does not exist on M-PESA."},
}

// AsyncOutcomeFor classifies an asynchronous result code for a result family.
//
// The family-specific map is checked first, then the shared initiator set. An
// undocumented code falls back to operational and non-retryable, so an unknown
// failure is never retried blindly.
func AsyncOutcomeFor(family ResultFamily, code string) AsyncOutcome {
	if code == "0" {
		return AsyncOutcome{Kind: OutcomeSuccess, Retryable: false, Message: "Processed successfully."}
	}
	if family == FamilyReversal {
		if outcome, ok := reversalOutcomes[code]; ok {
			return outcome
		}
	}
	if outcome, ok := initiatorOutcomes[code]; ok {
		return outcome
	}
	return AsyncOutcome{Kind: OutcomeOperational, Retryable: false, Message: "Daraja reported an undocumented failure."}
}

// Outcome classifies this result for the given family.
func (r Result) Outcome(family ResultFamily) AsyncOutcome {
	return AsyncOutcomeFor(family, r.ResultCode)
}
