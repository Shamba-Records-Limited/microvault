package airtel

import "strings"

// Status is the envelope every Kenya op-co product wraps its outcome in. It is
// the single most reused shape in the catalogue.
type Status struct {
	// Code is the HTTP status code, rendered as a string.
	Code string `json:"code"`

	Message string `json:"message"`
	Success bool   `json:"success"`

	// ResultCode is the legacy application code (ESB000010 and friends).
	// Airtel documents it as deprecated in favour of ResponseCode; it is kept
	// because it is what older products actually populate and because an
	// audit record should hold what arrived, not what should have.
	ResultCode string `json:"result_code"`

	// ResponseCode is the current product-specific code, e.g. DP00800001006.
	ResponseCode string `json:"response_code"`
}

// zero reports whether no envelope was present at all, as opposed to one
// reporting failure. A response that carries no status must not be read as an
// unsuccessful one.
func (s Status) zero() bool {
	return s.Code == "" && s.Message == "" && !s.Success && s.ResultCode == "" && s.ResponseCode == ""
}

// TransactionStatus is the outcome set shared by the wallet-movement cluster.
type TransactionStatus string

// The documented values. TE is Collection-enquiry only; TR belongs to Merchant
// Collection refunds and appears nowhere this package reaches.
const (
	StatusSuccess    TransactionStatus = "TS"
	StatusFailed     TransactionStatus = "TF"
	StatusAmbiguous  TransactionStatus = "TA"
	StatusInProgress TransactionStatus = "TIP"
	StatusExpired    TransactionStatus = "TE"
)

// StatusSource is where a TransactionStatus was read from. The callback
// vocabulary is a strict subset of the enquiry vocabulary — a callback carries
// only TS or TF — so a TF from a callback and a TF from an enquiry do not mean
// the same thing, and the source has to travel with the value.
type StatusSource string

// The two sources.
const (
	SourceCallback StatusSource = "callback"
	SourceEnquiry  StatusSource = "enquiry"
)

// Terminal reports whether the outcome can be acted on as final.
//
// A callback is documented as carrying intermediate or final status, so no
// callback value is terminal on its own — not even TF. Only an enquiry can
// close a transaction.
func (t TransactionStatus) Terminal(source StatusSource) bool {
	if source != SourceEnquiry {
		return false
	}
	switch t {
	case StatusSuccess, StatusFailed, StatusExpired:
		return true
	default:
		return false
	}
}

// ShouldEnquire reports whether the caller must ask again rather than act.
func (t TransactionStatus) ShouldEnquire(source StatusSource) bool {
	return !t.Terminal(source)
}

// Succeeded reports whether the transaction completed, which is the only state
// in which airtel_money_id is populated.
func (t TransactionStatus) Succeeded() bool { return t == StatusSuccess }

// Valid reports whether t is a documented value.
func (t TransactionStatus) Valid() bool {
	switch t {
	case StatusSuccess, StatusFailed, StatusAmbiguous, StatusInProgress, StatusExpired:
		return true
	default:
		return false
	}
}

// ParseTransactionStatus normalises a wire value. An unrecognised value is
// returned as-is and reports Valid false, so an undocumented status is visible
// rather than silently coerced into a failure.
func ParseTransactionStatus(value string) TransactionStatus {
	return TransactionStatus(strings.ToUpper(strings.TrimSpace(value)))
}
