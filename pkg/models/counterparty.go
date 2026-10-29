package models

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// CounterpartyKYBStatus is where a KYB entity stands in its own onboarding,
// independent of any address's screening status.
type CounterpartyKYBStatus string

const (
	CounterpartyKYBPending  CounterpartyKYBStatus = "pending"
	CounterpartyKYBApproved CounterpartyKYBStatus = "approved"
	CounterpartyKYBRejected CounterpartyKYBStatus = "rejected"
	CounterpartyKYBExpired  CounterpartyKYBStatus = "expired"
)

// Counterparty is an institutional vault depositor undergoing KYB — external
// and self-custodied, the opposite of the custodial borrower accounts in
// models.Account.
type Counterparty struct {
	ID string `json:"id" gorm:"type:uuid;primaryKey"`

	LegalName          string `json:"legal_name" gorm:"type:varchar(200);not null"`
	RegistrationNumber string `json:"registration_number,omitempty" gorm:"type:varchar(100)"`
	Jurisdiction       string `json:"jurisdiction,omitempty" gorm:"type:varchar(100)"`

	KYBStatus CounterpartyKYBStatus `json:"kyb_status" gorm:"column:kyb_status;type:varchar(20);not null;default:'pending';index"`

	// EllipticCustomerReference is sent as customer_reference on every
	// screening call for this counterparty's addresses — set once at
	// creation, never changed.
	EllipticCustomerReference string `json:"elliptic_customer_reference" gorm:"column:elliptic_customer_reference;type:varchar(100);uniqueIndex;not null"`

	KYBApprovedAt *time.Time `json:"kyb_approved_at,omitempty" gorm:"column:kyb_approved_at"`
	// KYBApprovedBy is the approving admin's Stellar public key, as text —
	// see counterparty_repository.go's doc comment on why this is text and
	// not a user-UUID FK.
	KYBApprovedBy *string `json:"kyb_approved_by,omitempty" gorm:"column:kyb_approved_by"`

	// KYB verification session with the provider (Didit): its id, the
	// hosted link sent to the lender, and the provider's latest status.
	KYBProvider      string     `json:"kyb_provider,omitempty" gorm:"column:kyb_provider;type:varchar(20);not null;default:''"`
	KYBSessionID     *string    `json:"kyb_session_id,omitempty" gorm:"column:kyb_session_id;type:varchar(80)"`
	KYBSessionURL    string     `json:"kyb_session_url,omitempty" gorm:"column:kyb_session_url;not null;default:''"`
	KYBSessionStatus string     `json:"kyb_session_status,omitempty" gorm:"column:kyb_session_status;type:varchar(20);not null;default:''"`
	KYBSessionAt     *time.Time `json:"kyb_session_at,omitempty" gorm:"column:kyb_session_at"`

	Addresses []CounterpartyAddress `json:"addresses,omitempty" gorm:"foreignKey:CounterpartyID"`

	CreatedAt time.Time `json:"created_at" gorm:"autoCreateTime;not null"`
	UpdatedAt time.Time `json:"updated_at" gorm:"autoUpdateTime;not null"`
}

func (Counterparty) TableName() string {
	return "counterparties"
}

// BeforeCreate assigns an ID only if the caller hasn't already set one —
// unlike every other model's BeforeCreate in this codebase, which always
// overwrites. Callers that need the ID before the insert (to derive
// EllipticCustomerReference from it, so the row is never briefly written
// with an empty value that would collide with the column's unique index)
// pre-generate it with uuid.NewV7() themselves.
func (c *Counterparty) BeforeCreate(g *gorm.DB) error {
	if c.ID != "" {
		return nil
	}
	id, err := uuid.NewV7()
	if err != nil {
		return err
	}
	c.ID = id.String()
	return nil
}
