// Package airtelstub is an in-process Airtel Open API, Kenya op-co.
//
// It exists for the same reason darajastub does: the interesting half of this
// integration is state and asynchrony, and a per-test http.HandlerFunc can
// express neither. It is a normal package rather than a set of _test.go files
// so callers outside the airtel package can drive it, and it deliberately does
// not import that package — a stub that shared the client's constants could
// not catch the client sending the wrong one, and the test binary would not
// link.
//
// Four behaviours are deliberate and worth not undoing:
//
// The receipt exists only on success. airtel_money_id is minted when a
// transaction resolves TS and is absent on TIP and TF, exactly as Airtel
// documents. A consumer that reads the receipt unconditionally therefore fails
// here rather than in production.
//
// A transaction can report twice. DeliverIntermediateThenFinal posts a
// callback that is not the last word, which is the behaviour with no M-Pesa
// equivalent and the one most likely to be handled wrongly. Nothing about a
// callback's shape says which it is.
//
// Callbacks are signed. Airtel computes an HmacSHA256 over the body when
// callback authentication is enabled, so this does too, under both readings of
// "the body" — with and without the hash member. Which reading a stub uses is
// selectable, because the portal does not say which Airtel uses.
//
// Signatures are verified, not inspected. A signed request has its x-key
// decrypted with the stub's own private half, the payload re-encrypted, and
// the result compared — so a client that skips signing, or signs the wrong
// bytes, is rejected with DP00800001026 rather than quietly passing.
package airtelstub
