// Package notifications delivers user-facing messages — loan lifecycle and
// account/PIN events — and is the concrete implementation of the notifier ports
// declared in the contracts package. It separates three concerns: what to say,
// how it reads, and how it is delivered.
//
// # Transport
//
// Notifier is a thin send-only interface — Send a message to a recipient —
// that decouples the rest of the package from any particular channel. SMSNotifier
// implements it over an SMS provider; NoOpNotifier discards everything, for tests
// and for environments where notifications are switched off. Phone numbers are
// redacted in logs, and FormatPhoneNumber normalizes a raw number and prepends a
// country code when one is missing.
//
// # Composition
//
// SMSLoanNotifier and SMSAccountNotifier satisfy contracts.LoanNotifier and
// contracts.AccountNotifier. Each takes a Notifier plus a set of templates,
// picks the template for the event it was asked to send, fills it from the
// notification's fields, and hands the result to the transport. Callers depend on
// the contracts interface, so the transport and wording can change without
// touching them.
//
// # Templates
//
// LoanTemplates and AccountTemplates are structs of fmt.Sprintf format strings,
// one per event, each documenting the arguments it expects. DefaultLoanTemplates
// and DefaultAccountTemplates supply ready copy, parameterized for any currency;
// pass a custom struct to either constructor to override the wording without
// changing the sending logic.
package notifications
