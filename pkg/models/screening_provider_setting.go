package models

import (
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// Screening provider setting statuses.
const (
	ScreeningSettingProposed = "proposed"
	ScreeningSettingApproved = "approved"
	ScreeningSettingRejected = "rejected"
)

// ScreeningProviderSetting is one proposed change of the screening
// providers, decided by a second operator.
type ScreeningProviderSetting struct {
	ID           string     `json:"id" gorm:"type:uuid;primaryKey"`
	Providers    string     `json:"providers" gorm:"type:varchar(120);not null"`
	Reason       string     `json:"reason" gorm:"not null"`
	Status       string     `json:"status" gorm:"type:varchar(20);not null;default:proposed"`
	ProposedBy   string     `json:"proposed_by" gorm:"type:varchar(56);not null"`
	ProposedAt   time.Time  `json:"proposed_at" gorm:"not null"`
	DecidedBy    *string    `json:"decided_by,omitempty" gorm:"type:varchar(56)"`
	DecidedAt    *time.Time `json:"decided_at,omitempty"`
	DecisionNote string     `json:"decision_note,omitempty" gorm:"not null;default:''"`
}

// TableName pins the table name.
func (ScreeningProviderSetting) TableName() string { return "screening_provider_settings" }

// ProviderList splits Providers into names.
func (s *ScreeningProviderSetting) ProviderList() []string {
	return strings.Split(s.Providers, ",")
}

// BeforeCreate assigns an ID and proposal time when unset.
func (s *ScreeningProviderSetting) BeforeCreate(*gorm.DB) error {
	if s.ID == "" {
		id, err := uuid.NewV7()
		if err != nil {
			return err
		}
		s.ID = id.String()
	}
	if s.ProposedAt.IsZero() {
		s.ProposedAt = time.Now()
	}
	return nil
}
