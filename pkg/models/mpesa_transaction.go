package models

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

// MpesaTransactionSource is where an inbound notification came from. It decides
// which confirmation path can verify it.
type MpesaTransactionSource string

// The inbound sources.
const (
	MpesaSourceSTKCallback     MpesaTransactionSource = "stk_callback"
	MpesaSourceC2BConfirmation MpesaTransactionSource = "c2b_confirmation"
	MpesaSourcePull            MpesaTransactionSource = "pull"
	MpesaSourceStatusResult    MpesaTransactionSource = "status_result"
	MpesaSourceSTKQuery        MpesaTransactionSource = "stk_query"
)

// MpesaTransactionConfirmVia is how an observation was independently verified.
type MpesaTransactionConfirmVia string

// The confirmation paths.
const (
	MpesaConfirmViaSTKQuery          MpesaTransactionConfirmVia = "stk_query"
	MpesaConfirmViaPull              MpesaTransactionConfirmVia = "pull"
	MpesaConfirmViaTransactionStatus MpesaTransactionConfirmVia = "transaction_status"
)

// MpesaTransactionReversal is the state of a reversal for this transaction.
type MpesaTransactionReversal string

// The reversal states.
const (
	MpesaReversalNone     MpesaTransactionReversal = "none"
	MpesaReversalProposed MpesaTransactionReversal = "proposed"
	MpesaReversalApproved MpesaTransactionReversal = "approved"
	MpesaReversalSent     MpesaTransactionReversal = "sent"
	MpesaReversalComplete MpesaTransactionReversal = "complete"
	MpesaReversalFailed   MpesaTransactionReversal = "failed"
)

// MpesaTransaction is the observation log for inbound M-Pesa notifications.
// Every callback lands here before anything else happens, so the
// confirm-before-credit discipline is enforceable.
//
// A row is never evidence of a payment. Daraja signs nothing, so a well-formed
// callback proves only that something posted to a URL. It becomes a payment
// only after ConfirmedVia names an independent check.
type MpesaTransaction struct {
	ID string `json:"id" gorm:"type:uuid;primaryKey"`

	// TransID is the M-Pesa receipt. Unique-indexed. The idempotency key.
	TransID string `json:"trans_id" gorm:"column:trans_id;type:varchar(20);uniqueIndex;not null"`

	Source       MpesaTransactionSource      `json:"source" gorm:"type:varchar(20);not null;index"`
	Confirmed    bool                        `json:"confirmed" gorm:"not null;default:false"`
	ConfirmedVia *MpesaTransactionConfirmVia `json:"confirmed_via,omitempty" gorm:"type:varchar(20)"`

	BillRefNumber string  `json:"bill_ref_number" gorm:"type:varchar(20);index"`
	LoanID        *string `json:"loan_id,omitempty" gorm:"type:uuid;index"`

	AmountKes int64 `json:"amount_kes" gorm:"column:amount_kes;not null"` // minor units

	// MsidnMasked comes from a C2B callback; MsidnFull comes from the Pull
	// reconciler. Full is nullable PII; never log it.
	MsidnMasked string  `json:"msisdn_masked,omitempty" gorm:"column:msisdn_masked"`
	MsidnFull   *string `json:"msisdn_full,omitempty" gorm:"column:msisdn_full"`

	// PayerName comes from the C2B callback or a reversal result. Nullable PII.
	PayerName *string `json:"payer_name,omitempty" gorm:"type:varchar(100)"`

	TransTime time.Time `json:"trans_time" gorm:"not null"`

	// ThirdPartyTransID is our own correlation handle, echoed validation →
	// confirmation.
	ThirdPartyTransID *string `json:"third_party_trans_id,omitempty"`

	CheckoutRequestID *string `json:"checkout_request_id,omitempty" gorm:"index"` // STK
	MerchantRequestID *string `json:"merchant_request_id,omitempty"`              // STK
	SequenceID        *string `json:"sequence_id,omitempty" gorm:"type:varchar(200);index"`

	// RawPayload is the bytes as received. It is not optional: when a
	// reconciliation disagrees with a callback, the bytes settle it.
	RawPayload datatypes.JSON `json:"raw_payload" gorm:"type:jsonb;not null"`

	Reversal   MpesaTransactionReversal `json:"reversal_state" gorm:"column:reversal_state;type:varchar(20);not null;default:'none'"`
	NextPollAt *time.Time               `json:"next_poll_at,omitempty" gorm:"index"`

	CreatedAt time.Time `json:"created_at" gorm:"autoCreateTime;not null"`
	UpdatedAt time.Time `json:"updated_at" gorm:"autoUpdateTime;not null"`
}

// TableName specifies the table name for MpesaTransaction model
func (MpesaTransaction) TableName() string {
	return "mpesa_transactions"
}

// BeforeCreate sets the ID before creating a new MpesaTransaction
func (tx *MpesaTransaction) BeforeCreate(g *gorm.DB) error {
	id, err := uuid.NewV7()
	if err != nil {
		return err
	}
	tx.ID = id.String()
	return nil
}
