package models

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

// AddressScreening is one screening performed on a CounterpartyAddress.
// Append-only — there is deliberately no Update path anywhere in this
// codebase for this table. A compliance record that changes in place
// cannot answer "what did we know on the day we approved this," which is
// the only question that matters after an incident. See the source design
// doc §12.
type AddressScreening struct {
	ID        string `json:"id" gorm:"type:uuid;primaryKey"`
	AddressID string `json:"address_id" gorm:"column:address_id;type:uuid;not null;index"`

	// EllipticAnalysisID/EllipticScreeningID let this screening be fetched
	// from Elliptic directly later, independent of RawPayload.
	EllipticAnalysisID  string `json:"elliptic_analysis_id,omitempty" gorm:"column:elliptic_analysis_id"`
	EllipticScreeningID string `json:"elliptic_screening_id,omitempty" gorm:"column:elliptic_screening_id"`

	ScreeningSource string `json:"screening_source" gorm:"column:screening_source;type:varchar(30);not null"`

	// RiskScore is nullable and never defaulted to zero — see
	// compliance.Screening.RiskScore's doc comment on why a null score is
	// not the same fact as a clean score.
	RiskScore  *float64 `json:"risk_score,omitempty" gorm:"column:risk_score;type:numeric"`
	Sanctioned bool     `json:"sanctioned" gorm:"not null;default:false"`
	Verdict    string   `json:"verdict" gorm:"type:varchar(20);not null"`

	// RawPayload is Elliptic's whole response. Kept because a compliance
	// record has to be reproducible years later, and a struct designed
	// today will not have a field for whatever Elliptic adds later.
	RawPayload datatypes.JSON `json:"raw_payload" gorm:"column:raw_payload;type:jsonb;not null"`

	CreatedAt time.Time `json:"created_at" gorm:"autoCreateTime;not null"`
}

func (AddressScreening) TableName() string {
	return "address_screenings"
}

func (s *AddressScreening) BeforeCreate(g *gorm.DB) error {
	id, err := uuid.NewV7()
	if err != nil {
		return err
	}
	s.ID = id.String()
	return nil
}
