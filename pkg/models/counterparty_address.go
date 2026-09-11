package models

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// CounterpartyAddressStatus is the address's current compliance state — the
// §10 verdict states, plus "expired" (a staleness state no single screening
// produces; it's derived from expires_at). Deliberately its own type rather
// than reusing compliance.Verdict: this column is what we currently believe
// about the address, which "expired" can describe even when the last
// screening's own verdict was "approved".
type CounterpartyAddressStatus string

const (
	AddressStatusPending      CounterpartyAddressStatus = "pending"
	AddressStatusApproved     CounterpartyAddressStatus = "approved"
	AddressStatusRejected     CounterpartyAddressStatus = "rejected"
	AddressStatusReview       CounterpartyAddressStatus = "review"
	AddressStatusUnscreenable CounterpartyAddressStatus = "unscreenable"
	AddressStatusExpired      CounterpartyAddressStatus = "expired"
)

// CounterpartyAddressOnchainState tracks whether the vault contract's
// on-chain allowlist actually reflects this row — a database write and a
// contract write are two systems, and one can fail after the other
// succeeds. See the source design doc §12.
type CounterpartyAddressOnchainState string

const (
	OnchainStateAbsent   CounterpartyAddressOnchainState = "absent"
	OnchainStatePending  CounterpartyAddressOnchainState = "pending"
	OnchainStateApproved CounterpartyAddressOnchainState = "approved"
	OnchainStateRevoked  CounterpartyAddressOnchainState = "revoked"
)

// CounterpartyAddress is a wallet address a counterparty has submitted for
// screening — the allowlist's source of truth, and the row an approval or
// revocation is actually attached to.
type CounterpartyAddress struct {
	ID             string `json:"id" gorm:"type:uuid;primaryKey"`
	CounterpartyID string `json:"counterparty_id" gorm:"column:counterparty_id;type:uuid;not null;index"`
	// Address is a Stellar G... account, 56 characters, matching
	// accounts.public_key.
	Address string `json:"address" gorm:"type:varchar(56);uniqueIndex;not null"`

	Status CounterpartyAddressStatus `json:"status" gorm:"type:varchar(20);not null;default:'pending';index"`

	LastScreeningID *string    `json:"last_screening_id,omitempty" gorm:"column:last_screening_id;type:uuid"`
	ScreenedAt      *time.Time `json:"screened_at,omitempty" gorm:"column:screened_at"`
	ExpiresAt       *time.Time `json:"expires_at,omitempty" gorm:"column:expires_at;index"`

	OnchainState CounterpartyAddressOnchainState `json:"onchain_state" gorm:"column:onchain_state;type:varchar(20);not null;default:'absent';index"`

	ApprovedBy     *string    `json:"approved_by,omitempty" gorm:"column:approved_by"`
	ApprovedAt     *time.Time `json:"approved_at,omitempty" gorm:"column:approved_at"`
	RevokedBy      *string    `json:"revoked_by,omitempty" gorm:"column:revoked_by"`
	RevokedAt      *time.Time `json:"revoked_at,omitempty" gorm:"column:revoked_at"`
	OverrideReason *string    `json:"override_reason,omitempty" gorm:"column:override_reason"`

	Counterparty *Counterparty `json:"counterparty,omitempty" gorm:"foreignKey:CounterpartyID"`
	// LatestScreening is a belongs-to on last_screening_id — an ordinary FK,
	// so Preload("LatestScreening") works the same as any other association.
	LatestScreening *AddressScreening `json:"latest_screening,omitempty" gorm:"foreignKey:LastScreeningID;references:ID"`

	CreatedAt time.Time `json:"created_at" gorm:"autoCreateTime;not null"`
	UpdatedAt time.Time `json:"updated_at" gorm:"autoUpdateTime;not null"`
}

func (CounterpartyAddress) TableName() string {
	return "counterparty_addresses"
}

func (a *CounterpartyAddress) BeforeCreate(g *gorm.DB) error {
	id, err := uuid.NewV7()
	if err != nil {
		return err
	}
	a.ID = id.String()
	return nil
}
