package types

import (
	"github.com/stellar/go-stellar-sdk/keypair"
)

// ============================================================================
// Classic DTOs
// ============================================================================

// CreateAccountRequest represents a request to create an child account from a master/root account (treasury account)
type CreateAccountRequest struct {
	ChildKeypair    *keypair.Full
	StartingBalance string // 0
	EnableMultiSig  bool
	MultiSigConfig  *MultiSigConfig
}

// MultiSigConfig represents the multisignature configuration for the child account created from the master/root account
type MultiSigConfig struct {
	TreasurySigner string // Set to 1
	TreasuryWeight uint32 // Set to 1
	ChildWeight    uint32 // Set to 0 (Only master/treasury can sign)
	LowThreshold   uint32 // Set to 1
	MedThreshold   uint32 // Set to 1
	HighThreshold  uint32 // Set to 1
}

// EstablishTrustlineRequest represents a request to add a sponsored USDC trustline
// to an existing child account whose reserves are covered by the treasury.
type EstablishTrustlineRequest struct {
	ChildKeypair *keypair.Full
}

// SponsoredPaymentTransactionRequest represents a request to perform a classic sponsored payment transaction
// used for use cases such as off-ramping USDC from child account to off-ramp account
type SponsoredPaymentTransactionRequest struct {
	Destination string
	Amount      int64
	Source      string
	AssetCode   string
	AssetIssuer string
}

// SponsoredPaymentTransactionResponse represents a response from a SponsoredPaymentTransactionRequest
type SponsoredPaymentTransactionResponse struct {
	TxHash string
	Ledger int64
	Status string
}

// SendUSDCRequest represents a request to send USDC directly from the treasury wallet
// to an external Stellar address.
type SendUSDCRequest struct {
	Destination string // Stellar address to send USDC to
	Memo        string // Text memo
	Amount      int64  // Amount in stroops (USDC * 10^7)
}

// SendUSDCResponse represents the result of a SendUSDC operation.
type SendUSDCResponse struct {
	TxHash string
	Ledger int64
	Status string
}

// Soroban Vault DTOs

// BorrowRequest represents a request to borrow funds from the vault.
// The vault contract transfers USDC to the treasury, not the recipient.
// RecipientAddress is recorded in the on-chain Borrowed event for audit tracking.
type BorrowRequest struct {
	RecipientAddress string // Child account recorded in Borrowed event (USDC goes to treasury)
	Amount           int64  // Amount in smallest unit (stroops)
}

// BorrowResponse represents the result of a borrow operation
type BorrowResponse struct {
	TxHash           string
	AmountBorrowed   int64
	RecipientAddress string
	EventRecipient   string // Child account address extracted from the on-chain Borrowed event
	BorrowIndex      int64  // Borrow index at time of borrow (WAD scale, 1e18 = 1.0)
	Ledger           int64
	Status           string
	ContractID       string
	ContractFunction string
}

// RepayRequest represents a request to repay borrowed funds
type RepayRequest struct {
	Amount int64 // Amount to repay in smallest unit (stroops)
}

// RepayForRequest represents a request to repay borrowed funds on behalf of a
// named borrower. The treasury remains the payer; BorrowerAddress is recorded
// in the on-chain Repaid event for attribution and authorizes nothing.
type RepayForRequest struct {
	BorrowerAddress string // Child account recorded in the Repaid event
	Amount          int64  // Amount to repay in smallest unit (stroops)
}

// RepayResponse represents the result of a repay operation.
// BorrowerAddress and EventBorrower are empty for an unattributed repay.
type RepayResponse struct {
	TxHash           string
	AmountRepaid     int64
	BorrowerAddress  string // Borrower the repayment was made for, as requested
	EventBorrower    string // Borrower address extracted from the on-chain Repaid event
	Ledger           int64
	Status           string
	ContractID       string
	ContractFunction string
}

// BumpYieldRequest represents a request to contribute assets to the vault
// without minting shares. The treasury is always the source of the funds.
type BumpYieldRequest struct {
	Amount int64 // Amount to contribute in smallest unit (stroops)
}

// BumpYieldResponse represents the result of a bump-yield operation
type BumpYieldResponse struct {
	TxHash             string
	AmountContributed  int64
	TotalManagedAssets int64 // Managed assets after the contribution, from the YieldBumped event
	Ledger             int64
	Status             string
	ContractID         string
	ContractFunction   string
}

// VaultStatus represents the current state of the vault
type VaultStatus struct {
	TreasuryAddress    string
	TotalBorrowed      int64
	AvailableLiquidity int64
	TotalManagedAssets int64 // Available + Borrowed (used for share calculations)
	UtilizationRate    int64 // Basis points (e.g., 8000 = 80%)
	BorrowAPR          int64 // Basis points
	LockPeriodSeconds  uint64
	IsPaused           bool
}
