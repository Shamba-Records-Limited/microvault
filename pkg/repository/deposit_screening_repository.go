package repository

import (
	"context"
	"errors"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	pkgErrors "github.com/Shamba-Records-Limited/microvault/pkg/errors"
	"github.com/Shamba-Records-Limited/microvault/pkg/models"
)

// DepositCounterparty is the counterparty context of a depositor address.
type DepositCounterparty struct {
	AddressID         string
	CustomerReference string
}

// DepositScreeningRepository queues and records vault deposit screenings.
type DepositScreeningRepository interface {
	// Enqueue stores a deposit to screen; a repeat of the same event is
	// ignored.
	Enqueue(ctx context.Context, d *models.DepositScreening) error
	// ListDue returns queued deposits whose next attempt is due, oldest
	// first.
	ListDue(ctx context.Context, now time.Time, limit int) ([]*models.DepositScreening, error)
	// RecordResult stores a screening and marks the deposit screened.
	RecordResult(ctx context.Context, id string, result *models.DepositScreening) error
	// RecordAttemptFailed counts a failed attempt; final marks it failed.
	RecordAttemptFailed(ctx context.Context, id, lastError string, next time.Time, final bool) error
	// MarkAlerted records that a person was alerted about the deposit.
	MarkAlerted(ctx context.Context, id string, at time.Time) error
	// Counterparty finds the counterparty an address belongs to; nil when
	// the address is not a registered lender address.
	Counterparty(ctx context.Context, address string) (*DepositCounterparty, error)
}

type depositScreeningRepository struct {
	db *gorm.DB
}

// NewDepositScreeningRepository builds the repository.
func NewDepositScreeningRepository(db *gorm.DB) (DepositScreeningRepository, error) {
	if db == nil {
		return nil, pkgErrors.ErrNilDB
	}
	return &depositScreeningRepository{db: db}, nil
}

func (r *depositScreeningRepository) Enqueue(ctx context.Context, d *models.DepositScreening) error {
	return r.db.WithContext(ctx).Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "event_id"}}, DoNothing: true}).Create(d).Error
}

func (r *depositScreeningRepository) ListDue(ctx context.Context, now time.Time, limit int) ([]*models.DepositScreening, error) {
	var out []*models.DepositScreening
	err := r.db.WithContext(ctx).
		Where("status = ? AND next_attempt_at <= ?", models.DepositScreeningQueued, now).
		Order("next_attempt_at ASC").Limit(limit).Find(&out).Error
	return out, err
}

func (r *depositScreeningRepository) RecordResult(ctx context.Context, id string, res *models.DepositScreening) error {
	now := time.Now()
	return r.db.WithContext(ctx).Model(&models.DepositScreening{}).
		Where("id = ? AND status = ?", id, models.DepositScreeningQueued).
		Updates(map[string]any{
			"status":                  models.DepositScreeningScreened,
			"attempts":                gorm.Expr("attempts + 1"),
			"last_error":              "",
			"provider":                res.Provider,
			"provider_ref":            res.ProviderRef,
			"verdict":                 res.Verdict,
			"sanctioned":              res.Sanctioned,
			"risk_score":              res.RiskScore,
			"raw_payload":             res.RawPayload,
			"counterparty_address_id": res.CounterpartyAddressID,
			"screened_at":             now,
		}).Error
}

func (r *depositScreeningRepository) RecordAttemptFailed(ctx context.Context, id, lastError string, next time.Time, final bool) error {
	fields := map[string]any{
		"attempts":        gorm.Expr("attempts + 1"),
		"last_error":      lastError,
		"next_attempt_at": next,
	}
	if final {
		fields["status"] = models.DepositScreeningFailed
	}
	return r.db.WithContext(ctx).Model(&models.DepositScreening{}).
		Where("id = ? AND status = ?", id, models.DepositScreeningQueued).Updates(fields).Error
}

func (r *depositScreeningRepository) MarkAlerted(ctx context.Context, id string, at time.Time) error {
	return r.db.WithContext(ctx).Model(&models.DepositScreening{}).Where("id = ?", id).Update("alerted_at", at).Error
}

func (r *depositScreeningRepository) Counterparty(ctx context.Context, address string) (*DepositCounterparty, error) {
	var row struct {
		AddressID         string
		CustomerReference string
	}
	err := r.db.WithContext(ctx).Table("counterparty_addresses AS a").
		Select("a.id AS address_id, c.elliptic_customer_reference AS customer_reference").
		Joins("JOIN counterparties c ON c.id = a.counterparty_id").
		Where("a.address = ?", address).Limit(1).Take(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil //nolint:nilnil // documented contract: nil means not a registered lender address
	}
	if err != nil {
		return nil, err
	}
	return &DepositCounterparty{AddressID: row.AddressID, CustomerReference: row.CustomerReference}, nil
}
