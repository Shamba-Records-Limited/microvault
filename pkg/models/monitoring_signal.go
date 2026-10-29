package models

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

// Monitoring signal statuses.
const (
	MonitoringSignalQueued     = "queued"
	MonitoringSignalRescreened = "rescreened"
	MonitoringSignalFailed     = "failed"
	MonitoringSignalUnmatched  = "unmatched"
)

// MonitoringSignal is a provider monitoring alert about one lender address,
// queued for a rescreen of that address. An alert that names no lender
// address is kept as unmatched.
type MonitoringSignal struct {
	ID                    string         `json:"id" gorm:"type:uuid;primaryKey"`
	Provider              string         `json:"provider" gorm:"type:varchar(20);not null"`
	Event                 string         `json:"event" gorm:"type:varchar(80);not null;default:''"`
	EventKey              string         `json:"event_key" gorm:"type:char(64);not null"`
	CounterpartyID        *string        `json:"counterparty_id,omitempty" gorm:"type:uuid"`
	CounterpartyAddressID *string        `json:"counterparty_address_id,omitempty" gorm:"type:uuid"`
	Status                string         `json:"status" gorm:"type:varchar(20);not null;default:queued"`
	Attempts              int            `json:"attempts" gorm:"not null;default:0"`
	NextAttemptAt         time.Time      `json:"next_attempt_at" gorm:"not null"`
	LastError             string         `json:"last_error,omitempty" gorm:"not null;default:''"`
	Verdict               string         `json:"verdict,omitempty" gorm:"type:varchar(20);not null;default:''"`
	ScreeningProvider     string         `json:"screening_provider,omitempty" gorm:"type:varchar(40);not null;default:''"`
	Payload               datatypes.JSON `json:"payload" gorm:"type:jsonb;not null"`
	AlertedAt             *time.Time     `json:"alerted_at,omitempty"`
	RescreenedAt          *time.Time     `json:"rescreened_at,omitempty"`
	ReceivedAt            time.Time      `json:"received_at" gorm:"not null"`
}

// TableName pins the table name.
func (MonitoringSignal) TableName() string { return "monitoring_signals" }

// BeforeCreate assigns an ID and timestamps when unset.
func (m *MonitoringSignal) BeforeCreate(*gorm.DB) error {
	if m.ID == "" {
		id, err := uuid.NewV7()
		if err != nil {
			return err
		}
		m.ID = id.String()
	}
	now := time.Now()
	if m.NextAttemptAt.IsZero() {
		m.NextAttemptAt = now
	}
	if m.ReceivedAt.IsZero() {
		m.ReceivedAt = now
	}
	return nil
}
