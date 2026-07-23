package models

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// Transaction represents a blockchain or off-chain transaction
type Transaction struct {
	ID               string    `json:"id" gorm:"type:uuid;primaryKey"`
	UserID           *string   `json:"user_id,omitempty" gorm:"type:uuid;index"`
	AccountID        *string   `json:"account_id,omitempty" gorm:"type:uuid;index"`
	LoanID           *string   `json:"loan_id,omitempty" gorm:"type:uuid;index"`
	TxType           string    `json:"tx_type" gorm:"type:varchar(50);not null;index"`
	Amount           int64     `json:"amount" gorm:"type:bigint;not null"`
	Asset            string    `json:"asset" gorm:"type:varchar(20);not null;index"`
	StellarTxHash    *string   `json:"stellar_tx_hash,omitempty" gorm:"type:varchar(64);uniqueIndex"`
	StellarLedger    *int64    `json:"stellar_ledger,omitempty" gorm:"type:bigint"`
	ContractID       *string   `json:"contract_id,omitempty" gorm:"type:varchar(56);index"`
	ContractFunction *string   `json:"contract_function,omitempty" gorm:"type:varchar(100)"`
	ExternalID       *string   `json:"external_id,omitempty" gorm:"type:varchar(100);index"`
	ExternalProvider *string   `json:"external_provider,omitempty" gorm:"type:varchar(50);index"`
	ExternalStatus   *string   `json:"external_status,omitempty" gorm:"type:varchar(20)"`
	Description      *string   `json:"description,omitempty" gorm:"type:text"`
	Metadata         *string   `json:"metadata,omitempty" gorm:"type:jsonb"`
	Status           string    `json:"status" gorm:"type:varchar(20);not null;default:'pending';index"`
	CreatedAt        time.Time `json:"created_at" gorm:"autoCreateTime;not null;index"`
	UpdatedAt        time.Time `json:"updated_at" gorm:"autoUpdateTime;not null"`

	User    *User    `gorm:"foreignKey:UserID"`
	Account *Account `gorm:"foreignKey:AccountID"`
}

// TableName specifies the table name for Transaction model
func (Transaction) TableName() string {
	return "transactions"
}

// BeforeCreate sets the ID before creating a new transaction
func (transaction *Transaction) BeforeCreate(tx *gorm.DB) error {
	id, err := uuid.NewV7()
	if err != nil {
		return err
	}
	transaction.ID = id.String()
	return nil
}

const (
	// Transaction Status
	TxStatusPending   = "pending"
	TxStatusSubmitted = "submitted"
	TxStatusSuccess   = "success"
	TxStatusFailed    = "failed"
	TxStatusCancelled = "cancelled"

	// Transaction Types — Loan Disbursement
	TxTypeVaultBorrow  = "vault_borrow"  // USDC borrowed from Stellar vault to user account
	TxTypeOffRamp      = "off_ramp"      // Off-ramp initiated (crypto-to-fiat via YellowCard)
	TxTypeFiatFailover = "fiat_failover" // Fiat failover after direct settlement refund
	TxTypeVaultRepay   = "vault_repay"   // USDC repaid from treasury back to Stellar vault
	TxTypeRefund       = "refund"        // USDC returned by an anchor after a cancelled off-ramp

	// TxTypeAnchorTransfer is the on-chain USDC leg of an anchor withdrawal:
	// treasury to the anchor's withdraw account. Distinct from TxTypeOffRamp,
	// which records the off-chain fiat leg the anchor pays out afterwards — a
	// cash-pickup disbursement produces both.
	TxTypeAnchorTransfer = "anchor_transfer"
)

// Transaction categories. Derived from TxType rather than stored: every type is
// settled either on the Stellar ledger or off it, never both, so a column would
// only add a way for the two to disagree.
const (
	TxCategoryOnChain  = "on_chain"
	TxCategoryOffChain = "off_chain"
)

// TxCategoryFor reports where a transaction type settles.
//
// Unknown types report off_chain: a type this function has not been taught
// about has no ledger presence to claim.
func TxCategoryFor(txType string) string {
	switch txType {
	case TxTypeVaultBorrow, TxTypeVaultRepay, TxTypeAnchorTransfer, TxTypeRefund:
		return TxCategoryOnChain
	default:
		return TxCategoryOffChain
	}
}
