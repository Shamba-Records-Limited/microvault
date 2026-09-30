package webhook

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/Shamba-Records-Limited/microvault/pkg/contracts"
	"github.com/Shamba-Records-Limited/microvault/pkg/payment/offramp"
	"github.com/Shamba-Records-Limited/microvault/pkg/payment/yellowcard"
)

// RefundPendingFetcher retrieves loans that are awaiting crypto refund from YellowCard.
type RefundPendingFetcher interface {
	// GetRefundPendingDisbursements returns (sequenceID, paymentID) pairs for all
	// disbursements in DisbursementRefundPending status.
	GetRefundPendingDisbursements(ctx context.Context) ([]RefundPendingRecord, error)
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
type RefundPoller struct {
	ycAdapter    *yellowcard.YellowcardAdapter
	offRamp      offramp.Provider
	fetcher      RefundPendingFetcher
	disbursement contracts.DisbursementUpdater
	alerts       AlertService
	transactions TransactionRecorder
	config       RefundPollerConfig
}

// NewRefundPoller creates a new RefundPoller.
func NewRefundPoller(
	ycAdapter *yellowcard.YellowcardAdapter,
	offRamp offramp.Provider,
	fetcher RefundPendingFetcher,
	disbursement contracts.DisbursementUpdater,
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

	slog.InfoContext(ctx, "refund_poller: started with interval", slog.Duration("poll_interval", p.config.PollInterval))

	for {
		select {
		case <-ctx.Done():
			slog.InfoContext(ctx, "refund_poller: shutting down")
			return
		case <-ticker.C:
			p.poll(ctx)
		}
	}
}

// poll executes a single polling cycle.
func (p *RefundPoller) poll(ctx context.Context) {
	records, err := p.fetcher.GetRefundPendingDisbursements(ctx)
	if err != nil {
		slog.ErrorContext(ctx, "refund_poller: failed to fetch pending refunds", slog.Any("error", err))
		return
	}

	if len(records) == 0 {
		return
	}

	slog.InfoContext(ctx, "refund_poller: checking pending refunds", slog.Int("records", len(records)))

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
		slog.ErrorContext(ctx, "refund_poller: failed to lookup payment", slog.String("payment_id", rec.PaymentID), slog.Any("error", err))
		return
	}

	switch details.Status {
	case yellowcard.StatusRefunded:
		slog.ErrorContext(ctx, "refund_poller: payment refunded, attempting fiat failover", slog.String("payment_id", rec.PaymentID))

		if err := p.disbursement.UpdateDisbursementStatus(ctx, rec.SequenceID, yellowcard.DisbursementRefundReceived); err != nil {
			slog.ErrorContext(ctx, "refund_poller: failed to update status", slog.String("sequence_id", rec.SequenceID), slog.Any("error", err))
			return
		}

		// Attempt fiat failover. The OffRampService will check balance before submitting.
		p.attemptFiatFailover(ctx, rec)

	case yellowcard.StatusRefundFailed:
		slog.ErrorContext(ctx, "refund_poller: refund failed for payment", slog.String("payment_id", rec.PaymentID))

		if err := p.disbursement.UpdateDisbursementStatus(ctx, rec.SequenceID, yellowcard.DisbursementFailed); err != nil {
			slog.ErrorContext(ctx, "refund_poller: failed to update status", slog.String("sequence_id", rec.SequenceID), slog.Any("error", err))
		}
		p.alertOps("Refund Failed",
			fmt.Sprintf("CRITICAL: Refund failed for payment %s (seq: %s). Manual intervention required.", rec.PaymentID, rec.SequenceID))

	case yellowcard.StatusPendingRefund, yellowcard.StatusRefundProcessing:
		// Still in progress — will check again next poll cycle.

	default:
		slog.InfoContext(ctx, "refund_poller: unexpected status", slog.String("status", details.Status), slog.String("payment_id", rec.PaymentID), slog.String("sequence_id", rec.SequenceID))
	}
}

// attemptFiatFailover triggers a fiat-mode disbursement for a refunded direct settlement.
// This is a best-effort operation — if it fails, the ops team is alerted.
func (p *RefundPoller) attemptFiatFailover(ctx context.Context, rec RefundPendingRecord) {
	fiatResult, err := p.offRamp.Initiate(ctx, offramp.Request{
		LoanID:           rec.LoanID,
		UserID:           rec.UserID,
		RecipientName:    rec.RecipientName,
		AmountUSD:        rec.AmountUSD,
		AmountStroops:    rec.AmountStroops,
		DestinationPhone: rec.DestinationPhone,
		CountryCode:      rec.CountryCode,
		NetworkCode:      rec.NetworkCode,
		NetworkName:      rec.NetworkName,
		IdempotencyKey:   rec.SequenceID + "_fiat",
		Options: yellowcard.Options{
			SettlementMethod: yellowcard.SettlementMethodFiat,
		},
	})
	if err != nil {
		slog.ErrorContext(ctx, "refund_poller: fiat failover failed", slog.String("payment_id", rec.PaymentID), slog.Any("error", err))
		p.alertOps("Fiat Failover Failed",
			fmt.Sprintf("Payment %s (seq: %s) fiat failover failed: %v", rec.PaymentID, rec.SequenceID, err))

		if err := p.disbursement.UpdateDisbursementStatus(ctx, rec.SequenceID, yellowcard.DisbursementFailed); err != nil {
			slog.ErrorContext(ctx, "refund_poller: failed to mark: as failed", slog.String("sequence_id", rec.SequenceID), slog.Any("error", err))
		}

		// USDC is back in treasury after refund, all attempts exhausted — repay vault.
		if repayErr := p.disbursement.RepayVault(ctx, rec.SequenceID); repayErr != nil {
			slog.ErrorContext(ctx, "refund_poller: failed to repay vault", slog.String("sequence_id", rec.SequenceID), slog.Any("error", repayErr))
		}
		return
	}

	// Flip settlement_method first so the eventual DisbursementComplete
	// event sees "fiat" and triggers the vault repay (the original "direct"
	// stamp would silently skip it).
	if err := p.disbursement.SetSettlementMethod(ctx, rec.SequenceID, "fiat"); err != nil {
		slog.ErrorContext(ctx, "refund_poller: failed to flip settlement_method to fiat", slog.String("sequence_id", rec.SequenceID), slog.Any("error", err))
	}

	fiatStatus := "fiat_submitted"
	if err := p.disbursement.UpdateDisbursementStatus(ctx, rec.SequenceID, fiatStatus); err != nil {
		slog.ErrorContext(ctx, "refund_poller: failed to update fiat status", slog.String("sequence_id", rec.SequenceID), slog.Any("error", err))
	}

	// Record fiat failover transaction via recorder.
	if p.transactions != nil {
		if err := p.transactions.RecordFiatFailover(ctx, rec, fiatResult.RequestID); err != nil {
			slog.ErrorContext(ctx, "refund_poller: failed to record fiat failover transaction", slog.Any("error", err))
		}
	}

	slog.ErrorContext(ctx, "refund_poller: fiat failover initiated for: new request_id", slog.String("payment_id", rec.PaymentID), slog.String("request_id", fiatResult.RequestID))
}

// alertOps sends an alert to the operations team, logging on failure.
func (p *RefundPoller) alertOps(subject, message string) {
	if p.alerts == nil {
		slog.Info("refund_poller alert", slog.String("subject", subject), slog.String("message", message))
		return
	}
	if err := p.alerts.AlertOps(subject, message); err != nil {
		slog.Error("refund_poller: failed to send ops alert", slog.String("subject", subject), slog.Any("error", err))
	}
}
