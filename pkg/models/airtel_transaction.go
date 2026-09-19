package models

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

// AirtelTransactionSource is where an observation came from.
type AirtelTransactionSource string

// The inbound sources.
const (
	AirtelSourceCallback AirtelTransactionSource = "callback"
	AirtelSourceEnquiry  AirtelTransactionSource = "enquiry"
	AirtelSourceSummary  AirtelTransactionSource = "summary"
)

// AirtelTransactionConfirmVia is how an observation was independently
// verified. A callback is not on this list, and that is the point: Airtel's
// callback carries intermediate or final status with nothing to tell them
// apart, so it can never be the thing that confirms.
type AirtelTransactionConfirmVia string

// The confirmation paths.
const (
	AirtelConfirmViaEnquiry AirtelTransactionConfirmVia = "enquiry"
	AirtelConfirmViaSummary AirtelTransactionConfirmVia = "summary"
)

// AirtelTransaction is the observation log for inbound Airtel Money
// notifications.
//
// A row is never evidence of a payment. A verified hash proves Airtel sent
// the callback; it does not prove the payment settled, and an intermediate
// callback looks exactly like a final one. The row becomes a payment only
// once ConfirmedVia names an enquiry.
type AirtelTransaction struct {
	ID string `json:"id" gorm:"type:uuid;primaryKey"`

	// PartnerTxnID is the id we generated and sent. Unique-indexed, and the
	// idempotency key — Airtel's own receipt cannot be, because it does not
	// exist until the transaction succeeds.
	PartnerTxnID string `json:"partner_txn_id" gorm:"column:partner_txn_id;type:varchar(64);uniqueIndex;not null"`

	// AirtelMoneyID is Airtel's receipt, minted only on TS. It is the only
	// key a refund accepts, which is why a refund is impossible until an
	// enquiry or a callback has disclosed it.
	AirtelMoneyID *string `json:"airtel_money_id,omitempty" gorm:"column:airtel_money_id;type:varchar(64)"`

	Source     AirtelTransactionSource `json:"source" gorm:"type:varchar(20);not null;index"`
	StatusCode string                  `json:"status_code" gorm:"type:varchar(4);not null"`

	Confirmed    bool                         `json:"confirmed" gorm:"not null;default:false"`
	ConfirmedVia *AirtelTransactionConfirmVia `json:"confirmed_via,omitempty" gorm:"type:varchar(20)"`

	// HashVerified records whether the callback's HmacSHA256 checked out.
	// HashVariant records which reading of "the callback body" matched, so
	// the answer the portal does not give is recorded from live traffic
	// rather than inferred.
	HashVerified bool    `json:"hash_verified" gorm:"not null;default:false"`
	HashVariant  *string `json:"hash_variant,omitempty" gorm:"type:varchar(20)"`

	Reference string  `json:"reference" gorm:"type:varchar(25);index"`
	LoanID    *string `json:"loan_id,omitempty" gorm:"type:uuid;index"`

	// AmountMinor is in minor units throughout. The M-Pesa rail's equivalent
	// column is named for shillings and stores them; naming this one for the
	// unit it holds removes the question.
	AmountMinor int64 `json:"amount_minor" gorm:"column:amount_minor;not null"`

	// AppliedStroops is set once, the first time this row is converted to
	// USDC and credited toward LoanID's repayment progress.
	AppliedStroops *int64 `json:"applied_stroops,omitempty" gorm:"column:applied_stroops"`

	// Msisdn is the payer's number in Airtel's national form. Nullable PII;
	// never log it. Airtel does not mask it the way a C2B confirmation does,
	// so there is no masked counterpart and no safe-to-log version.
	Msisdn    *string `json:"msisdn,omitempty" gorm:"type:varchar(25)"`
	PayerName *string `json:"payer_name,omitempty" gorm:"type:varchar(100)"`

	TransTime time.Time `json:"trans_time" gorm:"not null"`

	// RawPayload is the bytes as received. It is not optional: when an
	// enquiry disagrees with a callback, the bytes settle it — and with two
	// callbacks per transaction, which bytes arrived when is the only record
	// of the intermediate state.
	RawPayload datatypes.JSON `json:"raw_payload" gorm:"type:jsonb;not null"`

	NextPollAt   *time.Time `json:"next_poll_at,omitempty" gorm:"index"`
	PollAttempts int        `json:"poll_attempts" gorm:"not null;default:0"`

	CreatedAt time.Time `json:"created_at" gorm:"autoCreateTime;not null"`
	UpdatedAt time.Time `json:"updated_at" gorm:"autoUpdateTime;not null"`
}

// TableName specifies the table name for AirtelTransaction.
func (AirtelTransaction) TableName() string {
	return "airtel_transactions"
}

// BeforeCreate sets the ID before creating a new AirtelTransaction.
func (tx *AirtelTransaction) BeforeCreate(_ *gorm.DB) error {
	id, err := uuid.NewV7()
	if err != nil {
		return err
	}
	tx.ID = id.String()
	return nil
}
