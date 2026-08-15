// Package rpc waits for a submitted Stellar transaction to be applied to the
// ledger.
//
// Submitting a transaction only returns an acceptance status; the network
// applies it a few ledgers later. PollTransaction bridges that gap: it fetches
// the transaction by hash on an interval until it reaches a terminal state, then
// reports the outcome. NOT_FOUND means not yet applied and is retried; a
// transient fetch error is also retried; SUCCESS and FAILED are terminal. The
// poll ends in failure if the context is cancelled, the attempt budget is
// exhausted (timeout), or the status is unrecognized.
//
// TransactionGetter is the one-method interface PollTransaction depends on, so
// any RPC client — or a mock — can drive it. PollConfig sets the attempt budget,
// interval, and logger; DefaultPollConfig supplies sensible values that callers
// override as needed.
package rpc
