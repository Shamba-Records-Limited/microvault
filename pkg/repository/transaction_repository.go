package repository

import (
	"context"
	"errors"
	"log"
	"time"

	pkgErrors "github.com/Shamba-Records-Limited/microvault/pkg/errors"
	"github.com/Shamba-Records-Limited/microvault/pkg/models"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// Common errors for TransactionRepository
var (
	ErrTransactionNotFound             = errors.New("transaction not found")
	ErrFailedToCreateTransaction       = errors.New("failed to create transaction")
	ErrFailedToCreateBatchTransactions = errors.New("failed to create batch transactions")
	ErrFailedToGetTransaction          = errors.New("failed to get transaction")
	ErrFailedToGetTransactionByHash    = errors.New("failed to get transaction by stellar hash")
	ErrFailedToGetTransactionsByLoanID = errors.New("failed to get transactions by loan ID")
	ErrFailedToGetTransactionsByUserID = errors.New("failed to get transactions by user ID")
	ErrFailedToGetTransactionsByStatus = errors.New("failed to get transactions by status")
	ErrFailedToUpdateTransaction       = errors.New("failed to update transaction")
)

// TransactionRepository defines the interface for transaction data access
type TransactionRepository interface {
	// Create operations
	Create(ctx context.Context, tx *models.Transaction) error
	BatchCreate(ctx context.Context, txs []*models.Transaction) error

	// Read operations
	GetByID(ctx context.Context, id string) (*models.Transaction, error)
	GetByStellarHash(ctx context.Context, txHash string) (*models.Transaction, error)
	GetByExternalID(ctx context.Context, externalID string) (*models.Transaction, error)
	GetByLoanID(ctx context.Context, loanID string, limit, offset int) ([]*models.Transaction, error)
	GetByUserID(ctx context.Context, userID string, limit, offset int) ([]*models.Transaction, error)
	GetByStatus(ctx context.Context, status string, limit, offset int) ([]*models.Transaction, error)

	// Update operations
	Update(ctx context.Context, tx *models.Transaction) error
}

// transactionRepository represents a repository for managing transactions
type transactionRepository struct {
	db *gorm.DB
}

// NewTransactionRepository creates a new instance of TransactionRepository
func NewTransactionRepository(db *gorm.DB) (TransactionRepository, error) {
	if db == nil {
		return nil, pkgErrors.ErrNilDB
	}
	return &transactionRepository{db: db}, nil
}

// --- Create Operations ---

// Create creates a new transaction
func (r *transactionRepository) Create(ctx context.Context, tx *models.Transaction) error {
	result := r.db.WithContext(ctx).Create(tx)
	if result.Error != nil {
		log.Printf("Create: database error: %v", result.Error)
		return ErrFailedToCreateTransaction
	}
	return nil
}

// BatchCreate creates multiple transactions in a single batch
func (r *transactionRepository) BatchCreate(ctx context.Context, txs []*models.Transaction) error {
	if len(txs) == 0 {
		return nil
	}

	result := r.db.WithContext(ctx).Create(txs)
	if result.Error != nil {
		log.Printf("BatchCreate: database error: %v", result.Error)
		return ErrFailedToCreateBatchTransactions
	}
	return nil
}

// --- Read Operations ---

// GetByID retrieves a transaction by its ID
func (r *transactionRepository) GetByID(ctx context.Context, id string) (*models.Transaction, error) {
	var tx models.Transaction
	result := r.db.WithContext(ctx).
		Where("id = ?", id).
		First(&tx)
	if errors.Is(result.Error, gorm.ErrRecordNotFound) {
		return nil, ErrTransactionNotFound
	}
	if result.Error != nil {
		log.Printf("GetByID: database error: %v", result.Error)
		return nil, ErrFailedToGetTransaction
	}
	return &tx, nil
}

// GetByStellarHash retrieves a transaction by its Stellar transaction hash
func (r *transactionRepository) GetByStellarHash(ctx context.Context, txHash string) (*models.Transaction, error) {
	var tx models.Transaction
	result := r.db.WithContext(ctx).
		Session(&gorm.Session{Logger: r.db.Logger.LogMode(logger.Silent)}).
		Where("stellar_tx_hash = ?", txHash).
		First(&tx)
	if errors.Is(result.Error, gorm.ErrRecordNotFound) {
		return nil, ErrTransactionNotFound
	}
	if result.Error != nil {
		log.Printf("GetByStellarHash: database error: %v", result.Error)
		return nil, ErrFailedToGetTransactionByHash
	}
	return &tx, nil
}

// GetByExternalID retrieves a transaction by its external ID
func (r *transactionRepository) GetByExternalID(ctx context.Context, externalID string) (*models.Transaction, error) {
	var tx models.Transaction
	result := r.db.WithContext(ctx).
		Session(&gorm.Session{Logger: r.db.Logger.LogMode(logger.Silent)}).
		Where("external_id = ?", externalID).
		First(&tx)
	if errors.Is(result.Error, gorm.ErrRecordNotFound) {
		return nil, nil // Not found is not an error for idempotency checks
	}
	if result.Error != nil {
		log.Printf("GetByExternalID: database error: %v", result.Error)
		return nil, ErrFailedToGetTransaction
	}
	return &tx, nil
}

// GetByLoanID retrieves transactions by loan ID
func (r *transactionRepository) GetByLoanID(ctx context.Context, loanID string, limit, offset int) ([]*models.Transaction, error) {
	var transactions []*models.Transaction
	result := r.db.WithContext(ctx).
		Where("loan_id = ?", loanID).
		Order("created_at DESC").
		Limit(limit).
		Offset(offset).
		Find(&transactions)
	if result.Error != nil {
		log.Printf("GetByLoanID: database error: %v", result.Error)
		return nil, ErrFailedToGetTransactionsByLoanID
	}
	return transactions, nil
}

// GetByUserID retrieves transactions by user ID
func (r *transactionRepository) GetByUserID(ctx context.Context, userID string, limit, offset int) ([]*models.Transaction, error) {
	var transactions []*models.Transaction
	result := r.db.WithContext(ctx).
		Where("user_id = ?", userID).
		Order("created_at DESC").
		Limit(limit).
		Offset(offset).
		Find(&transactions)
	if result.Error != nil {
		log.Printf("GetByUserID: database error: %v", result.Error)
		return nil, ErrFailedToGetTransactionsByUserID
	}
	return transactions, nil
}

// GetByStatus retrieves transactions by status
func (r *transactionRepository) GetByStatus(ctx context.Context, status string, limit, offset int) ([]*models.Transaction, error) {
	var transactions []*models.Transaction
	result := r.db.WithContext(ctx).
		Where("status = ?", status).
		Order("created_at DESC").
		Limit(limit).
		Offset(offset).
		Find(&transactions)
	if result.Error != nil {
		log.Printf("GetByStatus: database error: %v", result.Error)
		return nil, ErrFailedToGetTransactionsByStatus
	}
	return transactions, nil
}

// --- Update Operations ---

// Update updates a transaction
func (r *transactionRepository) Update(ctx context.Context, tx *models.Transaction) error {
	result := r.db.WithContext(ctx).
		Model(tx).
		Where("id = ?", tx.ID).
		Updates(map[string]interface{}{
			"stellar_tx_hash":   tx.StellarTxHash,
			"stellar_ledger":    tx.StellarLedger,
			"stellar_status":    tx.StellarStatus,
			"external_id":       tx.ExternalID,
			"external_provider": tx.ExternalProvider,
			"external_status":   tx.ExternalStatus,
			"status":            tx.Status,
			"description":       tx.Description,
			"metadata":          tx.Metadata,
			"updated_at":        time.Now(),
		})
	if result.RowsAffected == 0 {
		return ErrTransactionNotFound
	}
	if result.Error != nil {
		log.Printf("Update: database error: %v", result.Error)
		return ErrFailedToUpdateTransaction
	}
	return nil
}
