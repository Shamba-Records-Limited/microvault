// Package pin manages user PINs for USSD-based authentication: setting and
// verifying them, enforcing strength and lockout, changing and resetting them,
// and the security-question recovery flow. Service is the entry point, built with
// NewService over the user repository, the security-question repository, and an
// account notifier.
//
// PINs and security-question answers are never stored in the clear. Both are
// hashed with bcrypt, and the PIN hash, attempt count, and lockout time live on
// the user record. Answers are normalized — trimmed and lowercased — before
// hashing so trivial formatting differences don't cause a false mismatch.
//
// # Strength and lockout
//
// ValidatePIN enforces the rules a PIN must satisfy: exactly four digits, not all
// the same digit, and not a sequential run up or down. Verification is
// rate-limited by a lockout: after the configured number of consecutive wrong
// entries the account is locked for a fixed duration, during which PIN operations
// return ErrAccountLocked. GetRemainingAttempts and IsLocked expose that state,
// and a correct entry resets the counter.
//
// # Recovery
//
// A user can register security questions — at least the required minimum — as a
// way to reset a forgotten PIN. SetSecurityQuestions stores the hashed answers,
// VerifySecurityAnswers checks a set of answers against them, and a successful
// verification gates ResetPIN.
//
// # Notifications
//
// Several operations send the user an SMS as a side effect — a wrong-PIN warning,
// a lockout notice, and confirmations or failures of PIN changes and resets —
// through a contracts.AccountNotifier. Passing a nil notifier to NewService
// substitutes a no-op, so notifications can be switched off without changing call
// sites.
package pin
