// Package airtelpoller holds the core-side Airtel Money tickers.
//
// It walks a wall-clock window rather than a next_poll_at column, because the
// Transactions Summary sweep reconciles a time range rather than a queue of
// rows — the same reasoning mpesapoller records for Daraja's Pull sweep.
//
// Confirming an individual collection is deliberately not here. Airtel's
// callback carries no reference, only the transaction id we minted, so a
// core-side confirmer sees an id it cannot attribute to a loan and would
// settle rows with no loan at all. The credit module's poller knows which
// loan minted which id, and that is where the enquiry belongs.
//
// What this does catch is the payment whose callback never arrived. Summary
// is the only endpoint in Airtel's catalogue that returns charges and both
// parties, and the only paginated one, which makes it the reconciliation
// source rather than a convenience.
package airtelpoller
