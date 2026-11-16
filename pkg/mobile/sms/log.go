package sms

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	pkgErrors "github.com/Shamba-Records-Limited/microvault/pkg/errors"
	"github.com/Shamba-Records-Limited/microvault/pkg/phone"
)

// Message statuses set at send time; delivery reports then overwrite them
// with the gateway's own (Sent, Buffered, Success, Failed, ...).
const (
	StatusSubmitted  = "Submitted"
	StatusRejected   = "Rejected"
	StatusSendFailed = "SendFailed"
)

// finalStatuses are never overwritten by a later report.
var finalStatuses = []string{"Success", "Failed", "Rejected", "Expired", StatusSendFailed}

// Message is one outbound SMS as sent and as last reported. The body is not
// kept: Kind and Ref say what it was.
type Message struct {
	ID                string    `gorm:"type:uuid;primaryKey"`
	Provider          string    `gorm:"type:varchar(20);not null"`
	ProviderMessageID *string   `gorm:"type:varchar(100)"`
	PhoneNumber       string    `gorm:"type:varchar(20);not null"`
	Kind              string    `gorm:"type:varchar(40);not null"`
	Ref               string    `gorm:"type:varchar(64);not null"`
	Status            string    `gorm:"type:varchar(24);not null"`
	FailureReason     string    `gorm:"type:varchar(200);not null"`
	NetworkCode       string    `gorm:"type:varchar(10);not null"`
	Cost              string    `gorm:"type:varchar(24);not null"`
	CreatedAt         time.Time `gorm:"not null"`
	UpdatedAt         time.Time `gorm:"not null"`
}

// TableName pins the table name.
func (Message) TableName() string { return "sms_messages" }

// Final reports whether no further report will change the status.
func (m Message) Final() bool {
	for _, s := range finalStatuses {
		if m.Status == s {
			return true
		}
	}
	return false
}

// Delivered reports whether the handset received the message.
func (m Message) Delivered() bool { return m.Status == "Success" }

// StoredPhone is a number in the form sms_messages and users.mobile_number
// hold: country code and subscriber digits, no leading +.
func StoredPhone(number string) string {
	e164 := phone.KenyaE164(number)
	if e164 == "" {
		e164 = phone.E164(number)
	}
	if e164 == "" {
		return strings.TrimPrefix(strings.TrimSpace(number), "+")
	}
	return strings.TrimPrefix(e164, "+")
}

// Log is the Postgres record of outbound SMS.
type Log struct {
	db *gorm.DB
}

// NewLog builds the log.
func NewLog(db *gorm.DB) (*Log, error) {
	if db == nil {
		return nil, pkgErrors.ErrNilDB
	}
	return &Log{db: db}, nil
}

// Record inserts m, filling its ID and normalising its phone number.
func (l *Log) Record(ctx context.Context, m *Message) error {
	if m.ID == "" {
		id, err := uuid.NewV7()
		if err != nil {
			return fmt.Errorf("sms log: new id: %w", err)
		}
		m.ID = id.String()
	}
	m.PhoneNumber = StoredPhone(m.PhoneNumber)
	m.FailureReason = clip(m.FailureReason, 200)
	if err := l.db.WithContext(ctx).Create(m).Error; err != nil {
		return fmt.Errorf("sms log: record: %w", err)
	}
	return nil
}

// ApplyReport moves the message the report names to the reported status,
// unless it already reached a final one. It reports whether a row changed.
func (l *Log) ApplyReport(ctx context.Context, r DeliveryReport) (bool, error) {
	res := l.db.WithContext(ctx).Model(&Message{}).
		Where("provider_message_id = ? AND status NOT IN ?", r.ID, finalStatuses).
		Updates(map[string]any{
			"status":         clip(r.Status, 24),
			"failure_reason": clip(r.FailureReason, 200),
			"network_code":   clip(r.NetworkCode, 10),
			"updated_at":     time.Now(),
		})
	if res.Error != nil {
		return false, fmt.Errorf("sms log: apply report: %w", res.Error)
	}
	return res.RowsAffected > 0, nil
}

// ForRef lists the messages about ref, newest first.
func (l *Log) ForRef(ctx context.Context, ref string, limit int) ([]Message, error) {
	var out []Message
	if err := l.db.WithContext(ctx).Where("ref = ?", ref).Order("created_at DESC").Limit(limit).Find(&out).Error; err != nil {
		return nil, fmt.Errorf("sms log: for ref: %w", err)
	}
	return out, nil
}

// ForPhone lists the messages sent to number, newest first.
func (l *Log) ForPhone(ctx context.Context, number string, limit int) ([]Message, error) {
	var out []Message
	if err := l.db.WithContext(ctx).Where("phone_number = ?", StoredPhone(number)).
		Order("created_at DESC").Limit(limit).Find(&out).Error; err != nil {
		return nil, fmt.Errorf("sms log: for phone: %w", err)
	}
	return out, nil
}

// LatestOfKind returns the newest message of kind to each of numbers, keyed
// by the number as stored.
func (l *Log) LatestOfKind(ctx context.Context, kind string, numbers []string) (map[string]*Message, error) {
	out := make(map[string]*Message, len(numbers))
	if len(numbers) == 0 {
		return out, nil
	}
	stored := make([]string, len(numbers))
	for i, n := range numbers {
		stored[i] = StoredPhone(n)
	}
	var rows []Message
	err := l.db.WithContext(ctx).Raw(`SELECT DISTINCT ON (phone_number) * FROM sms_messages
		WHERE kind = ? AND phone_number IN ? ORDER BY phone_number, created_at DESC`, kind, stored).Scan(&rows).Error
	if err != nil {
		return nil, fmt.Errorf("sms log: latest of kind: %w", err)
	}
	for i := range rows {
		out[rows[i].PhoneNumber] = &rows[i]
	}
	return out, nil
}

func clip(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
