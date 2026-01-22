package types

import (
	"errors"
	"fmt"
)

// ============================================================================
// Service-Level Errors (non-contract)
// ============================================================================

var (
	// Transaction building/signing errors
	ErrFailedToBuildTransaction      = errors.New("failed to build transaction")
	ErrFailedToSignTransaction       = errors.New("failed to sign transaction")
	ErrFailedToSubmitTransaction     = errors.New("failed to submit transaction")
	ErrFailedToSignWithSponsorKey    = errors.New("failed to sign transaction with sponsor key")
	ErrFailedToConvertTransaction    = errors.New("failed to convert transaction to XDR format")
	ErrFailedToLoadAccount           = errors.New("failed to load source account")
	ErrFailedToConvertCreditAsset    = errors.New("failed to convert credit asset to change trust asset")

	// Validation errors
	ErrInvalidStellarAddress     = errors.New("invalid stellar address")
	ErrInvalidTransactionAmount  = errors.New("invalid transaction amount")
	ErrFailedToValidateTrustline = errors.New("failed to validate trustline")
	ErrMissingTrustline          = errors.New("destination account does not have required trustline")

	// Transaction status errors
	ErrTransactionRejected           = errors.New("transaction rejected by stellar-core")
	ErrStellarCoreOverloaded         = errors.New("stellar-core overloaded, try again later")
	ErrTransactionFailed             = errors.New("transaction failed")
	ErrTransactionNotSuccessful      = errors.New("transaction not successful")
	ErrContextCancelled              = errors.New("context cancelled while waiting for transaction")
	ErrTransactionFailedOnLedger     = errors.New("transaction failed on ledger")
	ErrUnknownTransactionStatus      = errors.New("unknown transaction status")
	ErrTransactionTimeout            = errors.New("timeout waiting for transaction")
	ErrFailedToGetTransactionDetails = errors.New("failed to get transaction details")

	// Soroban-specific service errors
	ErrInvalidContractID   = errors.New("invalid contract ID")
	ErrNoSimulationResult  = errors.New("no result from simulation")
	ErrSimulationFailed    = errors.New("contract simulation failed")
	ErrVaultPaused         = errors.New("vault is paused")
	ErrSharesLocked        = errors.New("shares are locked")
	ErrInsufficientBalance = errors.New("insufficient balance")
)

// ============================================================================
// Contract Error Types
// ============================================================================

// ContractError represents an error returned by the smart contract
type ContractError struct {
	Code    uint32
	Name    string
	Message string
	Source  string // "MicroVault", "FungibleToken", "Vault", "Pausable", "Ownable"
}

func (e *ContractError) Error() string {
	return fmt.Sprintf("[%s:%d] %s: %s", e.Source, e.Code, e.Name, e.Message)
}

// IsRetryable returns true if the error is transient and operation can be retried
func (e *ContractError) IsRetryable() bool {
	// Math overflow and insufficient liquidity might resolve with time
	return e.Code == 104 || e.Code == 410 || e.Code == 10
}

// IsUserError returns true if the error is due to user input/state
func (e *ContractError) IsUserError() bool {
	switch e.Code {
	case 100, 101, 103, 12, 3, 4, 5, 11: // Balance, allowance, amount issues
		return true
	default:
		return false
	}
}

// IsAdminError returns true if the error relates to admin/ownership
func (e *ContractError) IsAdminError() bool {
	return e.Code >= 2100 && e.Code <= 2102
}

// IsPauseError returns true if the error relates to pause state
func (e *ContractError) IsPauseError() bool {
	return e.Code == 1000 || e.Code == 1001
}

// ============================================================================
// Error Mapping Function
// ============================================================================

// MapContractError maps a contract error code to a structured ContractError
func MapContractError(code uint32) *ContractError {
	switch code {
	// MicroVaultError (1-12)
	case 1:
		return &ContractError{code, "Unauthorized", "Caller is not authorized for this operation", "MicroVault"}
	case 2:
		return &ContractError{code, "CannotSweepUnderlyingAsset", "Cannot sweep the underlying asset", "MicroVault"}
	case 3:
		return &ContractError{code, "InvalidAmount", "Amount must be positive", "MicroVault"}
	case 4:
		return &ContractError{code, "ExceedsMaxDeposit", "Deposit exceeds maximum limit", "MicroVault"}
	case 5:
		return &ContractError{code, "ExceedsMaxWithdraw", "Withdrawal exceeds maximum limit", "MicroVault"}
	case 6:
		return &ContractError{code, "TreasuryNotSet", "Treasury not set", "MicroVault"}
	case 7:
		return &ContractError{code, "TimelockNotExpired", "Timelock not expired", "MicroVault"}
	case 8:
		return &ContractError{code, "NoPendingUpdate", "No pending update", "MicroVault"}
	case 9:
		return &ContractError{code, "ExceedsUtilizationCap", "Borrow would exceed utilization cap", "MicroVault"}
	case 10:
		return &ContractError{code, "InsufficientLiquidity", "Insufficient liquidity for withdrawal", "MicroVault"}
	case 11:
		return &ContractError{code, "RepayExceedsDebt", "Repay amount exceeds debt", "MicroVault"}
	case 12:
		return &ContractError{code, "SharesLocked", "Shares are locked and cannot be withdrawn", "MicroVault"}

	// FungibleTokenError (100-114)
	case 100:
		return &ContractError{code, "InsufficientBalance", "Account lacks required tokens for transfer", "FungibleToken"}
	case 101:
		return &ContractError{code, "InsufficientAllowance", "Spender lacks adequate allowance", "FungibleToken"}
	case 102:
		return &ContractError{code, "InvalidLiveUntilLedger", "Invalid live_until_ledger value for allowance", "FungibleToken"}
	case 103:
		return &ContractError{code, "LessThanZero", "Input must be non-negative", "FungibleToken"}
	case 104:
		return &ContractError{code, "MathOverflow", "Addition of two values causes overflow", "FungibleToken"}
	case 105:
		return &ContractError{code, "UnsetMetadata", "Access to uninitialized metadata", "FungibleToken"}
	case 106:
		return &ContractError{code, "ExceededCap", "Operation would exceed total supply cap", "FungibleToken"}
	case 107:
		return &ContractError{code, "InvalidCap", "Supplied cap value is invalid", "FungibleToken"}
	case 108:
		return &ContractError{code, "CapNotSet", "Cap was not configured", "FungibleToken"}
	case 109:
		return &ContractError{code, "SACNotSet", "SAC address not configured", "FungibleToken"}
	case 110:
		return &ContractError{code, "SACAddressMismatch", "SAC address differs from expected", "FungibleToken"}
	case 111:
		return &ContractError{code, "SACMissingFnParam", "Missing function parameter in SAC context", "FungibleToken"}
	case 112:
		return &ContractError{code, "SACInvalidFnParam", "Invalid function parameter in SAC context", "FungibleToken"}
	case 113:
		return &ContractError{code, "UserNotAllowed", "User lacks permission for operation", "FungibleToken"}
	case 114:
		return &ContractError{code, "UserBlocked", "User is blocked from this operation", "FungibleToken"}

	// VaultTokenError (400-410)
	case 400:
		return &ContractError{code, "VaultAssetAddressNotSet", "Vault asset address not initialized", "Vault"}
	case 401:
		return &ContractError{code, "VaultAssetAddressAlreadySet", "Vault asset address already set", "Vault"}
	case 402:
		return &ContractError{code, "VaultVirtualDecimalsOffsetAlreadySet", "Decimals offset already set", "Vault"}
	case 403:
		return &ContractError{code, "VaultInvalidAssetsAmount", "Invalid vault assets value", "Vault"}
	case 404:
		return &ContractError{code, "VaultInvalidSharesAmount", "Invalid vault shares value", "Vault"}
	case 405:
		return &ContractError{code, "VaultExceededMaxDeposit", "Deposit exceeds max amount", "Vault"}
	case 406:
		return &ContractError{code, "VaultExceededMaxMint", "Mint exceeds max amount", "Vault"}
	case 407:
		return &ContractError{code, "VaultExceededMaxWithdraw", "Withdraw exceeds max amount", "Vault"}
	case 408:
		return &ContractError{code, "VaultExceededMaxRedeem", "Redeem exceeds max amount", "Vault"}
	case 409:
		return &ContractError{code, "VaultMaxDecimalsOffsetExceeded", "Maximum decimals offset exceeded", "Vault"}
	case 410:
		return &ContractError{code, "MathOverflow", "Mathematical overflow in vault operation", "Vault"}

	// PausableError (1000-1001)
	case 1000:
		return &ContractError{code, "EnforcedPause", "Operation failed because contract is paused", "Pausable"}
	case 1001:
		return &ContractError{code, "ExpectedPause", "Operation failed because contract is not paused", "Pausable"}

	// OwnableError (2100-2102)
	case 2100:
		return &ContractError{code, "OwnerNotSet", "Owner has not been set", "Ownable"}
	case 2101:
		return &ContractError{code, "TransferInProgress", "Ownership transfer already in progress", "Ownable"}
	case 2102:
		return &ContractError{code, "OwnerAlreadySet", "Owner has already been set", "Ownable"}

	default:
		return &ContractError{code, "Unknown", fmt.Sprintf("Unknown contract error code: %d", code), "Unknown"}
	}
}
