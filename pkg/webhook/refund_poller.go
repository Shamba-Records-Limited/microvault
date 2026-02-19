package webhook

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/Shamba-Records-Limited/microvault/pkg/payment/yellowcard"
	"github.com/Shamba-Records-Limited/microvault/pkg/mobile/ussd/adapters"
)

// RefundPendingFetcher retrieves loans that are awaiting crypto refund from YellowCard.
type RefundPendingFetcher interface {
	// GetRefundPendingDisbursements returns (sequenceID, paymentID) pairs for all
	// disbursements in DisbursementRefundPending status.
	GetRefundPendingDisbursements() ([]RefundPendingRecord, error)
}

// RefundPendingRecord identifies a disbursement awaiting refund,
// including the data needed to attempt a fiat failover.
type RefundPendingRecord struct {
	SequenceID       string  // YC sequenceId / idempotency key
	PaymentID        string  // YC payment ID
	LoanID           string  // Loan ID for tracking
	UserID           string  // User ID for tracking
	RecipientName    string  // Recipient name for fiat disbursement
	AmountUSD        float64 // Amount in USD
	AmountStroops    int64   // Amount in stroops (USDC * 10^7)
	RampFiatAmount   int64   // Original off-ramp fiat amount in cents (local currency)
	RampFiatCurrency string  // Original off-ramp fiat currency (e.g. "KES")
	DestinationPhone string  // Recipient phone number
	CountryCode      string  // ISO country code
	NetworkCode      string  // MoMo network code
	NetworkName      string  // MoMo network name
}

// RefundPollerConfig configures the refund polling behavior.
type RefundPollerConfig struct {
	PollInterval time.Duration // How often to poll (default: 30s)
}

// DefaultRefundPollerConfig returns sensible defaults.
func DefaultRefundPollerConfig() RefundPollerConfig {
	return RefundPollerConfig{
		PollInterval: 30 * time.Second,
	}
}

// RefundPoller periodically checks YellowCard for refund status on disbursements
// that failed after USDC was sent (direct settlement F3 failover).
//
// Flow:
//  1. Fetch loans in DisbursementRefundPending status from local DB
//  2. For each, call YC API to check current status
//  3. On "refunded" → update to DisbursementRefundReceived, attempt fiat failover
//  4. On "refund_failed" → update to DisbursementFailed, alert ops
//  5. On "pending_refund" / "refund_processing" → skip, poll again next cycle
type RefundPoller struct {
	ycAdapter    *yellowcard.YellowcardAdapter
	offRamp      adapters.OffRampService
	fetcher      RefundPendingFetcher
	disbursement DisbursementUpdater
	alerts       AlertService
	transactions TransactionRecorder
	config       RefundPollerConfig
}

// NewRefundPoller creates a new RefundPoller.
func NewRefundPoller(
	ycAdapter *yellowcard.YellowcardAdapter,
	offRamp adapters.OffRampService,
	fetcher RefundPendingFetcher,
	disbursement DisbursementUpdater,
	alerts AlertService,
	transactions TransactionRecorder,
	config RefundPollerConfig,
) *RefundPoller {
	return &RefundPoller{
		ycAdapter:    ycAdapter,
		offRamp:      offRamp,
		fetcher:      fetcher,
		disbursement: disbursement,
		alerts:       alerts,
		transactions: transactions,
		config:       config,
	}
}

// Start runs the RefundPoller in a background goroutine. It polls until the
// context is cancelled (graceful shutdown).
func (p *RefundPoller) Start(ctx context.Context) {
	ticker := time.NewTicker(p.config.PollInterval)
	defer ticker.Stop()

	log.Printf("refund_poller: started with interval %s", p.config.PollInterval)

	for {
		select {
		case <-ctx.Done():
			log.Printf("refund_poller: shutting down")
			return
		case <-ticker.C:
			p.poll(ctx)
		}
	}
}

// poll executes a single polling cycle.
func (p *RefundPoller) poll(ctx context.Context) {
	records, err := p.fetcher.GetRefundPendingDisbursements()
	if err != nil {
		log.Printf("refund_poller: failed to fetch pending refunds: %v", err)
		return
	}

	if len(records) == 0 {
		return
	}

	log.Printf("refund_poller: checking %d pending refunds", len(records))

	for _, rec := range records {
		if ctx.Err() != nil {
			return
		}
		p.checkRefund(ctx, rec)
	}
}

// checkRefund checks the YC API for the current status of a single payment
// and takes appropriate action.
func (p *RefundPoller) checkRefund(ctx context.Context, rec RefundPendingRecord) {
	details, err := p.ycAdapter.LookupPayment(ctx, rec.PaymentID)
	if err != nil {
		log.Printf("refund_poller: failed to lookup payment %s: %v", rec.PaymentID, err)
		return
	}

	switch details.Status {
	case yellowcard.StatusRefunded:
		log.Printf("refund_poller: payment %s refunded, attempting fiat failover", rec.PaymentID)

		if err := p.disbursement.UpdateDisbursementStatus(rec.SequenceID, yellowcard.DisbursementRefundReceived); err != nil {
			log.Printf("refund_poller: failed to update status for %s: %v", rec.SequenceID, err)
			return
		}

		// Attempt fiat failover. The OffRampService will check balance before submitting.
		p.attemptFiatFailover(ctx, rec)

	case yellowcard.StatusRefundFailed:
		log.Printf("refund_poller: refund failed for payment %s", rec.PaymentID)

		if err := p.disbursement.UpdateDisbursementStatus(rec.SequenceID, yellowcard.DisbursementFailed); err != nil {
			log.Printf("refund_poller: failed to update status for %s: %v", rec.SequenceID, err)
		}
		p.alertOps("Refund Failed",
			fmt.Sprintf("CRITICAL: Refund failed for payment %s (seq: %s). Manual intervention required.", rec.PaymentID, rec.SequenceID))

	case yellowcard.StatusPendingRefund, yellowcard.StatusRefundProcessing:
		// Still in progress — will check again next poll cycle.

	default:
		log.Printf("refund_poller: unexpected status %q for payment %s (seq: %s)",
			details.Status, rec.PaymentID, rec.SequenceID)
	}
}

// attemptFiatFailover triggers a fiat-mode disbursement for a refunded direct settlement.
// This is a best-effort operation — if it fails, the ops team is alerted.
func (p *RefundPoller) attemptFiatFailover(ctx context.Context, rec RefundPendingRecord) {
	fiatResult, err := p.offRamp.InitiateOffRamp(ctx, adapters.OffRampRequest{
		LoanID:           rec.LoanID,
		UserID:           rec.UserID,
		RecipientName:    rec.RecipientName,
		AmountUSD:        rec.AmountUSD,
		AmountStroops:    rec.AmountStroops,
		DestinationPhone: rec.DestinationPhone,
		CountryCode:      rec.CountryCode,
		NetworkCode:      rec.NetworkCode,
		NetworkName:      rec.NetworkName,
		SettlementMethod: "fiat",
		IdempotencyKey:   rec.SequenceID + "_fiat",
	})
	if err != nil {
		log.Printf("refund_poller: fiat failover failed for %s: %v", rec.PaymentID, err)
		p.alertOps("Fiat Failover Failed",
			fmt.Sprintf("Payment %s (seq: %s) fiat failover failed: %v", rec.PaymentID, rec.SequenceID, err))

		if err := p.disbursement.UpdateDisbursementStatus(rec.SequenceID, yellowcard.DisbursementFailed); err != nil {
			log.Printf("refund_poller: failed to mark %s as failed: %v", rec.SequenceID, err)
		}
		return
	}

	fiatStatus := "fiat_submitted"
	if err := p.disbursement.UpdateDisbursementStatus(rec.SequenceID, fiatStatus); err != nil {
		log.Printf("refund_poller: failed to update fiat status for %s: %v", rec.SequenceID, err)
	}

	// Record fiat failover transaction via recorder.
	if p.transactions != nil {
		if err := p.transactions.RecordFiatFailover(ctx, rec, fiatResult.RequestID); err != nil {
			log.Printf("refund_poller: failed to record fiat failover transaction: %v", err)
		}
	}

	log.Printf("refund_poller: fiat failover initiated for %s, new request_id=%s",
		rec.PaymentID, fiatResult.RequestID)
}

// alertOps sends an alert to the operations team, logging on failure.
func (p *RefundPoller) alertOps(subject, message string) {
	if p.alerts == nil {
		log.Printf("refund_poller alert [%s]: %s", subject, message)
		return
	}
	if err := p.alerts.AlertOps(subject, message); err != nil {
		log.Printf("refund_poller: failed to send ops alert [%s]: %v", subject, err)
	}
}
