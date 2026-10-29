package models

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

// CounterpartyKYBEvent is one status change a KYB provider reported for a
// counterparty's verification session. Append-only.
type CounterpartyKYBEvent struct {
	ID             string         `json:"id" gorm:"type:uuid;primaryKey"`
	CounterpartyID string         `json:"counterparty_id" gorm:"type:uuid;not null;index"`
	Provider       string         `json:"provider" gorm:"type:varchar(20);not null"`
	SessionID      string         `json:"session_id" gorm:"type:varchar(80);not null"`
	Event          string         `json:"event" gorm:"type:varchar(40);not null"`
	Status         string         `json:"status" gorm:"type:varchar(20);not null"`
	PreviousStatus string         `json:"previous_status" gorm:"type:varchar(20);not null;default:''"`
	Payload        datatypes.JSON `json:"payload" gorm:"type:jsonb;not null"`
	ReceivedAt     time.Time      `json:"received_at" gorm:"not null"`
}

// TableName pins the table name.
func (CounterpartyKYBEvent) TableName() string { return "counterparty_kyb_events" }

// BeforeCreate assigns an ID and receipt time when unset.
func (e *CounterpartyKYBEvent) BeforeCreate(*gorm.DB) error {
	if e.ID == "" {
		id, err := uuid.NewV7()
		if err != nil {
			return err
		}
		e.ID = id.String()
	}
	if e.ReceivedAt.IsZero() {
		e.ReceivedAt = time.Now()
	}
	return nil
}
