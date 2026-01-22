package transaction

import "time"

// CreateTransactionRequest represents the request to create a new transaction
type CreateTransactionRequest struct {
	UserID           *string `json:"user_id,omitempty"`
	AccountID        *string `json:"account_id,omitempty"`
	LoanID           *string `json:"loan_id,omitempty"`
	TxType           string  `json:"tx_type" validate:"required"`
	TxCategory       string  `json:"tx_category,omitempty"`
	Amount           int64   `json:"amount" validate:"required,gt=0"`
	Asset            string  `json:"asset" validate:"required"`
	StellarTxHash    *string `json:"stellar_tx_hash,omitempty"`
	StellarLedger    *int64  `json:"stellar_ledger,omitempty"`
	ContractID       *string `json:"contract_id,omitempty"`
	ContractFunction *string `json:"contract_function,omitempty"`
	ExternalID       *string `json:"external_id,omitempty"`
	ExternalProvider *string `json:"external_provider,omitempty"`
	Description      *string `json:"description,omitempty"`
	Metadata         *string `json:"metadata,omitempty"`
}

// UpdateTransactionRequest represents the request to update a transaction
type UpdateTransactionRequest struct {
	StellarTxHash    *string `json:"stellar_tx_hash,omitempty"`
	StellarLedger    *int64  `json:"stellar_ledger,omitempty"`
	StellarStatus    *string `json:"stellar_status,omitempty"`
	ExternalID       *string `json:"external_id,omitempty"`
	ExternalProvider *string `json:"external_provider,omitempty"`
	ExternalStatus   *string `json:"external_status,omitempty"`
	Status           *string `json:"status,omitempty"`
	Description      *string `json:"description,omitempty"`
	Metadata         *string `json:"metadata,omitempty"`
}

// TransactionResponse represents the response containing transaction information
type TransactionResponse struct {
	ID               string     `json:"id"`
	UserID           *string    `json:"user_id,omitempty"`
	AccountID        *string    `json:"account_id,omitempty"`
	LoanID           *string    `json:"loan_id,omitempty"`
	TxType           string     `json:"tx_type"`
	TxCategory       string     `json:"tx_category"`
	Amount           int64      `json:"amount"`
	Asset            string     `json:"asset"`
	StellarTxHash    *string    `json:"stellar_tx_hash,omitempty"`
	StellarLedger    *int64     `json:"stellar_ledger,omitempty"`
	StellarStatus    *string    `json:"stellar_status,omitempty"`
	ContractID       *string    `json:"contract_id,omitempty"`
	ContractFunction *string    `json:"contract_function,omitempty"`
	ExternalID       *string    `json:"external_id,omitempty"`
	ExternalProvider *string    `json:"external_provider,omitempty"`
	ExternalStatus   *string    `json:"external_status,omitempty"`
	Description      *string    `json:"description,omitempty"`
	Metadata         *string    `json:"metadata,omitempty"`
	Status           string     `json:"status"`
	CreatedAt        time.Time  `json:"created_at"`
	UpdatedAt        time.Time  `json:"updated_at"`
}

// TransactionFilters represents filters for listing transactions
type TransactionFilters struct {
	UserID    string `json:"user_id,omitempty"`
	LoanID    string `json:"loan_id,omitempty"`
	Status    string `json:"status,omitempty"`
	TxType    string `json:"tx_type,omitempty"`
}
