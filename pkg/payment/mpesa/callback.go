package mpesa

// CallbackKind identifies which of the two URLs Daraja was given received a
// delivery.
//
// It is a parameter rather than something inferred from the payload because the
// payload cannot tell you: a queue-timeout body carries a ReferenceItem naming
// Safaricom's own internal listener, not the URL it was posted to. Register the
// result and timeout URLs as distinct routes and pass the kind that route was
// registered as.
type CallbackKind string

// The two deliveries.
const (
	CallbackResult  CallbackKind = "result"
	CallbackTimeout CallbackKind = "timeout"
)

// Outcome is what a delivery establishes about a transaction.
type Outcome string

// The three outcomes. There are three rather than two because "we do not know"
// is a real state and collapsing it into Failed is how money moves twice.
const (
	OutcomeSucceeded Outcome = "succeeded"
	OutcomeFailed    Outcome = "failed"
	OutcomeUnknown   Outcome = "unknown"
)

// Terminal reports whether the outcome needs no further resolution.
func (o Outcome) Terminal() bool { return o == OutcomeSucceeded || o == OutcomeFailed }

// Callback is a decoded asynchronous delivery.
type Callback struct {
	Kind    CallbackKind
	Outcome Outcome
	Result  *Result
}

// ParseCallback decodes a delivery and classifies it.
//
// A delivery to the queue-timeout URL is always OutcomeUnknown, whatever its
// body says. Daraja's wording — the request "timed out before processing" —
// invites reading it as a failure, but it establishes only that Daraja did not
// finish in time; the transaction may well have completed afterwards. Resolve
// an unknown with TransactionStatus. Never retry from one, and in particular
// never retry with a fresh OriginatorConversationID, which is what defeats the
// only server-side duplicate protection Daraja offers.
func ParseCallback(kind CallbackKind, raw []byte) (*Callback, error) {
	result, err := ParseResult(raw)
	if err != nil {
		return nil, err
	}

	callback := &Callback{Kind: kind, Result: result}
	switch {
	case kind == CallbackTimeout:
		callback.Outcome = OutcomeUnknown
	case result.Succeeded():
		callback.Outcome = OutcomeSucceeded
	default:
		callback.Outcome = OutcomeFailed
	}
	return callback, nil
}
