package contracts

import "errors"

var (
	ErrNotEligible          = errors.New("user is not eligible for a loan")
	ErrInsufficientLiquidity = errors.New("insufficient vault liquidity")
	ErrLoanNotFound         = errors.New("loan not found")
	ErrLoanNotActive        = errors.New("loan is not in an active state")
	ErrAccountNotFound      = errors.New("account not found")
	ErrAccountNotOwned      = errors.New("account is not owned by the user")
)
