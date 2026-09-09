package repository

import (
	"context"
	"time"

	"gorm.io/gorm"

	pkgErrors "github.com/Shamba-Records-Limited/microvault/pkg/errors"
)

// MpesaPullCursorRepository tracks how far the Pull reconciliation sweep has
// walked. One row (id=1); Get and Advance both operate on it.
type MpesaPullCursorRepository interface {
	// Get returns the cursor's current position — the exclusive start of the
	// next window to sweep.
	Get(ctx context.Context) (time.Time, error)
	// Advance moves the cursor forward to at.
	Advance(ctx context.Context, at time.Time) error
}

type mpesaPullCursorRepository struct {
	db *gorm.DB
}

// NewMpesaPullCursorRepository builds the repository.
func NewMpesaPullCursorRepository(db *gorm.DB) (MpesaPullCursorRepository, error) {
	if db == nil {
		return nil, pkgErrors.ErrNilDB
	}
	return &mpesaPullCursorRepository{db: db}, nil
}

func (r *mpesaPullCursorRepository) Get(ctx context.Context) (time.Time, error) {
	var sweptTo time.Time
	result := r.db.WithContext(ctx).
		Raw("SELECT swept_to FROM mpesa_pull_cursor WHERE id = 1").
		Scan(&sweptTo)
	if result.Error != nil {
		return time.Time{}, result.Error
	}
	return sweptTo, nil
}

func (r *mpesaPullCursorRepository) Advance(ctx context.Context, at time.Time) error {
	return r.db.WithContext(ctx).
		Exec("UPDATE mpesa_pull_cursor SET swept_to = ? WHERE id = 1", at).
		Error
}
