package models

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

// Deposit screening statuses.
const (
	DepositScreeningQueued   = "queued"
	DepositScreeningScreened = "screened"
	DepositScreeningFailed   = "failed"
)

// DepositScreening is the screening of one vault deposit transaction, queued
// by the vault watcher and resolved by the deposit screener.
type DepositScreening struct {
	ID                    string         `json:"id" gorm:"type:uuid;primaryKey"`
	EventID               string         `json:"event_id" gorm:"type:varchar(64);uniqueIndex;not null"`
	TxHash                string         `json:"tx_hash" gorm:"type:varchar(64);not null"`
	Ledger                int64          `json:"ledger" gorm:"not null"`
	Depositor             string         `json:"depositor" gorm:"type:varchar(56);not null"`
	Receiver              string         `json:"receiver" gorm:"type:varchar(56);not null;default:''"`
	CounterpartyAddressID *string        `json:"counterparty_address_id,omitempty" gorm:"type:uuid"`
	Status                string         `json:"status" gorm:"type:varchar(20);not null;default:queued"`
	Attempts              int            `json:"attempts" gorm:"not null;default:0"`
	NextAttemptAt         time.Time      `json:"next_attempt_at" gorm:"not null"`
	LastError             string         `json:"last_error,omitempty" gorm:"not null;default:''"`
	Provider              string         `json:"provider,omitempty" gorm:"type:varchar(40);not null;default:''"`
	ProviderRef           string         `json:"provider_ref,omitempty" gorm:"type:varchar(120);not null;default:''"`
	Verdict               string         `json:"verdict,omitempty" gorm:"type:varchar(20);not null;default:''"`
	Sanctioned            bool           `json:"sanctioned" gorm:"not null;default:false"`
	RiskScore             *float64       `json:"risk_score,omitempty" gorm:"type:numeric"`
	RawPayload            datatypes.JSON `json:"raw_payload,omitempty" gorm:"type:jsonb"`
	AlertedAt             *time.Time     `json:"alerted_at,omitempty"`
	ScreenedAt            *time.Time     `json:"screened_at,omitempty"`
	CreatedAt             time.Time      `json:"created_at" gorm:"autoCreateTime;not null"`
}

// TableName pins the table name.
func (DepositScreening) TableName() string { return "deposit_screenings" }

// BeforeCreate assigns an ID when none is set.
func (d *DepositScreening) BeforeCreate(*gorm.DB) error {
	if d.ID == "" {
		id, err := uuid.NewV7()
		if err != nil {
			return err
		}
		d.ID = id.String()
	}
	if d.NextAttemptAt.IsZero() {
		d.NextAttemptAt = time.Now()
	}
	return nil
}
