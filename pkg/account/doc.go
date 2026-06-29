// Package account is the business-logic layer for Stellar accounts. An account
// ties a Stellar public key to a user, along with a derivation index and a
// lifecycle status. Service wraps the account repository and adds the rules the
// repository does not enforce: input validation, uniqueness, and status
// transitions.
//
// Build a Service with NewService, passing the account and user repositories.
// It exposes creation, lookups (by ID, public key, or user), the next derivation
// index, soft delete/restore, and status updates. Requests and responses are the
// DTOs in dto.go; failures are the sentinel errors in errors.go, which handlers
// map onto HTTP status codes.
//
// # Status lifecycle
//
// An account moves through active, suspended, frozen, blocked, and closed. Not
// every move is legal: UpdateStatus consults a transition table and rejects an
// illegal change with ErrInvalidStatusTransition. closed is terminal — nothing
// transitions out of it — and a soft-deleted account cannot be modified at all.
//
// # Transactions
//
// Each mutating call has a WithTx variant that runs inside a *gorm.DB the caller
// owns, so an account can be created in the same database transaction as the user
// it belongs to. Because that user may not yet be visible to queries outside the
// transaction, CreateWithTx skips the user-existence check and relies on the
// foreign key for referential integrity. The derivation index can likewise be
// passed in when the caller has already reserved one inside the transaction.
package account
