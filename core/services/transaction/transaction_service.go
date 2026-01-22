package transaction

import (
	"context"
	"errors"
	"log"
	"slices"

	"github.com/Shamba-Records-Limited/Microvault/internal/core/app/repository"
	"github.com/Shamba-Records-Limited/Microvault/internal/core/services"
	"github.com/Shamba-Records-Limited/Microvault/pkg/models"
)

// Valid transaction status transitions
var validStatusTransitions = map[string][]string{
	models.TxStatusPending:   {models.TxStatusSubmitted, models.TxStatusCancelled},
	models.TxStatusSubmitted: {models.TxStatusSuccess, models.TxStatusFailed},
	models.TxStatusSuccess:   {},                       // Cannot transition from success
	models.TxStatusFailed:    {models.TxStatusPending}, // Can retry
	models.TxStatusCancelled: {},                       // Cannot transition from cancelled
}

// Valid transaction statuses
var validStatuses = map[string]bool{
	models.TxStatusPending:   true,
	models.TxStatusSubmitted: true,
	models.TxStatusSuccess:   true,
	models.TxStatusFailed:    true,
	models.TxStatusCancelled: true,
}

// Valid transaction categories
var validCategories = map[string]bool{
	models.TxCategoryOnChain:  true,
	models.TxCategoryOffChain: true,
	models.TxCategoryInternal: true,
}

// Service defines the interface for transaction business logic operations
type Service interface {
	// Transaction management
	Create(ctx context.Context, req CreateTransactionRequest) (*TransactionResponse, error)
	BatchCreate(ctx context.Context, reqs []CreateTransactionRequest) ([]*TransactionResponse, error)
	GetByID(ctx context.Context, id string) (*TransactionResponse, error)
	GetByStellarHash(ctx context.Context, txHash string) (*TransactionResponse, error)
	GetByExternalID(ctx context.Context, externalID string) (*TransactionResponse, error)
	GetByLoanID(ctx context.Context, loanID string, pagination services.Pagination) (*services.PaginatedResponse[TransactionResponse], error)
	GetByUserID(ctx context.Context, userID string, pagination services.Pagination) (*services.PaginatedResponse[TransactionResponse], error)
	GetByStatus(ctx context.Context, status string, pagination services.Pagination) (*services.PaginatedResponse[TransactionResponse], error)
	Update(ctx context.Context, id string, req UpdateTransactionRequest) (*TransactionResponse, error)
}

// service implements the Service interface
type service struct {
	repo repository.TransactionRepository
}

// NewService creates a new transaction service instance
func NewService(repo repository.TransactionRepository) Service {
	return &service{
		repo: repo,
	}
}

// Create creates a new transaction with business validation
func (s *service) Create(ctx context.Context, req CreateTransactionRequest) (*TransactionResponse, error) {
	// Validate input
	if err := s.validateCreateRequest(req); err != nil {
		return nil, err
	}

	// Check stellar hash uniqueness if provided
	if req.StellarTxHash != nil && *req.StellarTxHash != "" {
		existing, err := s.repo.GetByStellarHash(ctx, *req.StellarTxHash)
		if err == nil && existing != nil {
			return nil, ErrStellarHashAlreadyExists
		}
		if err != nil && !errors.Is(err, repository.ErrTransactionNotFound) {
			log.Printf("Create: failed to check stellar hash uniqueness: %v", err)
			return nil, err
		}
	}

	// Check external ID uniqueness if provided
	if req.ExternalID != nil && *req.ExternalID != "" {
		existing, err := s.repo.GetByExternalID(ctx, *req.ExternalID)
		if err != nil && !errors.Is(err, repository.ErrTransactionNotFound) {
			log.Printf("Create: failed to check external ID uniqueness: %v", err)
			return nil, err
		}
		if existing != nil {
			return nil, ErrExternalIDAlreadyExists
		}
	}

	// Set defaults
	category := req.TxCategory
	if category == "" {
		category = models.TxCategoryOnChain
	}

	// Create transaction model
	tx := &models.Transaction{
		UserID:           req.UserID,
		AccountID:        req.AccountID,
		LoanID:           req.LoanID,
		TxType:           req.TxType,
		TxCategory:       category,
		Amount:           req.Amount,
		Asset:            req.Asset,
		StellarTxHash:    req.StellarTxHash,
		StellarLedger:    req.StellarLedger,
		ContractID:       req.ContractID,
		ContractFunction: req.ContractFunction,
		ExternalID:       req.ExternalID,
		ExternalProvider: req.ExternalProvider,
		Description:      req.Description,
		Metadata:         req.Metadata,
		Status:           models.TxStatusPending,
	}

	// Create transaction in database
	if err := s.repo.Create(ctx, tx); err != nil {
		log.Printf("Create: failed to create transaction: %v", err)
		return nil, err
	}

	return toTransactionResponse(tx), nil
}

// BatchCreate creates multiple transactions
func (s *service) BatchCreate(ctx context.Context, reqs []CreateTransactionRequest) ([]*TransactionResponse, error) {
	if len(reqs) == 0 {
		return []*TransactionResponse{}, nil
	}

	txs := make([]*models.Transaction, len(reqs))
	for i, req := range reqs {
		if err := s.validateCreateRequest(req); err != nil {
			return nil, err
		}

		category := req.TxCategory
		if category == "" {
			category = models.TxCategoryOnChain
		}

		txs[i] = &models.Transaction{
			UserID:           req.UserID,
			AccountID:        req.AccountID,
			LoanID:           req.LoanID,
			TxType:           req.TxType,
			TxCategory:       category,
			Amount:           req.Amount,
			Asset:            req.Asset,
			StellarTxHash:    req.StellarTxHash,
			StellarLedger:    req.StellarLedger,
			ContractID:       req.ContractID,
			ContractFunction: req.ContractFunction,
			ExternalID:       req.ExternalID,
			ExternalProvider: req.ExternalProvider,
			Description:      req.Description,
			Metadata:         req.Metadata,
			Status:           models.TxStatusPending,
		}
	}

	if err := s.repo.BatchCreate(ctx, txs); err != nil {
		log.Printf("BatchCreate: failed to create transactions: %v", err)
		return nil, err
	}

	responses := make([]*TransactionResponse, len(txs))
	for i, tx := range txs {
		responses[i] = toTransactionResponse(tx)
	}

	return responses, nil
}

// GetByID retrieves a transaction by ID
func (s *service) GetByID(ctx context.Context, id string) (*TransactionResponse, error) {
	tx, err := s.repo.GetByID(ctx, id)
	if err != nil {
		if errors.Is(err, repository.ErrTransactionNotFound) {
			return nil, ErrTransactionNotFound
		}
		log.Printf("GetByID: failed to get transaction: %v", err)
		return nil, err
	}

	return toTransactionResponse(tx), nil
}

// GetByStellarHash retrieves a transaction by its Stellar hash
func (s *service) GetByStellarHash(ctx context.Context, txHash string) (*TransactionResponse, error) {
	tx, err := s.repo.GetByStellarHash(ctx, txHash)
	if err != nil {
		if errors.Is(err, repository.ErrTransactionNotFound) {
			return nil, ErrTransactionNotFound
		}
		log.Printf("GetByStellarHash: failed to get transaction: %v", err)
		return nil, err
	}

	return toTransactionResponse(tx), nil
}

// GetByExternalID retrieves a transaction by external ID
func (s *service) GetByExternalID(ctx context.Context, externalID string) (*TransactionResponse, error) {
	tx, err := s.repo.GetByExternalID(ctx, externalID)
	if err != nil {
		log.Printf("GetByExternalID: failed to get transaction: %v", err)
		return nil, err
	}
	if tx == nil {
		return nil, ErrTransactionNotFound
	}

	return toTransactionResponse(tx), nil
}

// GetByLoanID retrieves transactions by loan ID with pagination
func (s *service) GetByLoanID(ctx context.Context, loanID string, pagination services.Pagination) (*services.PaginatedResponse[TransactionResponse], error) {
	if pagination.Page <= 0 {
		pagination.Page = 1
	}
	if pagination.PageSize <= 0 {
		pagination.PageSize = 10
	}
	if pagination.PageSize > 100 {
		pagination.PageSize = 100
	}

	offset := (pagination.Page - 1) * pagination.PageSize

	txs, err := s.repo.GetByLoanID(ctx, loanID, pagination.PageSize, offset)
	if err != nil {
		log.Printf("GetByLoanID: failed to get transactions: %v", err)
		return nil, err
	}

	responses := make([]TransactionResponse, len(txs))
	for i, tx := range txs {
		responses[i] = *toTransactionResponse(tx)
	}

	return &services.PaginatedResponse[TransactionResponse]{
		Data:     responses,
		Page:     pagination.Page,
		PageSize: pagination.PageSize,
	}, nil
}

// GetByUserID retrieves transactions by user ID with pagination
func (s *service) GetByUserID(ctx context.Context, userID string, pagination services.Pagination) (*services.PaginatedResponse[TransactionResponse], error) {
	if pagination.Page <= 0 {
		pagination.Page = 1
	}
	if pagination.PageSize <= 0 {
		pagination.PageSize = 10
	}
	if pagination.PageSize > 100 {
		pagination.PageSize = 100
	}

	offset := (pagination.Page - 1) * pagination.PageSize

	txs, err := s.repo.GetByUserID(ctx, userID, pagination.PageSize, offset)
	if err != nil {
		log.Printf("GetByUserID: failed to get transactions: %v", err)
		return nil, err
	}

	responses := make([]TransactionResponse, len(txs))
	for i, tx := range txs {
		responses[i] = *toTransactionResponse(tx)
	}

	return &services.PaginatedResponse[TransactionResponse]{
		Data:     responses,
		Page:     pagination.Page,
		PageSize: pagination.PageSize,
	}, nil
}

// GetByStatus retrieves transactions by status with pagination
func (s *service) GetByStatus(ctx context.Context, status string, pagination services.Pagination) (*services.PaginatedResponse[TransactionResponse], error) {
	if !validStatuses[status] {
		return nil, ErrInvalidStatus
	}

	if pagination.Page <= 0 {
		pagination.Page = 1
	}
	if pagination.PageSize <= 0 {
		pagination.PageSize = 10
	}
	if pagination.PageSize > 100 {
		pagination.PageSize = 100
	}

	offset := (pagination.Page - 1) * pagination.PageSize

	txs, err := s.repo.GetByStatus(ctx, status, pagination.PageSize, offset)
	if err != nil {
		log.Printf("GetByStatus: failed to get transactions: %v", err)
		return nil, err
	}

	responses := make([]TransactionResponse, len(txs))
	for i, tx := range txs {
		responses[i] = *toTransactionResponse(tx)
	}

	return &services.PaginatedResponse[TransactionResponse]{
		Data:     responses,
		Page:     pagination.Page,
		PageSize: pagination.PageSize,
	}, nil
}

// Update updates a transaction
func (s *service) Update(ctx context.Context, id string, req UpdateTransactionRequest) (*TransactionResponse, error) {
	// Get existing transaction
	tx, err := s.repo.GetByID(ctx, id)
	if err != nil {
		if errors.Is(err, repository.ErrTransactionNotFound) {
			return nil, ErrTransactionNotFound
		}
		log.Printf("Update: failed to get transaction: %v", err)
		return nil, err
	}

	// Check if transaction can be modified
	if tx.Status == models.TxStatusSuccess || tx.Status == models.TxStatusCancelled {
		return nil, ErrCannotModifyTransaction
	}

	// Validate status transition if status is being updated
	if req.Status != nil && *req.Status != tx.Status {
		if !validStatuses[*req.Status] {
			return nil, ErrInvalidStatus
		}
		if !s.isValidStatusTransition(tx.Status, *req.Status) {
			return nil, ErrInvalidStatusTransition
		}
		tx.Status = *req.Status
	}

	// Update fields
	if req.StellarTxHash != nil {
		tx.StellarTxHash = req.StellarTxHash
	}
	if req.StellarLedger != nil {
		tx.StellarLedger = req.StellarLedger
	}
	if req.StellarStatus != nil {
		tx.StellarStatus = req.StellarStatus
	}
	if req.ExternalID != nil {
		tx.ExternalID = req.ExternalID
	}
	if req.ExternalProvider != nil {
		tx.ExternalProvider = req.ExternalProvider
	}
	if req.ExternalStatus != nil {
		tx.ExternalStatus = req.ExternalStatus
	}
	if req.Description != nil {
		tx.Description = req.Description
	}
	if req.Metadata != nil {
		tx.Metadata = req.Metadata
	}

	// Update in database
	if err := s.repo.Update(ctx, tx); err != nil {
		log.Printf("Update: failed to update transaction: %v", err)
		return nil, err
	}

	return toTransactionResponse(tx), nil
}

// --- Helper functions ---

// validateCreateRequest validates the create transaction request
func (s *service) validateCreateRequest(req CreateTransactionRequest) error {
	if req.TxType == "" {
		return ErrInvalidTxType
	}

	if req.Amount <= 0 {
		return ErrInvalidAmount
	}

	if req.Asset == "" {
		return ErrInvalidInput
	}

	if req.TxCategory != "" && !validCategories[req.TxCategory] {
		return ErrInvalidCategory
	}

	return nil
}

// isValidStatusTransition checks if a status transition is valid
func (s *service) isValidStatusTransition(from, to string) bool {
	if from == to {
		return true
	}

	validTransitions, exists := validStatusTransitions[from]
	if !exists {
		return false
	}

	return slices.Contains(validTransitions, to)
}

// toTransactionResponse converts a transaction model to response DTO
func toTransactionResponse(tx *models.Transaction) *TransactionResponse {
	return &TransactionResponse{
		ID:               tx.ID,
		UserID:           tx.UserID,
		AccountID:        tx.AccountID,
		LoanID:           tx.LoanID,
		TxType:           tx.TxType,
		TxCategory:       tx.TxCategory,
		Amount:           tx.Amount,
		Asset:            tx.Asset,
		StellarTxHash:    tx.StellarTxHash,
		StellarLedger:    tx.StellarLedger,
		StellarStatus:    tx.StellarStatus,
		ContractID:       tx.ContractID,
		ContractFunction: tx.ContractFunction,
		ExternalID:       tx.ExternalID,
		ExternalProvider: tx.ExternalProvider,
		ExternalStatus:   tx.ExternalStatus,
		Description:      tx.Description,
		Metadata:         tx.Metadata,
		Status:           tx.Status,
		CreatedAt:        tx.CreatedAt,
		UpdatedAt:        tx.UpdatedAt,
	}
}
