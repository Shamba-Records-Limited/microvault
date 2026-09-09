// Package darajastub is an in-process Daraja.
//
// It exists because the house pattern of a per-test http.HandlerFunc cannot
// express what this integration needs to be tested for. Most of Daraja is
// asynchronous: an endpoint acknowledges and then posts a result to a URL the
// caller supplied, so a handler that returns the acknowledgement tests half the
// code and none of the interesting half. The assertions that matter are about
// state — whether a balance moved, whether a shortcode is registered, whether a
// token was superseded — and a stateless handler cannot hold state.
//
// It is a normal package rather than a set of _test.go files so that callers
// outside this package can drive it too.
//
// Three behaviours are deliberate and worth not undoing:
//
// Callbacks are never delivered on a timer. A result is queued when the
// transaction resolves and flushed only when a test calls Deliver or
// DeliverNext. Tests therefore read as a sequence and the suite contains no
// sleeps, and "the poller queried status before the callback arrived" becomes
// something a test can state rather than race for.
//
// Tokens are adversarial. Every mint invalidates the previous token, exactly as
// Daraja does, and a request carrying a superseded token is rejected. A stub
// that handed out indefinitely valid tokens would let the client's single-flight
// pass its test vacuously.
//
// Nothing is signed. Daraja signs no callback, so neither does this. That is a
// feature: a forged-callback test is written by posting to the handler under
// test directly, without the stub's involvement.
package darajastub
