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
	TxCategory       string    `json:"tx_category" gorm:"type:varchar(20);not null;default:'on_chain';index"`
	Amount           int64     `json:"amount" gorm:"type:bigint;not null"`
	Asset            string    `json:"asset" gorm:"type:varchar(20);not null;index"`
	StellarTxHash    *string   `json:"stellar_tx_hash,omitempty" gorm:"type:varchar(64);uniqueIndex"`
	StellarLedger    *int64    `json:"stellar_ledger,omitempty" gorm:"type:bigint"`
	StellarStatus    *string   `json:"stellar_status,omitempty" gorm:"type:varchar(20)"`
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

	// Transaction Category
	TxCategoryOnChain  = "on_chain"
	TxCategoryOffChain = "off_chain"
	TxCategoryInternal = "internal"

	// Transaction Types — Loan Disbursement
	TxTypeVaultBorrow  = "vault_borrow"  // USDC borrowed from Stellar vault to user account
	TxTypeOffRamp      = "off_ramp"      // Off-ramp initiated (crypto-to-fiat via YellowCard)
	TxTypeFiatFailover = "fiat_failover" // Fiat failover after direct settlement refund
)
