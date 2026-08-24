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
// redacted in logs via the shared pkg/phone helper.
//
// # Composition
//
// SMSLoanNotifier and SMSAccountNotifier satisfy contracts.LoanNotifier and
// contracts.AccountNotifier. Each takes a Notifier plus a set of templates,
// picks the template for the event it was asked to send, and hands the result
// to the transport. Callers depend on the contracts interface, so the transport
// and wording can change without touching them.
//
// # Templates
//
// LoanTemplates and AccountTemplates hold one renderer per event — a func from
// the notification to the message text, not a format string. The compiler
// therefore checks both the fields a message reads and the verbs it formats
// them with, which a positional Sprintf template cannot do.
//
// The copy shipped here is deliberately brand-free: it names no company, no
// support URL and no USSD code, because those belong to whoever builds on the
// platform and, in the case of the USSD code, vary per deployment. Builders
// pass WithLoanTemplates or WithAccountTemplates to override the fields they
// care about; fields left nil keep the default, so overriding one message does
// not mean rewriting all of them. Values that vary by environment reach the
// copy by closure capture at construction rather than through a config type
// threaded into this package.
//
// # Validation
//
// The constructors return an error. Before returning a notifier they render
// every merged template against SentinelLoanNotification or
// SentinelAccountNotification and reject any that is unset, renders empty, or
// contains a rune outside GSM 03.38 — one such rune forces the whole SMS to
// UCS-2 and cuts a segment from 160 characters to 70. Broken copy therefore
// fails at startup rather than on a recipient's handset. GSM7Len and Segments
// are exported so builders can make the same checks over their own templates.
package notifications
