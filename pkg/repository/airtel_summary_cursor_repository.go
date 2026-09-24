package repository

import (
	"context"
	"log"
	"time"

	"gorm.io/gorm"

	pkgErrors "github.com/Shamba-Records-Limited/microvault/pkg/errors"
)

// AirtelSummaryCursorRepository tracks how far the Transactions Summary
// reconciliation sweep has walked. One row (id=1).
type AirtelSummaryCursorRepository interface {
	// Get returns the exclusive start of the next window to sweep.
	Get(ctx context.Context) (time.Time, error)
	// Advance moves the cursor forward to at.
	Advance(ctx context.Context, at time.Time) error
}

type airtelSummaryCursorRepository struct {
	db *gorm.DB
}

// NewAirtelSummaryCursorRepository builds the repository.
func NewAirtelSummaryCursorRepository(db *gorm.DB) (AirtelSummaryCursorRepository, error) {
	if db == nil {
		return nil, pkgErrors.ErrNilDB
	}
	return &airtelSummaryCursorRepository{db: db}, nil
}

func (r *airtelSummaryCursorRepository) Get(ctx context.Context) (time.Time, error) {
	var sweptTo time.Time
	result := r.db.WithContext(ctx).
		Raw("SELECT swept_to FROM airtel_summary_cursor WHERE id = 1").
		Scan(&sweptTo)
	if result.Error != nil {
		log.Printf("AirtelSummaryCursorRepository.Get: database error: %v", result.Error)
		return time.Time{}, result.Error
	}
	return sweptTo, nil
}

func (r *airtelSummaryCursorRepository) Advance(ctx context.Context, at time.Time) error {
	return r.db.WithContext(ctx).
		Exec("UPDATE airtel_summary_cursor SET swept_to = ? WHERE id = 1", at).
		Error
}
