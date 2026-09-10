package repository

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	pkgErrors "github.com/Shamba-Records-Limited/microvault/pkg/errors"
)

// ErrMpesaBalanceQueryNotFound means no pending query matches the
// OriginatorConversationID a balance result arrived with.
var ErrMpesaBalanceQueryNotFound = errors.New("mpesa balance query not found")

// mpesaBalanceQueryRow and mpesaAccountBalanceRow map the two tables. Kept
// unexported: this repository's job is to hide the correlation mechanics
// behind RecordQuery/ResolveQuery, not to expose two more model types.
type mpesaBalanceQueryRow struct {
	OriginatorConversationID string `gorm:"column:originator_conversation_id;primaryKey"`
	Shortcode                uint   `gorm:"column:shortcode"`
	RequestedAt              time.Time
}

func (mpesaBalanceQueryRow) TableName() string { return "mpesa_balance_queries" }

type mpesaAccountBalanceRow struct {
	ID           string `gorm:"type:uuid;primaryKey"`
	Shortcode    uint
	AccountName  string `gorm:"column:account_name"`
	Currency     string
	AvailableKES int64     `gorm:"column:available_kes"`
	ObservedAt   time.Time `gorm:"column:observed_at"`
}

func (mpesaAccountBalanceRow) TableName() string { return "mpesa_account_balances" }

// MpesaBalanceRepository correlates Account Balance requests with their
// asynchronous results and persists the parsed figures.
type MpesaBalanceRepository interface {
	// RecordQuery notes that a balance query for shortcode was sent, keyed on
	// the ack's OriginatorConversationID.
	RecordQuery(ctx context.Context, originatorConversationID string, shortcode uint) error
	// ResolveQuery returns the shortcode a landing result's
	// OriginatorConversationID was queried for, or ErrMpesaBalanceQueryNotFound.
	ResolveQuery(ctx context.Context, originatorConversationID string) (uint, error)
	// RecordBalance persists one parsed account balance snapshot.
	RecordBalance(ctx context.Context, shortcode uint, accountName, currency string, availableKES int64, observedAt time.Time) error
}

type mpesaBalanceRepository struct {
	db *gorm.DB
}

// NewMpesaBalanceRepository builds the repository.
func NewMpesaBalanceRepository(db *gorm.DB) (MpesaBalanceRepository, error) {
	if db == nil {
		return nil, pkgErrors.ErrNilDB
	}
	return &mpesaBalanceRepository{db: db}, nil
}

func (r *mpesaBalanceRepository) RecordQuery(ctx context.Context, originatorConversationID string, shortcode uint) error {
	return r.db.WithContext(ctx).Create(&mpesaBalanceQueryRow{
		OriginatorConversationID: originatorConversationID,
		Shortcode:                shortcode,
		RequestedAt:              time.Now(),
	}).Error
}

func (r *mpesaBalanceRepository) ResolveQuery(ctx context.Context, originatorConversationID string) (uint, error) {
	var row mpesaBalanceQueryRow
	result := r.db.WithContext(ctx).
		Where("originator_conversation_id = ?", originatorConversationID).
		First(&row)
	if errors.Is(result.Error, gorm.ErrRecordNotFound) {
		return 0, ErrMpesaBalanceQueryNotFound
	}
	if result.Error != nil {
		return 0, result.Error
	}
	return row.Shortcode, nil
}

func (r *mpesaBalanceRepository) RecordBalance(ctx context.Context, shortcode uint, accountName, currency string, availableKES int64, observedAt time.Time) error {
	id, err := uuid.NewV7()
	if err != nil {
		return err
	}
	return r.db.WithContext(ctx).Create(&mpesaAccountBalanceRow{
		ID:           id.String(),
		Shortcode:    shortcode,
		AccountName:  accountName,
		Currency:     currency,
		AvailableKES: availableKES,
		ObservedAt:   observedAt,
	}).Error
}
