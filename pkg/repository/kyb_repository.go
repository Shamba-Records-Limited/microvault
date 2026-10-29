package repository

import (
	"context"
	"errors"
	"time"

	"gorm.io/gorm"

	pkgErrors "github.com/Shamba-Records-Limited/microvault/pkg/errors"
	"github.com/Shamba-Records-Limited/microvault/pkg/models"
)

// ErrKYBSessionMismatch means a provider event named a session that is not
// the counterparty's current one.
var ErrKYBSessionMismatch = errors.New("kyb event is for a session that is not the counterparty's current one")

// KYBRepository stores counterparties' provider KYB sessions and events.
type KYBRepository interface {
	// SetSession records a new verification session on the counterparty.
	SetSession(ctx context.Context, counterpartyID, provider, sessionID, url, status string, at time.Time) error
	// RecordEvent appends a provider status change and, in the same
	// transaction, updates the counterparty's current session status.
	RecordEvent(ctx context.Context, e *models.CounterpartyKYBEvent) error
	// ListEvents returns a counterparty's KYB events, newest first.
	ListEvents(ctx context.Context, counterpartyID string, limit int) ([]*models.CounterpartyKYBEvent, error)
}

type kybRepository struct {
	db *gorm.DB
}

// NewKYBRepository builds the repository.
func NewKYBRepository(db *gorm.DB) (KYBRepository, error) {
	if db == nil {
		return nil, pkgErrors.ErrNilDB
	}
	return &kybRepository{db: db}, nil
}

func (r *kybRepository) SetSession(ctx context.Context, counterpartyID, provider, sessionID, url, status string, at time.Time) error {
	res := r.db.WithContext(ctx).Model(&models.Counterparty{}).Where("id = ?", counterpartyID).Updates(map[string]any{
		"kyb_provider":       provider,
		"kyb_session_id":     sessionID,
		"kyb_session_url":    url,
		"kyb_session_status": status,
		"kyb_session_at":     at,
		"updated_at":         at,
	})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrCounterpartyNotFound
	}
	return nil
}

func (r *kybRepository) RecordEvent(ctx context.Context, e *models.CounterpartyKYBEvent) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var cp models.Counterparty
		err := tx.Select("id", "kyb_session_id").Where("id = ?", e.CounterpartyID).First(&cp).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrCounterpartyNotFound
		}
		if err != nil {
			return err
		}
		if cp.KYBSessionID == nil || *cp.KYBSessionID != e.SessionID {
			return ErrKYBSessionMismatch
		}
		if err := tx.Create(e).Error; err != nil {
			return err
		}
		if e.Status == "" {
			return nil
		}
		return tx.Model(&models.Counterparty{}).Where("id = ?", e.CounterpartyID).Updates(map[string]any{
			"kyb_session_status": e.Status,
			"updated_at":         time.Now(),
		}).Error
	})
}

func (r *kybRepository) ListEvents(ctx context.Context, counterpartyID string, limit int) ([]*models.CounterpartyKYBEvent, error) {
	var out []*models.CounterpartyKYBEvent
	err := r.db.WithContext(ctx).Where("counterparty_id = ?", counterpartyID).
		Order("received_at DESC").Limit(limit).Find(&out).Error
	return out, err
}
