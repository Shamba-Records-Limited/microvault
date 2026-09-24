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

// Errors for AirtelTransactionRepository.
var (
	ErrAirtelNotFound       = errors.New("airtel transaction not found")
	ErrFailedToRecordAirtel = errors.New("failed to record airtel observation")
	ErrFailedToPollAirtel   = errors.New("failed to fetch due airtel observations")
)

// AirtelTransactionRepository persists inbound Airtel Money observations.
//
// It differs from MpesaTransactionRepository in one structural way, and the
// difference runs through the whole interface: an Airtel transaction can
// report more than once. Airtel's callback carries intermediate or final
// status, so the write path is an upsert keyed by our own transaction id
// rather than an insert that treats a second notification as a conflict to
// swallow.
type AirtelTransactionRepository interface {
	// RecordCallback upserts an observation from a callback. A second
	// callback for the same transaction updates the existing row rather than
	// conflicting, which is what makes an intermediate-then-final pair one
	// payment instead of two.
	//
	// A row already confirmed by an enquiry is left untouched: the enquiry is
	// the authority, and a late callback must not walk a settled row back.
	RecordCallback(ctx context.Context, tx *models.AirtelTransaction) error

	// GetByPartnerID fetches one observation by the id we generated.
	GetByPartnerID(ctx context.Context, partnerTxnID string) (*models.AirtelTransaction, error)

	// GetByAirtelMoneyID fetches one observation by Airtel's receipt. Only
	// settled rows have one.
	GetByAirtelMoneyID(ctx context.Context, airtelMoneyID string) (*models.AirtelTransaction, error)

	// DuePoll returns unconfirmed observations whose NextPollAt has come due,
	// oldest first so a backlog drains in arrival order.
	DuePoll(ctx context.Context, limit int) ([]*models.AirtelTransaction, error)

	// Confirm marks an observation verified by an independent check and
	// records the receipt that check disclosed. Passing an empty
	// airtelMoneyID leaves the column alone rather than clearing it: a
	// confirmation of a failed transaction has no receipt to record, and
	// nulling one that a callback already supplied would lose the only key a
	// refund accepts.
	Confirm(ctx context.Context, partnerTxnID string, via models.AirtelTransactionConfirmVia, statusCode, airtelMoneyID, loanID string) error

	// UpdatePoll writes the next poll moment and counts the attempt.
	UpdatePoll(ctx context.Context, partnerTxnID string, at time.Time) error

	// StopPoll clears the poll moment, taking the observation out of the due
	// set for good.
	StopPoll(ctx context.Context, partnerTxnID string) error

	// UpsertFromSummary records a settled transaction the reconciliation
	// sweep found, or fills in what it learned about one already staged.
	// This is how a payment whose callback was lost is still credited.
	UpsertFromSummary(ctx context.Context, tx *models.AirtelTransaction) error

	// ListUnappliedConfirmed returns confirmed, loan-attributed observations
	// not yet converted toward a loan's repayment progress, oldest first.
	ListUnappliedConfirmed(ctx context.Context, limit int) ([]*models.AirtelTransaction, error)

	// SetAppliedStroops records the converted figure for one observation.
	SetAppliedStroops(ctx context.Context, id string, stroops int64) error

	// SumAppliedStroopsByLoan totals every observation already converted for
	// a loan.
	SumAppliedStroopsByLoan(ctx context.Context, loanID string) (int64, error)
}

type airtelTransactionRepository struct {
	db *gorm.DB
}

// NewAirtelTransactionRepository creates a new AirtelTransactionRepository.
func NewAirtelTransactionRepository(db *gorm.DB) (AirtelTransactionRepository, error) {
	if db == nil {
		return nil, pkgErrors.ErrNilDB
	}
	return &airtelTransactionRepository{db: db}, nil
}

// RecordCallback inserts, falling back to an update when the transaction is
// already staged.
func (r *airtelTransactionRepository) RecordCallback(ctx context.Context, tx *models.AirtelTransaction) error {
	result := r.db.WithContext(ctx).Create(tx)
	if result.Error == nil {
		return nil
	}

	var pgErr *pgconn.PgError
	if !errors.As(result.Error, &pgErr) || pgErr.Code != "23505" {
		log.Printf("AirtelTransactionRepository.RecordCallback: database error: %v", result.Error)
		return ErrFailedToRecordAirtel
	}
	return r.updateFromCallback(ctx, tx)
}

// updateFromCallback applies a later callback to an existing row, refusing to
// touch one an enquiry has already settled.
func (r *airtelTransactionRepository) updateFromCallback(ctx context.Context, tx *models.AirtelTransaction) error {
	updates := map[string]any{
		"status_code":   tx.StatusCode,
		"raw_payload":   tx.RawPayload,
		"trans_time":    tx.TransTime,
		"hash_verified": tx.HashVerified,
		"hash_variant":  tx.HashVariant,
		"updated_at":    time.Now(),
	}
	// Only ever fill the receipt in, never clear it. An intermediate
	// callback carries none, and it must not erase one a later — or earlier
	// — notification supplied.
	if tx.AirtelMoneyID != nil && *tx.AirtelMoneyID != "" {
		updates["airtel_money_id"] = *tx.AirtelMoneyID
	}

	result := r.db.WithContext(ctx).
		Model(&models.AirtelTransaction{}).
		Where("partner_txn_id = ? AND confirmed = false", tx.PartnerTxnID).
		Updates(updates)
	if result.Error != nil {
		log.Printf("AirtelTransactionRepository.updateFromCallback: database error: %v", result.Error)
		return ErrFailedToRecordAirtel
	}
	// Zero rows means the row exists but is already confirmed. That is a
	// late callback arriving after the enquiry settled it, which is normal
	// and not an error.
	return nil
}

// GetByPartnerID fetches one observation by our own id.
func (r *airtelTransactionRepository) GetByPartnerID(ctx context.Context, partnerTxnID string) (*models.AirtelTransaction, error) {
	var tx models.AirtelTransaction
	result := r.db.WithContext(ctx).Where("partner_txn_id = ?", partnerTxnID).First(&tx)
	if errors.Is(result.Error, gorm.ErrRecordNotFound) {
		return nil, ErrAirtelNotFound
	}
	if result.Error != nil {
		log.Printf("AirtelTransactionRepository.GetByPartnerID: database error: %v", result.Error)
		return nil, ErrAirtelNotFound
	}
	return &tx, nil
}

// GetByAirtelMoneyID fetches one observation by Airtel's receipt.
func (r *airtelTransactionRepository) GetByAirtelMoneyID(ctx context.Context, airtelMoneyID string) (*models.AirtelTransaction, error) {
	var tx models.AirtelTransaction
	result := r.db.WithContext(ctx).Where("airtel_money_id = ?", airtelMoneyID).First(&tx)
	if errors.Is(result.Error, gorm.ErrRecordNotFound) {
		return nil, ErrAirtelNotFound
	}
	if result.Error != nil {
		log.Printf("AirtelTransactionRepository.GetByAirtelMoneyID: database error: %v", result.Error)
		return nil, ErrAirtelNotFound
	}
	return &tx, nil
}

// DuePoll returns unconfirmed observations whose NextPollAt has come due.
func (r *airtelTransactionRepository) DuePoll(ctx context.Context, limit int) ([]*models.AirtelTransaction, error) {
	var txs []*models.AirtelTransaction
	result := r.db.WithContext(ctx).
		Where("confirmed = false AND next_poll_at IS NOT NULL AND next_poll_at <= ?", time.Now()).
		Order("next_poll_at").
		Limit(limit).
		Find(&txs)
	if result.Error != nil {
		log.Printf("AirtelTransactionRepository.DuePoll: database error: %v", result.Error)
		return nil, ErrFailedToPollAirtel
	}
	return txs, nil
}

// Confirm marks an observation verified and stamps how.
func (r *airtelTransactionRepository) Confirm(ctx context.Context, partnerTxnID string, via models.AirtelTransactionConfirmVia, statusCode, airtelMoneyID, loanID string) error {
	updates := map[string]any{
		"confirmed":     true,
		"confirmed_via": string(via),
		"status_code":   statusCode,
		"next_poll_at":  nil,
		"updated_at":    time.Now(),
	}
	if airtelMoneyID != "" {
		updates["airtel_money_id"] = airtelMoneyID
	}
	if loanID != "" {
		updates["loan_id"] = loanID
	}

	result := r.db.WithContext(ctx).
		Model(&models.AirtelTransaction{}).
		Where("partner_txn_id = ?", partnerTxnID).
		Updates(updates)
	if result.Error != nil {
		log.Printf("AirtelTransactionRepository.Confirm: database error: %v", result.Error)
		return ErrFailedToRecordAirtel
	}
	if result.RowsAffected == 0 {
		return ErrAirtelNotFound
	}
	return nil
}

// UpdatePoll writes the next poll moment and counts the attempt.
func (r *airtelTransactionRepository) UpdatePoll(ctx context.Context, partnerTxnID string, at time.Time) error {
	result := r.db.WithContext(ctx).
		Model(&models.AirtelTransaction{}).
		Where("partner_txn_id = ?", partnerTxnID).
		Updates(map[string]any{
			"next_poll_at":  at,
			"poll_attempts": gorm.Expr("poll_attempts + 1"),
			"updated_at":    time.Now(),
		})
	if result.Error != nil {
		log.Printf("AirtelTransactionRepository.UpdatePoll: database error: %v", result.Error)
		return ErrFailedToPollAirtel
	}
	return nil
}

// StopPoll clears the poll moment.
func (r *airtelTransactionRepository) StopPoll(ctx context.Context, partnerTxnID string) error {
	result := r.db.WithContext(ctx).
		Model(&models.AirtelTransaction{}).
		Where("partner_txn_id = ?", partnerTxnID).
		Update("next_poll_at", nil)
	if result.Error != nil {
		log.Printf("AirtelTransactionRepository.StopPoll: database error: %v", result.Error)
		return ErrFailedToPollAirtel
	}
	return nil
}

// UpsertFromSummary records what the reconciliation sweep found.
//
// The sweep only ever reports settled transactions, so what it learns is
// authoritative: it confirms the row. A transaction it finds that was never
// staged is a payment whose callback was lost, and inserting it here is the
// only way that payment is ever credited.
func (r *airtelTransactionRepository) UpsertFromSummary(ctx context.Context, tx *models.AirtelTransaction) error {
	result := r.db.WithContext(ctx).Create(tx)
	if result.Error == nil {
		return nil
	}

	var pgErr *pgconn.PgError
	if !errors.As(result.Error, &pgErr) || pgErr.Code != "23505" {
		log.Printf("AirtelTransactionRepository.UpsertFromSummary: database error: %v", result.Error)
		return ErrFailedToRecordAirtel
	}

	updates := map[string]any{
		"status_code":   tx.StatusCode,
		"confirmed":     true,
		"confirmed_via": string(models.AirtelConfirmViaSummary),
		"next_poll_at":  nil,
		"updated_at":    time.Now(),
	}
	if tx.AirtelMoneyID != nil && *tx.AirtelMoneyID != "" {
		updates["airtel_money_id"] = *tx.AirtelMoneyID
	}
	if tx.LoanID != nil && *tx.LoanID != "" {
		updates["loan_id"] = *tx.LoanID
	}

	update := r.db.WithContext(ctx).
		Model(&models.AirtelTransaction{}).
		Where("partner_txn_id = ?", tx.PartnerTxnID).
		Updates(updates)
	if update.Error != nil {
		log.Printf("AirtelTransactionRepository.UpsertFromSummary: database error: %v", update.Error)
		return ErrFailedToRecordAirtel
	}
	return nil
}

// ListUnappliedConfirmed returns the settlement sweep's queue.
func (r *airtelTransactionRepository) ListUnappliedConfirmed(ctx context.Context, limit int) ([]*models.AirtelTransaction, error) {
	var txs []*models.AirtelTransaction
	result := r.db.WithContext(ctx).
		Where("confirmed = true AND loan_id IS NOT NULL AND applied_stroops IS NULL").
		Order("trans_time ASC").
		Limit(limit).
		Find(&txs)
	if result.Error != nil {
		log.Printf("AirtelTransactionRepository.ListUnappliedConfirmed: database error: %v", result.Error)
		return nil, ErrFailedToRecordAirtel
	}
	return txs, nil
}

// SetAppliedStroops records the converted figure for one observation.
func (r *airtelTransactionRepository) SetAppliedStroops(ctx context.Context, id string, stroops int64) error {
	result := r.db.WithContext(ctx).
		Model(&models.AirtelTransaction{}).
		Where("id = ?", id).
		Update("applied_stroops", stroops)
	if result.Error != nil {
		log.Printf("AirtelTransactionRepository.SetAppliedStroops: database error: %v", result.Error)
		return ErrFailedToRecordAirtel
	}
	if result.RowsAffected == 0 {
		return ErrAirtelNotFound
	}
	return nil
}

// SumAppliedStroopsByLoan totals every observation already converted for a
// loan. COALESCE guards the zero-rows case, where SUM scans as NULL.
func (r *airtelTransactionRepository) SumAppliedStroopsByLoan(ctx context.Context, loanID string) (int64, error) {
	var total int64
	result := r.db.WithContext(ctx).
		Model(&models.AirtelTransaction{}).
		Where("loan_id = ? AND applied_stroops IS NOT NULL", loanID).
		Select("COALESCE(SUM(applied_stroops), 0)").
		Scan(&total)
	if result.Error != nil {
		log.Printf("AirtelTransactionRepository.SumAppliedStroopsByLoan: database error: %v", result.Error)
		return 0, ErrFailedToRecordAirtel
	}
	return total, nil
}
