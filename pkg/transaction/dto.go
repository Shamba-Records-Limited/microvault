package transaction

import "time"

// CreateTransactionRequest represents the request to create a new transaction
type CreateTransactionRequest struct {
	UserID           *string `json:"user_id,omitempty"`
	AccountID        *string `json:"account_id,omitempty"`
	LoanID           *string `json:"loan_id,omitempty"`
	TxType           string  `json:"tx_type" validate:"required"`
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
	ExternalID       *string `json:"external_id,omitempty"`
	ExternalProvider *string `json:"external_provider,omitempty"`
	ExternalStatus   *string `json:"external_status,omitempty"`
	Status           *string `json:"status,omitempty"`
	Description      *string `json:"description,omitempty"`
	Metadata         *string `json:"metadata,omitempty"`
}

// TransactionResponse represents the response containing transaction information
type TransactionResponse struct {
	ID        string  `json:"id"`
	UserID    *string `json:"user_id,omitempty"`
	AccountID *string `json:"account_id,omitempty"`
	LoanID    *string `json:"loan_id,omitempty"`
	TxType    string  `json:"tx_type"`
	// TxCategory is derived from TxType, not stored. See models.TxCategoryFor.
	TxCategory       string    `json:"tx_category"`
	Amount           int64     `json:"amount"`
	Asset            string    `json:"asset"`
	StellarTxHash    *string   `json:"stellar_tx_hash,omitempty"`
	StellarLedger    *int64    `json:"stellar_ledger,omitempty"`
	ContractID       *string   `json:"contract_id,omitempty"`
	ContractFunction *string   `json:"contract_function,omitempty"`
	ExternalID       *string   `json:"external_id,omitempty"`
	ExternalProvider *string   `json:"external_provider,omitempty"`
	ExternalStatus   *string   `json:"external_status,omitempty"`
	Description      *string   `json:"description,omitempty"`
	Metadata         *string   `json:"metadata,omitempty"`
	Status           string    `json:"status"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
}

// TransactionFilters represents filters for listing transactions
type TransactionFilters struct {
	UserID string `json:"user_id,omitempty"`
	LoanID string `json:"loan_id,omitempty"`
	Status string `json:"status,omitempty"`
	TxType string `json:"tx_type,omitempty"`
}

// changedFields returns only the columns this request actually sets, so a
// partial update writes a handful of columns instead of rewriting the whole
// row (text description + jsonb metadata) and all 12 indexes on it.
//
// Callers must run any status-transition/immutability validation before using
// this; it does no validation itself.
func (r UpdateTransactionRequest) changedFields() map[string]any {
	f := make(map[string]any, 8)
	if r.StellarTxHash != nil {
		f["stellar_tx_hash"] = r.StellarTxHash
	}
	if r.StellarLedger != nil {
		f["stellar_ledger"] = r.StellarLedger
	}
	if r.ExternalID != nil {
		f["external_id"] = r.ExternalID
	}
	if r.ExternalProvider != nil {
		f["external_provider"] = r.ExternalProvider
	}
	if r.ExternalStatus != nil {
		f["external_status"] = r.ExternalStatus
	}
	if r.Status != nil {
		f["status"] = r.Status
	}
	if r.Description != nil {
		f["description"] = r.Description
	}
	if r.Metadata != nil {
		f["metadata"] = r.Metadata
	}
	return f
}
