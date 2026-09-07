// Package mpesapoller resolves M-Pesa observations.
//
// A callback is never the end of a payment — it is an observation. Daraja signs
// nothing, so only an independent check moves one from "recorded" to
// "confirmed". That check is an STK query here, so a callback is nothing more
// than the thing that puts the receipt on the queue.
//
// Cadence lives in the next_poll_at column, not in a ticker. A prompt spends
// most of its life doing nothing; driving the query off wall-clock intervals
// would ask Daraja for a pending prompt on every tick. In-memory timers are
// lost on restart; the column is not.
//
// The runner only decides when to ask; the column decides which come back.
// mpesa_transactions covers inbound notifications plainly — one file, one
// driver, one FetchFunc.
package mpesapoller
