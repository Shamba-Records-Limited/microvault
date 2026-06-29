// Package errors holds sentinel errors shared across packages that would
// otherwise have to import one another to match on them. Keeping the values here
// lets unrelated packages compare against the same error with errors.Is without
// creating a dependency between themselves.
//
// At present this is the repository-construction error ErrNilDB, which the
// repository constructors return when handed a nil database connection.
package errors
