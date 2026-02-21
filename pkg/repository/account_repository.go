package repository

import (
	"context"
	"errors"
	"log"
	"time"

	pkgErrors "github.com/Shamba-Records-Limited/microvault/pkg/errors"
	"github.com/Shamba-Records-Limited/microvault/pkg/models"
	"gorm.io/gorm"
)

// Common errors for AccountRepository
var (
	ErrAccountNotFound           = errors.New("account not found")
	ErrFailedToCreateAccount     = errors.New("failed to create account")
	ErrFailedToGetAccount        = errors.New("failed to get account")
	ErrFailedToGetAccountsByUser = errors.New("failed to get accounts by user")
	ErrFailedToGetNextIndex      = errors.New("failed to get next account index")
	ErrFailedToUpdateAccount     = errors.New("failed to update account")
	ErrFailedToDeleteAccount     = errors.New("failed to delete account")
	ErrFailedToRestoreAccount    = errors.New("failed to restore account")
)

// AccountRepository defines the interface for account data access
type AccountRepository interface {
	// Create operations
	Create(ctx context.Context, account *models.Account) error
	CreateWithTx(ctx context.Context, tx *gorm.DB, account *models.Account) error

	// Read operations
	GetByID(ctx context.Context, id string) (*models.Account, error)
	GetByUserID(ctx context.Context, userID string) (*models.Account, error)
	GetByPublicKey(ctx context.Context, publicKey string) (*models.Account, error)
	GetNextAccountIndex(ctx context.Context, userID string) (int, error)
	GetNextAccountIndexWithTx(ctx context.Context, tx *gorm.DB) (int, error)

	// Update operations
	Update(ctx context.Context, account *models.Account) error
	Restore(ctx context.Context, id string) error

	// Delete operations
	Delete(ctx context.Context, id string) error
}

// accountRepository is a repository for managing accounts
type accountRepository struct {
	db *gorm.DB
}

// NewAccountRepository creates a new AccountRepository
func NewAccountRepository(db *gorm.DB) (AccountRepository, error) {
	if db == nil {
		return nil, pkgErrors.ErrNilDB
	}
	return &accountRepository{db: db}, nil
}

// --- Create Operations ---

// Create creates a new stellar account
func (r *accountRepository) Create(ctx context.Context, account *models.Account) error {
	result := r.db.WithContext(ctx).Create(account)
	if result.Error != nil {
		log.Printf("Create: database error: %v", result.Error)
		return ErrFailedToCreateAccount
	}
	return nil
}

// CreateWithTx creates a new stellar account with a transaction
func (r *accountRepository) CreateWithTx(ctx context.Context, tx *gorm.DB, account *models.Account) error {
	result := tx.Create(account)
	if result.Error != nil {
		log.Printf("CreateWithTx: database error: %v", result.Error)
		return ErrFailedToCreateAccount
	}
	return nil
}

// --- Read Operations ---

// GetByID retrieves an account by its ID
func (r *accountRepository) GetByID(ctx context.Context, id string) (*models.Account, error) {
	var account models.Account
	result := r.db.WithContext(ctx).
		Where("id = ? AND deleted_at IS NULL", id).
		First(&account)
	if errors.Is(result.Error, gorm.ErrRecordNotFound) {
		return nil, ErrAccountNotFound
	}
	if result.Error != nil {
		log.Printf("GetByID: database error: %v", result.Error)
		return nil, ErrFailedToGetAccount
	}
	return &account, nil
}

// GetByUserID retrieves account for a user
func (r *accountRepository) GetByUserID(ctx context.Context, userID string) (*models.Account, error) {
	var account models.Account
	result := r.db.WithContext(ctx).
		Where("user_id = ? AND deleted_at IS NULL", userID).
		First(&account)
	if errors.Is(result.Error, gorm.ErrRecordNotFound) {
		return nil, ErrAccountNotFound
	}
	if result.Error != nil {
		log.Printf("GetByUserID: database error: %v", result.Error)
		return nil, ErrFailedToGetAccount
	}
	return &account, nil
}

// GetByPublicKey retrieves an account by its public key
func (r *accountRepository) GetByPublicKey(ctx context.Context, publicKey string) (*models.Account, error) {
	var account models.Account
	result := r.db.WithContext(ctx).
		Where("public_key = ? AND deleted_at IS NULL", publicKey).
		First(&account)
	if errors.Is(result.Error, gorm.ErrRecordNotFound) {
		return nil, ErrAccountNotFound
	}
	if result.Error != nil {
		log.Printf("GetByPublicKey: database error: %v", result.Error)
		return nil, ErrFailedToGetAccount
	}
	return &account, nil
}

// GetByStatus retrieves accounts by status
func (r *accountRepository) GetByStatus(ctx context.Context, status string) (*models.Account, error) {
	var account models.Account
	result := r.db.WithContext(ctx).
		Where("status = ? AND deleted_at IS NULL", status).
		First(&account)
	if result.Error != nil {
		log.Printf("GetByStatus: database error: %v", result.Error)
		return nil, ErrFailedToGetAccount
	}
	return &account, nil
}

// GetNextAccountIndex retrieves the next available account index globally
// This ensures unique BIP44 derivation paths when using a single master seed
// Note: Index 0 is reserved for the admin/master account at m/44'/148'/0'
// User accounts start from index 1
func (r *accountRepository) GetNextAccountIndex(ctx context.Context, userID string) (int, error) {
	var maxIndex *int64
	result := r.db.WithContext(ctx).
		Model(&models.Account{}).
		Where("deleted_at IS NULL").
		Select("COALESCE(MAX(account_index), 0)").
		Scan(&maxIndex)
	if result.Error != nil {
		log.Printf("GetNextAccountIndex: database error: %v", result.Error)
		return 0, ErrFailedToGetNextIndex
	}

	if maxIndex != nil {
		nextIndex := int(*maxIndex)
		log.Printf("GetNextAccountIndex: returning global index %d (max was %d)", nextIndex, *maxIndex)
		return nextIndex, nil
	}

	log.Printf("GetNextAccountIndex: no accounts exist, returning index 1 (0 reserved for admin)")
	return 1, nil
}

// GetNextAccountIndexWithTx retrieves the next available account index globally within a transaction
// This ensures unique BIP44 derivation paths when using a single master seed
// Note: Index 0 is reserved for the admin/master account at m/44'/148'/0'
// User accounts start from index 1
func (r *accountRepository) GetNextAccountIndexWithTx(ctx context.Context, tx *gorm.DB) (int, error) {
	var maxIndex *int64
	result := tx.WithContext(ctx).
		Model(&models.Account{}).
		Where("deleted_at IS NULL").
		Select("COALESCE(MAX(account_index), 0)").
		Scan(&maxIndex)
	if result.Error != nil {
		log.Printf("GetNextAccountIndexWithTx: database error: %v", result.Error)
		return 0, ErrFailedToGetNextIndex
	}

	if maxIndex != nil {
		nextIndex := int(*maxIndex) + 1
		log.Printf("GetNextAccountIndexWithTx: returning global index %d (max was %d)", nextIndex, *maxIndex)
		return nextIndex, nil
	}

	log.Printf("GetNextAccountIndexWithTx: no accounts exist, returning index 1 (0 reserved for admin)")
	return 1, nil
}

// --- Update Operations ---

// Update updates an existing account
func (r *accountRepository) Update(ctx context.Context, account *models.Account) error {
	result := r.db.WithContext(ctx).
		Model(account).
		Where("id = ? AND deleted_at IS NULL", account.ID).
		Updates(map[string]interface{}{
			"status":     account.Status,
			"updated_at": time.Now(),
		})
	if result.RowsAffected == 0 {
		return ErrAccountNotFound
	}
	if result.Error != nil {
		log.Printf("Update: database error: %v", result.Error)
		return ErrFailedToUpdateAccount
	}
	return nil
}

// Restore restores a soft-deleted account
func (r *accountRepository) Restore(ctx context.Context, id string) error {
	result := r.db.WithContext(ctx).
		Unscoped().
		Model(&models.Account{}).
		Where("id = ?", id).
		Update("deleted_at", nil)
	if result.RowsAffected == 0 {
		return ErrAccountNotFound
	}
	if result.Error != nil {
		log.Printf("Restore: database error: %v", result.Error)
		return ErrFailedToRestoreAccount
	}
	return nil
}

// --- Delete Operations ---

// Delete soft deletes an existing stellar account
func (r *accountRepository) Delete(ctx context.Context, id string) error {
	result := r.db.WithContext(ctx).
		Model(&models.Account{}).
		Where("id = ? AND deleted_at IS NULL", id).
		Update("deleted_at", time.Now())
	if result.RowsAffected == 0 {
		return ErrAccountNotFound
	}
	if result.Error != nil {
		log.Printf("Delete: database error: %v", result.Error)
		return ErrFailedToDeleteAccount
	}
	return nil
}
