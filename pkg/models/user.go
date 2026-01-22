package models

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// User represents a system user
type User struct {
	ID                string     `json:"id" gorm:"type:uuid;primaryKey"`
	MobileNumber      string     `json:"mobile_number" gorm:"type:varchar(20);uniqueIndex;not null"`
	MobileCountryCode string     `json:"mobile_country_code" gorm:"type:varchar(5);not null;default:'254'"`
	FullName          *string    `json:"full_name,omitempty" gorm:"type:varchar(255)"`
	NationalID        *string    `json:"national_id,omitempty" gorm:"type:varchar(50);uniqueIndex"`
	KYCStatus         string     `json:"kyc_status" gorm:"type:varchar(20);not null;default:'pending'"`
	KYCVerifiedAt     *time.Time `json:"kyc_verified_at,omitempty" gorm:"type:timestamp"`
	PreferredLanguage string     `json:"preferred_language" gorm:"type:varchar(10);not null;default:'en'"`
	Status            string     `json:"status" gorm:"type:varchar(20);not null;default:'active'"`
	Role              string     `json:"role" gorm:"type:varchar(20);not null;default:'user'"`
	CreatedAt         time.Time  `json:"created_at" gorm:"autoCreateTime;not null"`
	UpdatedAt         time.Time  `json:"updated_at" gorm:"autoUpdateTime;not null"`
	DeletedAt         *time.Time `json:"deleted_at,omitempty" gorm:"index"`
}

// TableName specifies the table name for User model
func (User) TableName() string {
	return "users"
}

// BeforeCreate sets the ID before creating a new user
func (user *User) BeforeCreate(tx *gorm.DB) error {
	id, err := uuid.NewV7()
	if err != nil {
		return err
	}
	user.ID = id.String()
	return nil
}

// Account represents a Stellar child account
type Account struct {
	ID           string     `json:"id" gorm:"type:uuid;primaryKey"`
	UserID       string     `json:"user_id" gorm:"type:uuid;not null;index"`
	PublicKey    string     `json:"public_key" gorm:"type:varchar(56);uniqueIndex;not null"`
	AccountIndex int        `json:"account_index" gorm:"not null"`
	Status       string     `json:"status" gorm:"type:varchar(20);not null;default:'active'"`
	CreatedAt    time.Time  `json:"created_at" gorm:"autoCreateTime;not null"`
	UpdatedAt    time.Time  `json:"updated_at" gorm:"autoUpdateTime;not null"`
	DeletedAt    *time.Time `json:"deleted_at,omitempty" gorm:"index"`

	User User `gorm:"foreignKey:UserID"`
}

// TableName specifies the table name for Account model
func (Account) TableName() string {
	return "accounts"
}

// BeforeCreate sets the ID before creating a new account
func (account *Account) BeforeCreate(tx *gorm.DB) error {
	id, err := uuid.NewV7()
	if err != nil {
		return err
	}
	account.ID = id.String()
	return nil
}

// Constants for status enums
const (
	// User KYC Status
	KYCStatusPending  = "pending"
	KYCStatusVerified = "verified"
	KYCStatusRejected = "rejected"
	KYCStatusExpired  = "expired"

	// User/Account Status
	StatusActive    = "active"
	StatusSuspended = "suspended"
	StatusBlocked   = "blocked"
	StatusFrozen    = "frozen"
	StatusClosed    = "closed"
)
