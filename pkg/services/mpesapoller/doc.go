// Package mpesapoller holds the core-side M-Pesa tickers.
//
// Both walk mpesa_transactions on a wall-clock cadence rather than off a
// next_poll_at column, because neither is driven by a per-row schedule: the
// Pull sweep reconciles a time window, and the balance poll asks about a
// shortcode. Cadence in a column serves a queue of rows; these have none.
//
// Confirming an STK observation is deliberately not here. That row is confirmed
// by the loan poller in the credit module, which knows which loan the checkout
// belongs to and can therefore attribute the receipt; a core-side confirmer
// sees only the checkout ID and would settle the row with no loan at all.
package mpesapoller
