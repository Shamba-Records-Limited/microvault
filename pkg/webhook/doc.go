// Package webhook reacts to off-ramp settlement outcomes. It has two halves: a
// service that processes inbound payment-provider webhook events, and a poller
// that chases disbursements stuck awaiting a refund.
//
// Both depend only on consumer-defined interfaces — DisbursementUpdater,
// PaymentLookup, TransactionRecorder, AlertService — which the loan/disbursement
// service implements. That keeps this package free of any dependency on the
// lending module while still driving loan state.
//
// # Event service
//
// Service (webhook_service.go) implements WebhookEventHandler. The HTTP layer
// verifies a webhook's signature and hands the event here; the service maps it to
// a disbursement status change and the side effects that follow — notifying the
// user, recording the final financials, and repaying the vault on completion.
// Failures are branched by settlement method: a FAILED event on a direct
// settlement becomes refund-pending, while one on a fiat settlement is terminal,
// a distinction the service resolves with IsDirectSettlement rather than relying
// on the provider to flag it.
//
// # Refund poller
//
// RefundPoller (refund_poller.go) runs in the background on an interval, fetching
// disbursements left in the refund-pending state — direct settlements whose USDC
// was returned to the treasury — and attempting a fiat failover so the borrower
// is still paid. It marks the loan's settlement method so the eventual completion
// event takes the vault-repay path, and alerts ops when a case needs a human.
package webhook
