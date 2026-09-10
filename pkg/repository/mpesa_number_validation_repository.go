package repository

import (
	"context"
	"errors"

	"gorm.io/gorm"

	pkgErrors "github.com/Shamba-Records-Limited/microvault/pkg/errors"
	"github.com/Shamba-Records-Limited/microvault/pkg/models"
)

// ErrMpesaValidationNotFound means no cached verdict exists for that hash.
var ErrMpesaValidationNotFound = errors.New("mpesa number validation not found")

// MpesaNumberValidationRepository caches Mobile Number Validation verdicts so
// a payer's identity is checked at the paid rate at most once per cache
// entry rather than once per payment.
type MpesaNumberValidationRepository interface {
	// Get returns the cached verdict for a hash, or ErrMpesaValidationNotFound.
	Get(ctx context.Context, identityHash string) (*models.MpesaNumberValidation, error)
	// Upsert records a fresh verdict, replacing whatever was cached for the
	// same identity hash.
	Upsert(ctx context.Context, v *models.MpesaNumberValidation) error
}

type mpesaNumberValidationRepository struct {
	db *gorm.DB
}

// NewMpesaNumberValidationRepository builds the repository.
func NewMpesaNumberValidationRepository(db *gorm.DB) (MpesaNumberValidationRepository, error) {
	if db == nil {
		return nil, pkgErrors.ErrNilDB
	}
	return &mpesaNumberValidationRepository{db: db}, nil
}

func (r *mpesaNumberValidationRepository) Get(ctx context.Context, identityHash string) (*models.MpesaNumberValidation, error) {
	var v models.MpesaNumberValidation
	result := r.db.WithContext(ctx).Where("identity_hash = ?", identityHash).First(&v)
	if errors.Is(result.Error, gorm.ErrRecordNotFound) {
		return nil, ErrMpesaValidationNotFound
	}
	if result.Error != nil {
		return nil, result.Error
	}
	return &v, nil
}

// Upsert tries an update first, since a repeat check on an existing hash is
// the common case; it falls back to inserting only when nothing was there to
// update. The unique index on identity_hash is what makes a lost race between
// two concurrent checks harmless rather than a duplicate row.
func (r *mpesaNumberValidationRepository) Upsert(ctx context.Context, v *models.MpesaNumberValidation) error {
	result := r.db.WithContext(ctx).
		Model(&models.MpesaNumberValidation{}).
		Where("identity_hash = ?", v.IdentityHash).
		Updates(map[string]any{
			"matched":       v.Matched,
			"response_code": v.ResponseCode,
			"checked_at":    v.CheckedAt,
		})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected > 0 {
		return nil
	}
	if err := r.db.WithContext(ctx).Create(v).Error; err != nil {
		// A concurrent Upsert may have inserted between the failed update and
		// this create; treat the resulting unique-constraint violation as the
		// success it functionally is rather than surfacing it.
		if errors.Is(err, gorm.ErrDuplicatedKey) {
			return nil
		}
		return err
	}
	return nil
}
