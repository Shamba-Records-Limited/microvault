package repository

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"gorm.io/gorm"

	pkgErrors "github.com/Shamba-Records-Limited/microvault/pkg/errors"
	"github.com/Shamba-Records-Limited/microvault/pkg/models"
)

var (
	// ErrScreeningProposalOpen means another provider change is waiting for
	// a decision.
	ErrScreeningProposalOpen = errors.New("another screening provider change is waiting for approval")
	// ErrScreeningProposalNotOpen means the change was already decided or
	// does not exist.
	ErrScreeningProposalNotOpen = errors.New("this screening provider change is not waiting for a decision")
)

// ScreeningSettingRepository stores screening provider changes.
type ScreeningSettingRepository interface {
	// Propose stores a new change; only one may wait at a time.
	Propose(ctx context.Context, s *models.ScreeningProviderSetting) error
	// Get returns one change.
	Get(ctx context.Context, id string) (*models.ScreeningProviderSetting, error)
	// Decide approves or rejects a waiting change.
	Decide(ctx context.Context, id, status, actor, note string, at time.Time) error
	// Current returns the approved change in force, or nil when none has
	// been approved.
	Current(ctx context.Context) (*models.ScreeningProviderSetting, error)
	// List returns changes, newest first.
	List(ctx context.Context, limit int) ([]*models.ScreeningProviderSetting, error)
}

type screeningSettingRepository struct {
	db *gorm.DB
}

// NewScreeningSettingRepository builds the repository.
func NewScreeningSettingRepository(db *gorm.DB) (ScreeningSettingRepository, error) {
	if db == nil {
		return nil, pkgErrors.ErrNilDB
	}
	return &screeningSettingRepository{db: db}, nil
}

func (r *screeningSettingRepository) Propose(ctx context.Context, s *models.ScreeningProviderSetting) error {
	err := r.db.WithContext(ctx).Create(s).Error
	var pgErr *pgconn.PgError
	if errors.Is(err, gorm.ErrDuplicatedKey) || (errors.As(err, &pgErr) && pgErr.Code == "23505") {
		return ErrScreeningProposalOpen
	}
	return err
}

func (r *screeningSettingRepository) Get(ctx context.Context, id string) (*models.ScreeningProviderSetting, error) {
	var s models.ScreeningProviderSetting
	err := r.db.WithContext(ctx).Where("id = ?", id).Take(&s).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrScreeningProposalNotOpen
	}
	return &s, err
}

func (r *screeningSettingRepository) Decide(ctx context.Context, id, status, actor, note string, at time.Time) error {
	res := r.db.WithContext(ctx).Model(&models.ScreeningProviderSetting{}).
		Where("id = ? AND status = ?", id, models.ScreeningSettingProposed).
		Updates(map[string]any{"status": status, "decided_by": actor, "decided_at": at, "decision_note": note})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrScreeningProposalNotOpen
	}
	return nil
}

func (r *screeningSettingRepository) Current(ctx context.Context) (*models.ScreeningProviderSetting, error) {
	var s models.ScreeningProviderSetting
	err := r.db.WithContext(ctx).Where("status = ?", models.ScreeningSettingApproved).
		Order("decided_at DESC").Take(&s).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil //nolint:nilnil // documented contract: nil means none approved yet
	}
	if err != nil {
		return nil, err
	}
	return &s, nil
}

func (r *screeningSettingRepository) List(ctx context.Context, limit int) ([]*models.ScreeningProviderSetting, error) {
	var out []*models.ScreeningProviderSetting
	err := r.db.WithContext(ctx).Order("proposed_at DESC").Limit(limit).Find(&out).Error
	return out, err
}
