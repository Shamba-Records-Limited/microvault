package models

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// MpesaNumberValidation caches the verdict of a Mobile Number Validation
// call, keyed on a hash of the (msisdn, idType, idNumber) tuple so the cache
// itself never stores the PII it exists to avoid re-checking.
type MpesaNumberValidation struct {
	ID string `json:"id" gorm:"type:uuid;primaryKey"`

	// IdentityHash is sha256(msisdn|idType|idNumber), hex-encoded.
	IdentityHash string `json:"identity_hash" gorm:"column:identity_hash;type:varchar(64);uniqueIndex;not null"`

	Matched      bool   `json:"matched" gorm:"not null"`
	ResponseCode string `json:"response_code" gorm:"column:response_code;type:varchar(10);not null"`

	CheckedAt time.Time `json:"checked_at" gorm:"column:checked_at;not null"`
}

// TableName specifies the table name for MpesaNumberValidation.
func (MpesaNumberValidation) TableName() string {
	return "mpesa_number_validations"
}

// BeforeCreate sets the ID before creating a new MpesaNumberValidation.
func (v *MpesaNumberValidation) BeforeCreate(g *gorm.DB) error {
	id, err := uuid.NewV7()
	if err != nil {
		return err
	}
	v.ID = id.String()
	return nil
}
