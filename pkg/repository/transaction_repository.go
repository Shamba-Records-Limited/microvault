package repository

import (
	"context"
	"errors"
	"log"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	pkgErrors "github.com/Shamba-Records-Limited/microvault/pkg/errors"
	"github.com/Shamba-Records-Limited/microvault/pkg/models"
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
	ErrFailedToListTransactions        = errors.New("failed to list transactions")
	ErrFailedToCountTransactions       = errors.New("failed to count transactions")
)

// TransactionRepository defines the interface for transaction data access
type TransactionRepository interface {
	// Create operations
	Create(ctx context.Context, tx *models.Transaction) error
	BatchCreate(ctx context.Context, txs []*models.Transaction) error

	// Read operations
	GetByID(ctx context.Context, id string) (*models.Transaction, error)
	GetByStellarHash(ctx context.Context, txHash string) (*models.Transaction, error)
	ListByExternalID(ctx context.Context, externalID string) ([]*models.Transaction, error)
	GetByLoanIDAndType(ctx context.Context, loanID, txType string) (*models.Transaction, error)
	GetByLoanID(ctx context.Context, loanID string, limit, offset int) ([]*models.Transaction, error)
	GetByUserID(ctx context.Context, userID string, limit, offset int) ([]*models.Transaction, error)
	GetByStatus(ctx context.Context, status string, limit, offset int) ([]*models.Transaction, error)

	// List returns transactions newest first, filtered by status and/or type
	// when either is given. Empty filters return everything.
	List(ctx context.Context, status, txType string, limit, offset int) ([]*models.Transaction, error)

	// Count returns the number of transactions matching the same filters List applies.
	Count(ctx context.Context, status, txType string) (int64, error)

	// Update operations
	Update(ctx context.Context, tx *models.Transaction) error

	// UpdateFields writes only the supplied columns, validated against the same
	// allow-list Update uses. Preferred for partial changes to avoid rewriting
	// the text/jsonb columns and all indexes on every touch.
	UpdateFields(ctx context.Context, id string, fields map[string]any) error
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

// ListByExternalID retrieves every transaction sharing a provider reference.
//
// One anchor transaction produces several legs — a MoneyGram cash pickup writes
// an anchor_transfer, an off_ramp and possibly a refund, all under the same
// request ID — so this returns a set. It is the reverse lookup: a provider
// quotes a reference and this finds what it touched.
func (r *transactionRepository) ListByExternalID(ctx context.Context, externalID string) ([]*models.Transaction, error) {
	var transactions []*models.Transaction
	result := r.db.WithContext(ctx).
		Session(&gorm.Session{Logger: r.db.Logger.LogMode(logger.Silent)}).
		Where("external_id = ?", externalID).
		Order("created_at ASC").
		Find(&transactions)
	if result.Error != nil {
		log.Printf("ListByExternalID: database error: %v", result.Error)
		return nil, ErrFailedToGetTransaction
	}
	return transactions, nil
}

// GetByLoanIDAndType retrieves the transaction of a given type for a loan,
// returning nil when there is none.
//
// This is how a webhook finds the row it needs to update. It replaces a lookup
// by external ID, which stopped identifying a single row once every leg of an
// anchor transaction began carrying the same provider reference.
func (r *transactionRepository) GetByLoanIDAndType(ctx context.Context, loanID, txType string) (*models.Transaction, error) {
	var tx models.Transaction
	result := r.db.WithContext(ctx).
		Session(&gorm.Session{Logger: r.db.Logger.LogMode(logger.Silent)}).
		Where("loan_id = ? AND tx_type = ?", loanID, txType).
		First(&tx)
	if errors.Is(result.Error, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if result.Error != nil {
		log.Printf("GetByLoanIDAndType: database error: %v", result.Error)
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

// List returns transactions newest first, filtered by status and/or tx_type.
func (r *transactionRepository) List(ctx context.Context, status, txType string, limit, offset int) ([]*models.Transaction, error) {
	var transactions []*models.Transaction
	query := r.db.WithContext(ctx)
	if status != "" {
		query = query.Where("status = ?", status)
	}
	if txType != "" {
		query = query.Where("tx_type = ?", txType)
	}
	result := query.
		Order("created_at DESC").
		Limit(limit).
		Offset(offset).
		Find(&transactions)
	if result.Error != nil {
		log.Printf("List: database error: %v", result.Error)
		return nil, ErrFailedToListTransactions
	}
	return transactions, nil
}

// Count returns the number of transactions matching List's filters.
func (r *transactionRepository) Count(ctx context.Context, status, txType string) (int64, error) {
	var count int64
	query := r.db.WithContext(ctx).Model(&models.Transaction{})
	if status != "" {
		query = query.Where("status = ?", status)
	}
	if txType != "" {
		query = query.Where("tx_type = ?", txType)
	}
	if err := query.Count(&count).Error; err != nil {
		log.Printf("Count: database error: %v", err)
		return 0, ErrFailedToCountTransactions
	}
	return count, nil
}

// --- Update Operations ---

// transactionUpdateMap is the single source of truth for which transaction
// columns may be written. Both Update (full rewrite) and UpdateFields (partial)
// derive from it, so the allow-list cannot drift from the writer.
func transactionUpdateMap(tx *models.Transaction) map[string]interface{} {
	return map[string]interface{}{
		"stellar_tx_hash":   tx.StellarTxHash,
		"stellar_ledger":    tx.StellarLedger,
		"external_id":       tx.ExternalID,
		"external_provider": tx.ExternalProvider,
		"external_status":   tx.ExternalStatus,
		"status":            tx.Status,
		"description":       tx.Description,
		"metadata":          tx.Metadata,
	}
}

// transactionUpdatableColumns is the set of columns transactionUpdateMap writes.
var transactionUpdatableColumns = func() map[string]bool {
	cols := make(map[string]bool)
	for k := range transactionUpdateMap(&models.Transaction{}) {
		cols[k] = true
	}
	return cols
}()

// IsTransactionUpdatableColumn reports whether col may be written via
// UpdateFields. Exposed for test-guarding the service-layer partial-update
// mapping against drift.
func IsTransactionUpdatableColumn(col string) bool {
	return transactionUpdatableColumns[col]
}

// Update rewrites every updatable column from the model. Prefer UpdateFields
// for partial changes: this table carries a text `description` and jsonb
// `metadata` plus 12 indexes, so a full rewrite is expensive for a one-field
// change (e.g. a stellar_status transition).
func (r *transactionRepository) Update(ctx context.Context, tx *models.Transaction) error {
	fields := transactionUpdateMap(tx)
	fields["updated_at"] = time.Now()
	return r.updateColumns(ctx, tx.ID, fields)
}

// UpdateFields writes only the supplied columns. Unknown columns are rejected.
func (r *transactionRepository) UpdateFields(ctx context.Context, id string, fields map[string]any) error {
	if len(fields) == 0 {
		return nil
	}
	out := make(map[string]interface{}, len(fields)+1)
	for k, v := range fields {
		if !transactionUpdatableColumns[k] {
			log.Printf("UpdateFields: rejected non-updatable column %q", k)
			return ErrFailedToUpdateTransaction
		}
		out[k] = v
	}
	out["updated_at"] = time.Now()
	return r.updateColumns(ctx, id, out)
}

// updateColumns applies a pre-validated column map to one transaction row.
func (r *transactionRepository) updateColumns(ctx context.Context, id string, fields map[string]interface{}) error {
	result := r.db.WithContext(ctx).
		Model(&models.Transaction{}).
		Where("id = ?", id).
		Updates(fields)
	if result.Error != nil {
		log.Printf("Update: database error: %v", result.Error)
		return ErrFailedToUpdateTransaction
	}
	if result.RowsAffected == 0 {
		return ErrTransactionNotFound
	}
	return nil
}
