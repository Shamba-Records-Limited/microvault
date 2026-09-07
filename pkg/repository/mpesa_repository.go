package repository

import (
	"context"
	"errors"
	"log"
	"time"

	"github.com/jackc/pgconn"
	"gorm.io/gorm"

	pkgErrors "github.com/Shamba-Records-Limited/microvault/pkg/errors"
	"github.com/Shamba-Records-Limited/microvault/pkg/models"
)

// Errors for MpesaTransactionRepository.
var (
	ErrMpesaConflict  = errors.New("mpesa transaction already recorded")
	ErrFailedToRecord = errors.New("failed to record mpesa observation")
	ErrMpesaNotFound  = errors.New("mpesa transaction not found")
	ErrFailedToPoll   = errors.New("failed to fetch due mpesa observations")
)

// MpesaTransactionRepository persists inbound M-Pesa observations.
//
// It exists to make confirm-before-credit enforceable, so Create is careful to
// give only the "recorded" state and Confirm marks the verified one.
type MpesaTransactionRepository interface {
	// Record inserts a new observation. A duplicate TransID returns
	// ErrMpesaConflict, which is the idempotency check the controller relies
	// on.
	Record(ctx context.Context, tx *models.MpesaTransaction) error

	// GetByTransID fetches one observation by receipt, e.g. to confirm a
	// callback against.
	GetByTransID(ctx context.Context, transID string) (*models.MpesaTransaction, error)

	// DuePoll returns unconfirmed observations whose NextPollAt has come due,
	// newest last so a stuck poll does not starve newer ones.
	DuePoll(ctx context.Context, limit int) ([]*models.MpesaTransaction, error)

	// Confirm marks an observation verified and stamps how, which is what
	// moves it out of the poller's due set.
	Confirm(ctx context.Context, transID string, via models.MpesaTransactionConfirmVia, loanID string) error

	// UpdatePoll writes the next poll moment — the cadence lives in a column,
	// not in in-memory timers, per the mgpoller reasoning.
	UpdatePoll(ctx context.Context, transID string, at time.Time) error

	// StopPoll clears the poll moment, taking the observation out of the due
	// set for good: the terminal-failure path, where re-asking Daraja can never
	// change the answer.
	StopPoll(ctx context.Context, transID string) error

	// GetLoanIDByReference resolves a loan reference to a loan ID on the shared
	// platform DB. Raw SQL rather than the credit module's repository because
	// importing the lending module would invert the layering — the same
	// reasoning mgpoller documents for its loan status constants. Returns ""
	// when the reference resolves to nothing.
	GetLoanIDByReference(ctx context.Context, reference string) (string, error)

	// SetReversalState writes the reversal lifecycle state.
	SetReversalState(ctx context.Context, transID string, state models.MpesaTransactionReversal) error

	// UpdateFields sets the mutable fields a Pull reconciler fills in —
	// unmasked MSISDN, loan attribution, confirmed status.
	UpdateFields(ctx context.Context, tx *models.MpesaTransaction) error
}

type mpesaTransactionRepository struct {
	db *gorm.DB
}

// NewMpesaTransactionRepository creates a new MpesaTransactionRepository.
func NewMpesaTransactionRepository(db *gorm.DB) (MpesaTransactionRepository, error) {
	if db == nil {
		return nil, pkgErrors.ErrNilDB
	}
	return &mpesaTransactionRepository{db: db}, nil
}

// Record inserts an observation, converting a unique violation on trans_id
// into the idempotency sentinel.
func (r *mpesaTransactionRepository) Record(ctx context.Context, tx *models.MpesaTransaction) error {
	var pgErr *pgconn.PgError
	result := r.db.WithContext(ctx).Create(tx)
	if result.Error != nil {
		if errors.As(result.Error, &pgErr) && pgErr.Code == "23505" {
			return ErrMpesaConflict
		}
		log.Printf("MpesaTransactionRepository.Record: database error: %v", result.Error)
		return ErrFailedToRecord
	}
	return nil
}

// GetByTransID fetches one observation by receipt.
func (r *mpesaTransactionRepository) GetByTransID(ctx context.Context, transID string) (*models.MpesaTransaction, error) {
	var tx models.MpesaTransaction
	result := r.db.WithContext(ctx).Where("trans_id = ?", transID).First(&tx)
	if errors.Is(result.Error, gorm.ErrRecordNotFound) {
		return nil, ErrMpesaNotFound
	}
	if result.Error != nil {
		log.Printf("MpesaTransactionRepository.GetByTransID: database error: %v", result.Error)
		return nil, ErrMpesaNotFound
	}
	return &tx, nil
}

// DuePoll returns unconfirmed observations whose NextPollAt has come due.
func (r *mpesaTransactionRepository) DuePoll(ctx context.Context, limit int) ([]*models.MpesaTransaction, error) {
	var txs []*models.MpesaTransaction
	result := r.db.WithContext(ctx).
		Where("confirmed = false AND next_poll_at IS NOT NULL AND next_poll_at <= ?", time.Now()).
		Order("next_poll_at").
		Limit(limit).
		Find(&txs)
	if result.Error != nil {
		return nil, ErrFailedToPoll
	}
	return txs, nil
}

// Confirm marks an observation verified and stamps how.
func (r *mpesaTransactionRepository) Confirm(ctx context.Context, transID string, via models.MpesaTransactionConfirmVia, loanID string) error {
	result := r.db.WithContext(ctx).
		Model(&models.MpesaTransaction{}).
		Where("trans_id = ?", transID).
		Updates(map[string]any{"confirmed": true, "confirmed_via": string(via), "loan_id": loanID, "next_poll_at": nil})
	if result.Error != nil {
		log.Printf("MpesaTransactionRepository.Confirm: database error: %v", result.Error)
		return ErrFailedToRecord
	}
	if result.RowsAffected == 0 {
		return ErrMpesaNotFound
	}
	return nil
}

// UpdatePoll writes the next poll moment.
func (r *mpesaTransactionRepository) UpdatePoll(ctx context.Context, transID string, at time.Time) error {
	result := r.db.WithContext(ctx).
		Model(&models.MpesaTransaction{}).
		Where("trans_id = ?", transID).
		Update("next_poll_at", at)
	if result.Error != nil {
		return ErrFailedToPoll
	}
	return nil
}

// SetReversalState writes the reversal lifecycle state.
// GetLoanIDByReference resolves a loan reference against the loans table,
// trying the current format first, then the legacy column.
func (r *mpesaTransactionRepository) GetLoanIDByReference(ctx context.Context, reference string) (string, error) {
	var loanID string
	result := r.db.WithContext(ctx).
		Raw(`SELECT id FROM loans
		     WHERE deleted_at IS NULL
		       AND (loan_reference = ? OR legacy_loan_reference = ?)
		     LIMIT 1`, reference, reference).
		Scan(&loanID)
	if result.Error != nil {
		log.Printf("MpesaTransactionRepository.GetLoanIDByReference: database error: %v", result.Error)
		return "", ErrFailedToRecord
	}
	return loanID, nil
}

// StopPoll clears the poll moment, taking the observation out of the due set.
func (r *mpesaTransactionRepository) StopPoll(ctx context.Context, transID string) error {
	result := r.db.WithContext(ctx).
		Model(&models.MpesaTransaction{}).
		Where("trans_id = ?", transID).
		Update("next_poll_at", nil)
	if result.Error != nil {
		return ErrFailedToPoll
	}
	return nil
}

func (r *mpesaTransactionRepository) SetReversalState(ctx context.Context, transID string, state models.MpesaTransactionReversal) error {
	result := r.db.WithContext(ctx).
		Model(&models.MpesaTransaction{}).
		Where("trans_id = ?", transID).
		Update("reversal_state", string(state))
	if result.Error != nil {
		return ErrFailedToRecord
	}
	return nil
}

// UpdateFields sets the mutable fields a Pull reconciler fills in.
func (r *mpesaTransactionRepository) UpdateFields(ctx context.Context, tx *models.MpesaTransaction) error {
	result := r.db.WithContext(ctx).
		Model(&models.MpesaTransaction{}).
		Where("trans_id = ?", tx.TransID).
		Updates(map[string]any{
			"msisdn_full": tx.MsidnFull,
			"loan_id":     tx.LoanID,
			"confirmed":   tx.Confirmed,
		})
	if result.Error != nil {
		return ErrFailedToRecord
	}
	return nil
}
