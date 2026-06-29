// Package transaction is the business-logic layer for the platform's transaction
// ledger — the durable record of every money movement, whether it settled on the
// Stellar network, through an external provider, or internally. Service wraps the
// transaction repository and adds validation, uniqueness, and status-transition
// rules.
//
// Build a Service with NewService. It records transactions (Create, BatchCreate),
// looks them up by ID, Stellar hash, external ID, loan, user, or status, and
// applies updates. List operations are paginated through services.Pagination,
// which the service clamps (default page size 10, capped at 100). Requests and
// responses are the DTOs in dto.go; failures are the sentinel errors in errors.go,
// which handlers map onto HTTP status codes.
//
// # What a record holds
//
// Each transaction carries a type and a category — on-chain, off-chain, or
// internal — an amount and asset, and optional links to a user, account, or loan.
// Settlement details hang off the same row: the Stellar hash, ledger, and status
// for on-chain work, or the external ID, provider, and status for an off-ramp
// provider. New transactions default to the on-chain category and open in the
// pending status.
//
// # Status lifecycle
//
// A transaction moves from pending to submitted, then to success or failed. A
// failed transaction may return to pending for a retry; pending may also be
// cancelled.
// success and cancelled are terminal — Update refuses to modify a transaction in
// either state, and every other status change is checked against the transition
// table, rejecting an illegal move with ErrInvalidStatusTransition.
//
// # Uniqueness
//
// A Stellar hash or external ID, when supplied, must be unique across the ledger.
// Create rejects a duplicate with ErrStellarHashAlreadyExists or
// ErrExternalIDAlreadyExists, which keeps a single settlement from being recorded
// twice.
package transaction
