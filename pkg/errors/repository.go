package errors

import "errors"

// ErrNilDB is returned when a repository is initialized with a nil database connection.
var ErrNilDB = errors.New("database connection cannot be nil")
