// Package elliptic implements compliance.Screener against Elliptic's AML
// API — wallet screening for a Stellar depositor address, the verdict
// policy that turns a risk score into a decision, and Standard Webhooks
// verification for rescreening callbacks.
//
// Scope for this pass (Phase 1 of the source design doc): the client's
// mechanics only — signing, the synchronous wallet-screening call, the
// verdict policy, webhook verification. No persistence, no wiring into a
// service, no batch/rescreening endpoints (those are later phases). See
// elliptic-compliance-integration.md in the knowledge vault for the full
// design and the phased delivery plan.
package elliptic
