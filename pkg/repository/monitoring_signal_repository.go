package repository

import (
	"context"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	pkgErrors "github.com/Shamba-Records-Limited/microvault/pkg/errors"
	"github.com/Shamba-Records-Limited/microvault/pkg/models"
)

// MonitoredAddress is a lender address a monitoring alert can rescreen.
type MonitoredAddress struct {
	AddressID      string
	CounterpartyID string
	Address        string
}

// MonitoringSignalRepository queues provider monitoring alerts for rescreening.
type MonitoringSignalRepository interface {
	// MatchAddresses returns the lender addresses among addresses that have
	// been screened before; pending ones wait for KYB approval instead.
	MatchAddresses(ctx context.Context, addresses []string) ([]MonitoredAddress, error)
	// CounterpartyAddresses returns a counterparty's screened addresses.
	CounterpartyAddresses(ctx context.Context, counterpartyID string) ([]MonitoredAddress, error)
	// Enqueue stores signals; a repeat delivery of the same event for the
	// same address is ignored. It reports how many rows were new.
	Enqueue(ctx context.Context, signals []*models.MonitoringSignal) (int64, error)
	// ListDue returns queued signals whose next attempt is due, oldest first.
	ListDue(ctx context.Context, now time.Time, limit int) ([]*models.MonitoringSignal, error)
	// RecordResult marks a signal rescreened with the rescreen's outcome.
	RecordResult(ctx context.Context, id, verdict, provider string, at time.Time) error
	// RecordAttemptFailed counts a failed attempt; final marks it failed.
	RecordAttemptFailed(ctx context.Context, id, lastError string, next time.Time, final bool) error
	// MarkAlerted records that a person was alerted about the signal.
	MarkAlerted(ctx context.Context, id string, at time.Time) error
}

type monitoringSignalRepository struct {
	db *gorm.DB
}

// NewMonitoringSignalRepository builds the repository.
func NewMonitoringSignalRepository(db *gorm.DB) (MonitoringSignalRepository, error) {
	if db == nil {
		return nil, pkgErrors.ErrNilDB
	}
	return &monitoringSignalRepository{db: db}, nil
}

func (r *monitoringSignalRepository) monitored(ctx context.Context, where string, arg any) ([]MonitoredAddress, error) {
	var out []MonitoredAddress
	err := r.db.WithContext(ctx).Model(&models.CounterpartyAddress{}).
		Select("id AS address_id, counterparty_id, address").
		Where(where, arg).
		Where("status <> ?", models.AddressStatusPending).
		Order("created_at ASC").
		Scan(&out).Error
	return out, err
}

func (r *monitoringSignalRepository) MatchAddresses(ctx context.Context, addresses []string) ([]MonitoredAddress, error) {
	if len(addresses) == 0 {
		return nil, nil
	}
	return r.monitored(ctx, "address IN ?", addresses)
}

func (r *monitoringSignalRepository) CounterpartyAddresses(ctx context.Context, counterpartyID string) ([]MonitoredAddress, error) {
	return r.monitored(ctx, "counterparty_id = ?", counterpartyID)
}

func (r *monitoringSignalRepository) Enqueue(ctx context.Context, signals []*models.MonitoringSignal) (int64, error) {
	if len(signals) == 0 {
		return 0, nil
	}
	res := r.db.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(signals)
	return res.RowsAffected, res.Error
}

func (r *monitoringSignalRepository) ListDue(ctx context.Context, now time.Time, limit int) ([]*models.MonitoringSignal, error) {
	var out []*models.MonitoringSignal
	err := r.db.WithContext(ctx).
		Where("status = ? AND next_attempt_at <= ?", models.MonitoringSignalQueued, now).
		Order("next_attempt_at ASC").Limit(limit).Find(&out).Error
	return out, err
}

func (r *monitoringSignalRepository) RecordResult(ctx context.Context, id, verdict, provider string, at time.Time) error {
	return r.db.WithContext(ctx).Model(&models.MonitoringSignal{}).
		Where("id = ? AND status = ?", id, models.MonitoringSignalQueued).
		Updates(map[string]any{
			"status":             models.MonitoringSignalRescreened,
			"attempts":           gorm.Expr("attempts + 1"),
			"last_error":         "",
			"verdict":            verdict,
			"screening_provider": provider,
			"rescreened_at":      at,
		}).Error
}

func (r *monitoringSignalRepository) RecordAttemptFailed(ctx context.Context, id, lastError string, next time.Time, final bool) error {
	fields := map[string]any{
		"attempts":        gorm.Expr("attempts + 1"),
		"last_error":      lastError,
		"next_attempt_at": next,
	}
	if final {
		fields["status"] = models.MonitoringSignalFailed
	}
	return r.db.WithContext(ctx).Model(&models.MonitoringSignal{}).
		Where("id = ? AND status = ?", id, models.MonitoringSignalQueued).Updates(fields).Error
}

func (r *monitoringSignalRepository) MarkAlerted(ctx context.Context, id string, at time.Time) error {
	return r.db.WithContext(ctx).Model(&models.MonitoringSignal{}).Where("id = ?", id).Update("alerted_at", at).Error
}
