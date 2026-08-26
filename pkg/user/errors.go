package user

import "errors"

// User service specific errors
var (
	// Resource not found errors
	ErrUserNotFound = errors.New("user not found")

	// Conflict errors
	ErrMobileNumberAlreadyExists = errors.New("mobile number already registered")
	ErrNationalIDAlreadyExists   = errors.New("national ID already registered")

	// Business logic errors
	ErrCannotDeleteLastAdmin   = errors.New("cannot delete the last admin")
	ErrInvalidKYCTransition    = errors.New("invalid KYC status transition")
	ErrCannotModifyDeletedUser = errors.New("cannot modify deleted user")
	ErrUserAlreadyDeleted      = errors.New("user is already deleted")
	ErrUserNotDeleted          = errors.New("user is not deleted")
	ErrInvalidStatusTransition = errors.New("invalid status transition")
	ErrCannotDeleteSelf        = errors.New("cannot delete your own account")
	ErrCannotChangeOwnRole     = errors.New("cannot change your own role")

	// Validation errors
	ErrInvalidInput        = errors.New("invalid input")
	ErrInvalidMobileNumber = errors.New("invalid mobile number format")
	ErrInvalidNationalID   = errors.New("invalid national ID format")
	ErrInvalidRole         = errors.New("invalid role")
	ErrInvalidKYCStatus    = errors.New("invalid KYC status")
	ErrInvalidUserStatus   = errors.New("invalid user status")
)
