package transaction

import "errors"

// Transaction service specific errors
var (
	// Resource not found errors
	ErrTransactionNotFound = errors.New("transaction not found")

	// Conflict errors
	ErrStellarHashAlreadyExists = errors.New("stellar transaction hash already registered")
	ErrExternalIDAlreadyExists  = errors.New("external ID already registered")

	// Business logic errors
	ErrInvalidStatusTransition = errors.New("invalid status transition")
	ErrCannotModifyTransaction = errors.New("cannot modify completed transaction")

	// Validation errors
	ErrInvalidInput    = errors.New("invalid input")
	ErrInvalidTxType   = errors.New("invalid transaction type")
	ErrInvalidAmount   = errors.New("invalid amount")
	ErrInvalidStatus   = errors.New("invalid transaction status")
	ErrInvalidCategory = errors.New("invalid transaction category")
)
