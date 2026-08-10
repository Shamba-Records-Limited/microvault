// Package types holds the request and response DTOs shared across the stellar
// packages, plus the error values they return.
//
// dto.go carries the inputs and outputs for both surfaces: classic operations
// (CreateAccountRequest, EstablishTrustlineRequest, SponsoredPaymentTransaction*,
// SendUSDC*, MultiSigConfig) and Vault contract operations (BorrowRequest,
// RepayRequest, and their responses). Keeping them here lets classic and soroban
// depend on one shared vocabulary without importing each other.
//
// errors.go holds the sentinel errors callers match with errors.Is — transaction
// build/sign/submit failures, validation failures, and terminal transaction
// statuses. It also defines ContractError and MapContractError, which translate a
// Vault contract's numeric error code into a typed Go error (ErrVaultPaused,
// ErrSharesLocked, ErrInsufficientBalance, and the rest).
package types
