// Package models holds the GORM persistence models — the database schema
// expressed as Go structs. Each type maps to one table and carries the JSON and
// gorm struct tags that define its API shape and its columns. The repository
// layer reads and writes these; the service layers wrap them in their own DTOs.
//
// The entities are User, Account, and Transaction, plus SecurityQuestion for the
// PIN-recovery flow. An Account is a Stellar child account belonging to a User; a
// Transaction optionally references a User and an Account. PIN material lives on
// the User (its hash, attempt count, and lockout time), and security-question
// answers are stored hashed.
//
// # Conventions
//
// Every model follows the same rules. The primary key is a UUIDv7 assigned in a
// BeforeCreate hook, so IDs are unique and time-ordered without a database
// sequence. A TableName method pins the table name. Secret fields — PIN hash and
// security-answer hash — are tagged json:"-" so they never serialize into a
// response. User and Account carry a nullable, indexed DeletedAt that the
// repository stamps and clears by hand for reversible soft deletes; transactions
// are never deleted.
//
// # Canonical enums
//
// The string constants defined here are the single source of truth for the
// values the service state machines validate against: KYC statuses, the shared
// user and account lifecycle statuses, and the transaction statuses, categories,
// and types. Referencing these constants rather than bare strings keeps the
// services and the stored rows in agreement.
package models
