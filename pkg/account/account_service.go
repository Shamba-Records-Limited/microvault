package account

import (
	"context"
	"errors"
	"log"
	"slices"

	"github.com/Shamba-Records-Limited/microvault/pkg/repository"
	"github.com/Shamba-Records-Limited/microvault/pkg/services"
	"github.com/Shamba-Records-Limited/microvault/pkg/models"
	"gorm.io/gorm"
)

// Valid account status transitions
var validStatusTransitions = map[string][]string{
	"active":    {"suspended", "blocked", "frozen", "closed"},
	"suspended": {"active", "blocked", "frozen", "closed"},
	"frozen":    {"active", "blocked", "closed"},
	"blocked":   {"active", "closed"}, // Can unblock to active or permanently close
	"closed":    {},                   // Cannot transition from closed
}

// Valid account statuses
var validStatuses = map[string]bool{
	"active":    true,
	"suspended": true,
	"blocked":   true,
	"frozen":    true,
	"closed":    true,
}

// Service defines the interface for account business logic operations
type Service interface {
	// Account management
	Create(ctx context.Context, req CreateAccountRequest) (*AccountResponse, error)
	CreateWithTx(ctx context.Context, tx *gorm.DB, req CreateAccountRequest) (*AccountResponse, error)
	GetByID(ctx context.Context, id string) (*AccountResponse, error)
	GetByPublicKey(ctx context.Context, publicKey string) (*AccountResponse, error)
	GetByUserID(ctx context.Context, userID string) (*AccountResponse, error)
	GetNextAccountIndex(ctx context.Context, userID string) (int, error)
	GetNextAccountIndexWithTx(ctx context.Context, tx *gorm.DB) (int, error)
	Delete(ctx context.Context, id string) error
	Restore(ctx context.Context, id string) error

	// Status management
	UpdateStatus(ctx context.Context, id string, req UpdateAccountStatusRequest) (*AccountResponse, error)
}

// service implements the Service interface
type service struct {
	repo     repository.AccountRepository
	userRepo repository.UserRepository
}

// NewService creates a new account service instance
func NewService(repo repository.AccountRepository, userRepo repository.UserRepository) Service {
	return &service{
		repo:     repo,
		userRepo: userRepo,
	}
}

// Create creates a new Stellar account with business validation
func (s *service) Create(ctx context.Context, req CreateAccountRequest) (*AccountResponse, error) {
	// Validate input
	if err := s.validateCreateRequest(req); err != nil {
		return nil, err
	}

	// Check if user exists
	_, err := s.userRepo.GetByID(ctx, req.UserID)
	if err != nil {
		if errors.Is(err, repository.ErrUserNotFound) {
			return nil, services.ErrNotFound
		}
		log.Printf("Create: failed to get user: %v", err)
		return nil, err
	}

	// Check public key uniqueness
	_, err = s.repo.GetByPublicKey(ctx, req.PublicKey)
	if err == nil {
		return nil, ErrPublicKeyAlreadyExists
	}
	if !errors.Is(err, repository.ErrAccountNotFound) {
		log.Printf("Create: failed to check public key uniqueness: %v", err)
		return nil, err
	}

	// Get next account index for the user
	accountIndex, err := s.repo.GetNextAccountIndex(ctx, req.UserID)
	if err != nil {
		log.Printf("Create: failed to get next account index: %v", err)
		return nil, err
	}

	// Create account model
	account := &models.Account{
		UserID:       req.UserID,
		PublicKey:    req.PublicKey,
		AccountIndex: accountIndex,
		Status:       "active",
	}

	// Create account in database
	if err := s.repo.Create(ctx, account); err != nil {
		log.Printf("Create: failed to create account: %v", err)
		return nil, err
	}

	return toAccountResponse(account), nil
}

// CreateWithTx creates a new account with a transaction.
func (s *service) CreateWithTx(ctx context.Context, tx *gorm.DB, req CreateAccountRequest) (*AccountResponse, error) {
	// Validate input
	if err := s.validateCreateRequest(req); err != nil {
		return nil, err
	}

	// Note: We skip user existence check here because in transaction context,
	// the user may have just been created in the same transaction and won't be
	// visible to queries outside the transaction. The foreign key constraint
	// will ensure referential integrity.

	// Check public key uniqueness within the transaction
	var existingAccount models.Account
	result := tx.WithContext(ctx).
		Where("public_key = ? AND deleted_at IS NULL", req.PublicKey).
		First(&existingAccount)

	if result.Error == nil {
		// Account with this public key already exists
		return nil, ErrPublicKeyAlreadyExists
	}
	if !errors.Is(result.Error, gorm.ErrRecordNotFound) {
		log.Printf("CreateWithTx: failed to check public key uniqueness: %v", result.Error)
		return nil, result.Error
	}

	// Determine account index
	var accountIndex int
	if req.AccountIndex != nil {
		// Use provided account index (caller already fetched it within the transaction)
		accountIndex = *req.AccountIndex
		log.Printf("CreateWithTx: using provided account index %d", accountIndex)
	} else {
		// Get next account index within the transaction
		var err error
		accountIndex, err = s.repo.GetNextAccountIndexWithTx(ctx, tx)
		if err != nil {
			log.Printf("CreateWithTx: failed to get next account index: %v", err)
			return nil, err
		}
		log.Printf("CreateWithTx: fetched account index %d", accountIndex)
	}

	// Create account model
	account := &models.Account{
		UserID:       req.UserID,
		PublicKey:    req.PublicKey,
		AccountIndex: accountIndex,
		Status:       "active",
	}

	// Create account in database
	if err := s.repo.CreateWithTx(ctx, tx, account); err != nil {
		log.Printf("CreateWithTx: failed to create account: %v", err)
		return nil, err
	}

	return toAccountResponse(account), nil
}

// GetByID retrieves an account by ID
func (s *service) GetByID(ctx context.Context, id string) (*AccountResponse, error) {
	account, err := s.repo.GetByID(ctx, id)
	if err != nil {
		if errors.Is(err, repository.ErrAccountNotFound) {
			return nil, ErrAccountNotFound
		}
		log.Printf("GetByID: failed to get account: %v", err)
		return nil, err
	}

	return toAccountResponse(account), nil
}

// GetByPublicKey retrieves an account by its public key
func (s *service) GetByPublicKey(ctx context.Context, publicKey string) (*AccountResponse, error) {
	account, err := s.repo.GetByPublicKey(ctx, publicKey)
	if err != nil {
		if errors.Is(err, repository.ErrAccountNotFound) {
			return nil, ErrAccountNotFound
		}
		log.Printf("GetByPublicKey: failed to get account: %v", err)
		return nil, err
	}

	return toAccountResponse(account), nil
}

// GetByUserID retrieves an account for a user
func (s *service) GetByUserID(ctx context.Context, userID string) (*AccountResponse, error) {
	// Check if user exists
	_, err := s.userRepo.GetByID(ctx, userID)
	if err != nil {
		if errors.Is(err, repository.ErrUserNotFound) {
			return nil, services.ErrNotFound
		}
		log.Printf("GetByUserID: failed to get user: %v", err)
		return nil, err
	}

	// Get account by user ID
	account, err := s.repo.GetByUserID(ctx, userID)
	if err != nil {
		if errors.Is(err, repository.ErrAccountNotFound) {
			return nil, ErrAccountNotFound
		}
		log.Printf("GetByUserID: failed to get account: %v", err)
		return nil, err
	}

	return toAccountResponse(account), nil
}

// GetNextAccountIndex retrieves the next available account index for a user
func (s *service) GetNextAccountIndex(ctx context.Context, userID string) (int, error) {
	// Check if user exists
	_, err := s.userRepo.GetByID(ctx, userID)
	if err != nil {
		if errors.Is(err, repository.ErrUserNotFound) {
			return 0, services.ErrNotFound
		}
		log.Printf("GetNextAccountIndex: failed to get user: %v", err)
		return 0, err
	}

	// Get next account index
	accountIndex, err := s.repo.GetNextAccountIndex(ctx, userID)
	if err != nil {
		log.Printf("GetNextAccountIndex: failed to get next account index: %v", err)
		return 0, err
	}

	return accountIndex, nil
}

// GetNextAccountIndexWithTx retrieves the next available account index globally within a transaction
func (s *service) GetNextAccountIndexWithTx(ctx context.Context, tx *gorm.DB) (int, error) {
	// Get next account index from repository
	accountIndex, err := s.repo.GetNextAccountIndexWithTx(ctx, tx)
	if err != nil {
		log.Printf("GetNextAccountIndexWithTx: failed to get next account index: %v", err)
		return 0, err
	}

	return accountIndex, nil
}

// UpdateStatus updates account status with transition validation
func (s *service) UpdateStatus(ctx context.Context, id string, req UpdateAccountStatusRequest) (*AccountResponse, error) {
	// Validate status
	if !validStatuses[req.Status] {
		return nil, ErrInvalidStatus
	}

	// Get account
	account, err := s.repo.GetByID(ctx, id)
	if err != nil {
		if errors.Is(err, repository.ErrAccountNotFound) {
			return nil, ErrAccountNotFound
		}
		log.Printf("UpdateStatus: failed to get account: %v", err)
		return nil, err
	}

	// Check if account is deleted
	if account.DeletedAt != nil {
		return nil, ErrCannotModifyDeletedAccount
	}

	// Validate state transition
	if !s.isValidStatusTransition(account.Status, req.Status) {
		return nil, ErrInvalidStatusTransition
	}

	// Update status
	account.Status = req.Status

	// Update in database
	if err := s.repo.Update(ctx, account); err != nil {
		log.Printf("UpdateStatus: failed to update account: %v", err)
		return nil, err
	}

	return toAccountResponse(account), nil
}

// Delete soft deletes an account
func (s *service) Delete(ctx context.Context, id string) error {
	// Get account
	account, err := s.repo.GetByID(ctx, id)
	if err != nil {
		if errors.Is(err, repository.ErrAccountNotFound) {
			return ErrAccountNotFound
		}
		log.Printf("Delete: failed to get account: %v", err)
		return err
	}

	// Check if account is already deleted
	if account.DeletedAt != nil {
		return ErrAccountAlreadyDeleted
	}

	// Delete account
	if err := s.repo.Delete(ctx, id); err != nil {
		log.Printf("Delete: failed to delete account: %v", err)
		return err
	}

	return nil
}

// Restore restores a soft-deleted account
func (s *service) Restore(ctx context.Context, id string) error {
	// Restore account
	if err := s.repo.Restore(ctx, id); err != nil {
		if errors.Is(err, repository.ErrAccountNotFound) {
			return ErrAccountNotFound
		}
		log.Printf("Restore: failed to restore account: %v", err)
		return err
	}

	return nil
}

// --- Helper functions ---

// validateCreateRequest validates the create account request
func (s *service) validateCreateRequest(req CreateAccountRequest) error {
	if req.UserID == "" {
		return ErrInvalidInput
	}

	if req.PublicKey == "" {
		return ErrInvalidPublicKey
	}

	// Basic Stellar public key validation (starts with 'G' and is 56 characters)
	if len(req.PublicKey) != 56 || req.PublicKey[0] != 'G' {
		return ErrInvalidPublicKey
	}

	return nil
}

// isValidStatusTransition checks if a status transition is valid
func (s *service) isValidStatusTransition(from, to string) bool {
	if from == to {
		return true // No change is valid
	}

	validTransitions, exists := validStatusTransitions[from]
	if !exists {
		return false
	}

	return slices.Contains(validTransitions, to)
}

// toAccountResponse converts an account model to response DTO
func toAccountResponse(account *models.Account) *AccountResponse {
	return &AccountResponse{
		ID:           account.ID,
		UserID:       account.UserID,
		PublicKey:    account.PublicKey,
		AccountIndex: account.AccountIndex,
		Status:       account.Status,
		CreatedAt:    account.CreatedAt,
		UpdatedAt:    account.UpdatedAt,
	}
}
