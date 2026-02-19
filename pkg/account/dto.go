package account

import "time"

// CreateAccountRequest represents the request to create a new Stellar account
type CreateAccountRequest struct {
	UserID       string `json:"user_id" validate:"required"`
	PublicKey    string `json:"public_key" validate:"required"`
	AccountIndex *int   `json:"account_index,omitempty"` // Optional: if provided, use this index instead of auto-generating
}

// UpdateAccountStatusRequest represents the request to update account status
type UpdateAccountStatusRequest struct {
	Status string `json:"status" validate:"required,oneof=active suspended frozen closed"`
}

// AccountResponse represents the response containing account information
type AccountResponse struct {
	ID           string    `json:"id"`
	UserID       string    `json:"user_id"`
	PublicKey    string    `json:"public_key"`
	AccountIndex int       `json:"account_index"`
	Status       string    `json:"status"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}
