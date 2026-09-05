// Package mpesa is a client for Safaricom's Daraja API gateway.
//
// The package is a client library and nothing more. It holds no database, no
// queue and no notification path, so several of the safety rules the platform
// depends on cannot be enforced here. They are the caller's, and they are
// listed below because getting them wrong moves real money.
//
// # Callbacks are not evidence
//
// Daraja signs nothing. A callback carries no HMAC, no shared secret and no
// mutual TLS, so anyone who learns a callback URL can post a well-formed
// payment notification to it. Never credit anything on the strength of a
// callback alone: confirm it independently with ExpressQuery, PullTransactions
// or TransactionStatus first.
//
// # A queue timeout is not a failure
//
// Every Initiator call takes both a ResultURL and a QueueTimeOutURL. A
// delivery to the timeout URL means Daraja did not finish processing in time.
// It does not mean the transaction failed, and it says nothing about whether
// the transaction later completed. Treat it as unknown and resolve it with
// TransactionStatus. Retrying on a timeout is how money moves twice.
//
// The two deliveries cannot be told apart by their payload — a timeout body
// names Safaricom's own internal listener, not ours — so ParseResult takes the
// kind as an argument. Route each URL to a distinct handler and pass the kind
// that URL was registered as.
//
// # Reversal has preconditions this package cannot check
//
// Reverse moves money out and is irreversible. Before calling it the caller
// must establish that the transaction was observed from Safaricom rather than
// asserted by a callback, that it credited no loan, that no reversal is already
// in flight for it, and that the amount matches exactly. None of that is
// visible from here.
//
// # Verification fails closed
//
// ValidateMobileNumber reports success only on the documented success code.
// Any unrecognised response is not-verified. A verifier that answers "verified"
// to a code it does not understand is worse than no verifier.
//
// # Certificates
//
// The embedded Safaricom certificates are expired — sandbox since 2016,
// production since 2018 — and that is expected. Daraja publishes them as a
// carrier for an RSA public key, not as a trust anchor, and Safaricom has never
// rotated them. Nothing here validates a chain or an expiry date.
//
// The wire shapes and result-code tables were read from Safaricom's published
// documentation. The design owes a debt to github.com/jwambugu/mpesa-golang-sdk,
// which is a useful reference implementation and not a dependency; the places
// where this package deliberately departs from it are recorded in the vault.
package mpesa
