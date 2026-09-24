// Package airtel is a client for Airtel Africa's Open API, Kenya op-co.
//
// The package is a client library and nothing more. It holds no database, no
// queue and no notification path, so several of the safety rules the platform
// depends on cannot be enforced here. They are the caller's, and they are
// listed below because getting them wrong moves real money.
//
// # A callback is not necessarily final
//
// Airtel documents the collection callback as carrying intermediate or final
// status. Its status_code is limited to TS and TF, while the enquiry enum is
// wider — TA, TIP and TE have no callback spelling. A TF callback is therefore
// not proof that a payment failed for good, and no loan may be closed on one.
// Confirm with CollectionEnquiry before crediting anything.
//
// # A verified hash is authenticity, not evidence
//
// Unlike Daraja, Airtel can sign a callback: with callback authentication
// enabled, a hash field carries an HmacSHA256 over the body. VerifyCallbackHash
// establishes that Airtel sent it. It does not establish that the payment
// settled, and it does not replace the enquiry.
//
// # Ambiguous and in-process mean enquire, never retry
//
// DP00800001000 and DP00800001006 both mean the transaction may yet succeed.
// So do HTTP 408, 502 and 504, and the ESB000001/4/8/14 family. Resending the
// same transaction id produces a duplicate-transaction error rather than a
// second payment; resending a fresh id produces a second payment. Enquire.
//
// # The receipt exists only on success
//
// airtel_money_id is generated on TS and is absent on TIP and TF. Code that
// reads it unconditionally breaks on every non-success path. It is also the
// only key CollectionRefund accepts, while CollectionEnquiry is keyed by the
// partner's own transaction id — so a refund is impossible until an enquiry or
// a callback has disclosed Airtel's id.
//
// # Wrong PIN and broken encryption are different failures
//
// ROUTER115 is the payer typing the wrong PIN. ROUTER116 is our RSA encryption
// being wrong. They are adjacent codes and completely different actions.
//
// # Enquiry has a floor
//
// Airtel documents a three-minute wait after CollectionPayment before
// CollectionEnquiry. Asking earlier is not an error, it is just uninformative.
//
// The wire shapes, header casings and code tables were read from the Kenya
// developer portal, which is login-gated; the transcription this package was
// built from is recorded in the vault as the Airtel Kenya API knowledge graph,
// including the places where the portal's own documentation is defective.
package airtel
