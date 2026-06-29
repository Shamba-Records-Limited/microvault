// Package repository is the data-access layer. It holds the GORM-backed
// repositories for the core models — users, accounts, and transactions — behind
// interfaces that expose persistence operations and nothing else. Business rules
// live in the service packages above; this layer only reads and writes rows.
//
// Repositories bundles the three repositories, and NewRepositories wires them
// from a single *gorm.DB. Each constructor (and NewRepositories itself) rejects a
// nil database with errors.ErrNilDB. Every repository owns its own sentinel
// errors — ErrUserNotFound, ErrAccountNotFound, ErrTransactionNotFound, and the
// matching ErrFailedTo* values — and translates raw GORM errors into them, so
// callers match on package errors and never import GORM's.
//
// Reads that return lists take limit and offset for pagination; the service layer
// is responsible for turning page numbers into those.
//
// # Soft deletes
//
// Deletion is reversible and implemented by hand through a deleted_at column,
// not GORM's automatic soft delete. Delete stamps deleted_at with the current
// time, Restore clears it (an Unscoped update, since the row is otherwise
// hidden), and every read filters on deleted_at IS NULL. A deleted row therefore
// stays in the table but disappears from normal queries until it is restored.
//
// # Transactions
//
// Write methods come in a plain form and a WithTx form. The WithTx variant
// accepts a *gorm.DB supplied by the caller, so a write can join a larger unit of
// work — for example creating a user and their first account atomically. The
// account index handed to a new account is derived as one past the highest
// existing index across all accounts, with index 0 reserved for the admin.
package repository
