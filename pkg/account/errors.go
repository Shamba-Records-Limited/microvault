package account

import "errors"

// Account service specific errors
var (
	// Resource not found errors
	ErrAccountNotFound = errors.New("account not found")

	// Conflict errors
	ErrPublicKeyAlreadyExists = errors.New("public key already registered")
	ErrAccountAlreadyDeleted  = errors.New("account is already deleted")
	ErrChainStatusConflict    = errors.New("account chain status is conflict and cannot change")

	// On-chain ensure outcomes, so callers can classify a failure without
	// reading error codes through a wrap chain.
	ErrDerivationConflict     = errors.New("account address was derived from a reused index")
	ErrDerivedAddressMismatch = errors.New("derived address does not match the stored one")
	ErrChainCheckUnavailable  = errors.New("on-chain account state could not be checked")

	// Business logic errors
	ErrCannotModifyDeletedAccount = errors.New("cannot modify deleted account")
	ErrInvalidStatusTransition    = errors.New("invalid status transition")
	ErrCannotDeleteLastAccount    = errors.New("cannot delete the last active account")

	// Validation errors
	ErrInvalidInput        = errors.New("invalid input")
	ErrInvalidPublicKey    = errors.New("invalid public key format")
	ErrInvalidStatus       = errors.New("invalid account status")
	ErrInvalidAccountIndex = errors.New("invalid account index")
)
