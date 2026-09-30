package webhook

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/Shamba-Records-Limited/microvault/pkg/contracts"
	"github.com/Shamba-Records-Limited/microvault/pkg/logging"
	"github.com/Shamba-Records-Limited/microvault/pkg/payment/yellowcard"

	"github.com/samber/oops"

	pkgErrors "github.com/Shamba-Records-Limited/microvault/pkg/errors"
)

// WebhookEventHandler processes incoming YellowCard webhook events.
type WebhookEventHandler interface {
	// ProcessYellowCardEvent handles a single webhook event, mapping it to the
	// appropriate disbursement status update and triggering any side effects.
	ProcessYellowCardEvent(ctx context.Context, event yellowcard.WebhookEvent) error
}

// webhookErr starts an error builder for provider callback handling. These
// errors reach the refund poller's escalation path, so the sequence ID and the
// status being written are the attributes ops read first.
func webhookErr(op string) oops.OopsErrorBuilder {
	return oops.In(pkgErrors.DomainOffRamp).Tags("webhook").With(pkgErrors.AttrOperation, op)
}

// PaymentLookup fetches final payment details (amounts, fees) at completion.
type PaymentLookup interface {
	LookupPayment(ctx context.Context, paymentID string) (*yellowcard.PaymentDetails, error)
}

// AlertService is the interface for sending operational alerts.
type AlertService interface {
	// AlertOps sends an alert to the operations team.
	AlertOps(subject string, message string) error
}

// TransactionRecorder records and updates transaction records for disbursement events.
type TransactionRecorder interface {
	// UpdateOffRampTransaction syncs a loan's off-ramp row with the provider's
	// reported status. Keyed by loan rather than by external ID, which no longer
	// identifies a single row now that every leg of an anchor transaction shares
	// the provider's request ID.
	UpdateOffRampTransaction(ctx context.Context, loanID string, status string, externalStatus string) error

	// RecordFiatFailover records a fiat failover transaction after a direct settlement refund.
	RecordFiatFailover(ctx context.Context, rec RefundPendingRecord, newRequestID string) error
}

// Service processes incoming payment provider webhook events.
type Service struct {
	disbursements contracts.DisbursementUpdater
	alerts        AlertService
	transactions  TransactionRecorder
	payments      PaymentLookup
}

// NewService creates a new webhook processing service.
func NewService(disbursements contracts.DisbursementUpdater, alerts AlertService, transactions TransactionRecorder, payments PaymentLookup) *Service {
	return &Service{
		disbursements: disbursements,
		alerts:        alerts,
		transactions:  transactions,
		payments:      payments,
	}
}

var _ WebhookEventHandler = (*Service)(nil)

// ProcessYellowCardEvent handles a single YellowCard webhook event by mapping
// it to the appropriate disbursement status update and side effects.
//
// Event to Action mapping:
//
//	DISBURSEMENT.COMPLETE / PAYMENT.COMPLETE / SEND.COMPLETE to DisbursementComplete + notify user
//	DISBURSEMENT.FAILED / PAYMENT.FAILED / SEND.FAILED to if direct: DisbursementRefundPending; if fiat: DisbursementFailed + alert ops
//	PENDING_LIQUIDITY to alert ops (YC balance low, auto-retries for 2hrs)
//	REFUNDED to DisbursementRefundReceived (RefundPoller handles fiat failover)
//	REFUND_FAILED to DisbursementFailed + alert ops
//	EXPIRED / CANCELLED to treat as FAILED
//	PROCESSING / PENDING / PROCESS to DisbursementProcessing
func (s *Service) ProcessYellowCardEvent(ctx context.Context, event yellowcard.WebhookEvent) error {
	seqID := event.SequenceID
	paymentID := event.PaymentID
	status := event.Status

	ctx = logging.With(ctx, slog.String(pkgErrors.AttrSequenceID, seqID), slog.String("payment_id", paymentID))
	slog.InfoContext(ctx, "yellowcard webhook received", slog.String("event", event.Event), slog.String("status", status))

	// YC's webhook payload doesn't carry directSettlement; derive it from the
	// loan record. Look up lazily — only failed events branch on it.
	directSettlementLookup := func() (bool, error) {
		return s.disbursements.IsDirectSettlement(ctx, seqID)
	}

	switch event.Event {
	case yellowcard.EventDisbursementComplete, yellowcard.EventPaymentComplete,
		yellowcard.EventSendComplete:
		return s.handleComplete(ctx, seqID, paymentID)

	case yellowcard.EventDisbursementFailed, yellowcard.EventPaymentFailed,
		yellowcard.EventSendFailed:
		isDirect, err := directSettlementLookup()
		if err != nil {
			return webhookErr("lookup_settlement_method").With(pkgErrors.AttrSequenceID, seqID).
				Code(pkgErrors.CodeLoanLoadFailed).Wrapf(err, "could not look up the settlement method")
		}
		return s.handleFailedEvent(ctx, seqID, paymentID, status, isDirect)

	default:
		// Route by status for events that don't have specific event constants.
		// handleByStatus does its own lazy lookup if a failed branch is hit.
		return s.handleByStatus(ctx, seqID, paymentID, status, directSettlementLookup)
	}
}

// handleComplete marks the disbursement complete, records the final financials
// from a payment lookup (delivered amount + service/partner fees), and notifies
// the borrower.
func (s *Service) handleComplete(ctx context.Context, seqID, paymentID string) error {
	if err := s.disbursements.UpdateDisbursementStatus(ctx, seqID, yellowcard.DisbursementComplete); err != nil {
		return webhookErr("update_status").With("target_status", "complete").
			Code(pkgErrors.CodeStateWriteFailed).Wrapf(err, "could not write the disbursement status")
	}
	s.recordCompletionFinancials(ctx, seqID, paymentID)
	if err := s.disbursements.NotifyDisbursementComplete(ctx, seqID); err != nil {
		slog.ErrorContext(ctx, "yellowcard webhook: failed to notify completion", slog.Any("error", err))
	}
	return nil
}

func (s *Service) recordCompletionFinancials(ctx context.Context, seqID, paymentID string) {
	if s.payments == nil || paymentID == "" {
		return
	}
	details, err := s.payments.LookupPayment(ctx, paymentID)
	if err != nil {
		slog.ErrorContext(ctx, "yellowcard webhook: failed to look up payment for completion financials", slog.Any("error", err))
		return
	}
	fin := contracts.CompletionFinancials{
		ConvertedAmountLocal:  details.ConvertedAmount,
		ServiceFeeAmountUSD:   details.ServiceFeeAmountUSD,
		ServiceFeeAmountLocal: details.ServiceFeeAmountLocal,
		PartnerFeeAmountUSD:   details.PartnerFeeAmountUSD,
		PartnerFeeAmountLocal: details.PartnerFeeAmountLocal,
	}
	if err := s.disbursements.RecordDisbursementCompletion(ctx, seqID, fin); err != nil {
		slog.ErrorContext(ctx, "yellowcard webhook: failed to record completion financials", slog.Any("error", err))
	}
}

// handleFailedEvent processes FAILED events with mode-specific logic.
func (s *Service) handleFailedEvent(ctx context.Context, seqID, paymentID, status string, isDirect bool) error {
	if isDirect {
		// Direct settlement failed after USDC was sent to YC will refund crypto.
		// Set to refund_pending; RefundPoller will detect the refund and trigger fiat failover.
		if err := s.disbursements.UpdateDisbursementStatus(ctx, seqID, yellowcard.DisbursementRefundPending); err != nil {
			return webhookErr("update_status").With("target_status", "refund_pending").
				Code(pkgErrors.CodeStateWriteFailed).Wrapf(err, "could not write the disbursement status")
		}
		s.alertOps(ctx, "Direct Settlement Failed",
			fmt.Sprintf("Payment %s (seq: %s) failed after USDC sent. Awaiting crypto refund.", paymentID, seqID))
	} else {
		// Fiat disbursement failed to terminal failure.
		if err := s.disbursements.UpdateDisbursementStatus(ctx, seqID, yellowcard.DisbursementFailed); err != nil {
			return webhookErr("update_status").With("target_status", "failed").
				Code(pkgErrors.CodeStateWriteFailed).Wrapf(err, "could not write the disbursement status")
		}
		if err := s.disbursements.NotifyDisbursementFailed(ctx, seqID); err != nil {
			slog.ErrorContext(ctx, "yellowcard webhook: failed to notify user of failure", slog.Any("error", err))
		}
		s.alertOps(ctx, "Fiat Disbursement Failed",
			fmt.Sprintf("Payment %s (seq: %s) fiat disbursement failed.", paymentID, seqID))
	}
	return nil
}

// handleByStatus routes events based on the payment status field. directLookup
// is only invoked when a failed branch is hit, to avoid a needless loan read
// on every Processing/Pending tick.
func (s *Service) handleByStatus(ctx context.Context, seqID, paymentID, status string, directLookup func() (bool, error)) error {
	switch status {
	case yellowcard.StatusComplete:
		return s.handleComplete(ctx, seqID, paymentID)

	case yellowcard.StatusFailed, yellowcard.StatusExpired, yellowcard.StatusCancelled:
		isDirect, err := directLookup()
		if err != nil {
			return webhookErr("lookup_settlement_method").With(pkgErrors.AttrSequenceID, seqID).
				Code(pkgErrors.CodeLoanLoadFailed).Wrapf(err, "could not look up the settlement method")
		}
		return s.handleFailedEvent(ctx, seqID, paymentID, status, isDirect)

	case yellowcard.StatusPendingLiquidity:
		// YC balance is low; YC auto-retries for ~2 hours. Alert ops.
		s.alertOps(ctx, "YellowCard Pending Liquidity",
			fmt.Sprintf("Payment %s (seq: %s) is pending liquidity. YC will auto-retry.", paymentID, seqID))

	case yellowcard.StatusPendingRefund, yellowcard.StatusRefundProcessing:
		// Crypto refund in progress — RefundPoller handles this.
		if err := s.disbursements.UpdateDisbursementStatus(ctx, seqID, yellowcard.DisbursementRefundPending); err != nil {
			return webhookErr("update_status").With("target_status", "refund_pending").
				Code(pkgErrors.CodeStateWriteFailed).Wrapf(err, "could not write the disbursement status")
		}

	case yellowcard.StatusRefunded:
		// Crypto has been refunded. RefundPoller will detect this and trigger fiat failover.
		if err := s.disbursements.UpdateDisbursementStatus(ctx, seqID, yellowcard.DisbursementRefundReceived); err != nil {
			return webhookErr("update_status").With("target_status", "refund_received").
				Code(pkgErrors.CodeStateWriteFailed).Wrapf(err, "could not write the disbursement status")
		}

	case yellowcard.StatusRefundFailed:
		// Refund failed — needs manual intervention.
		if err := s.disbursements.UpdateDisbursementStatus(ctx, seqID, yellowcard.DisbursementFailed); err != nil {
			return webhookErr("update_status").With("target_status", "failed").
				Code(pkgErrors.CodeStateWriteFailed).Wrapf(err, "could not write the disbursement status")
		}
		s.alertOps(ctx, "YellowCard Refund Failed",
			fmt.Sprintf("CRITICAL: Refund failed for payment %s (seq: %s). Manual intervention required.", paymentID, seqID))

	case yellowcard.StatusProcess, yellowcard.StatusProcessing, yellowcard.StatusPending,
		yellowcard.StatusCreated, yellowcard.StatusPendingApproval:
		if err := s.disbursements.UpdateDisbursementStatus(ctx, seqID, yellowcard.DisbursementProcessing); err != nil {
			return webhookErr("update_status").With("target_status", "processing").
				Code(pkgErrors.CodeStateWriteFailed).Wrapf(err, "could not write the disbursement status")
		}

	case yellowcard.StatusPendingSettlement:
		// Direct settlement only: waiting for crypto payment. This is expected.
		if err := s.disbursements.UpdateDisbursementStatus(ctx, seqID, yellowcard.DisbursementDirectSubmitted); err != nil {
			return webhookErr("update_status").With("target_status", "direct_submitted").
				Code(pkgErrors.CodeStateWriteFailed).Wrapf(err, "could not write the disbursement status")
		}

	default:
		slog.WarnContext(ctx, "yellowcard webhook: unhandled status", slog.String("status", status))
	}

	return nil
}

// alertOps sends an alert to the operations team, logging on failure.
func (s *Service) alertOps(ctx context.Context, subject, message string) {
	if s.alerts == nil {
		slog.InfoContext(ctx, "yellowcard webhook alert", slog.String("subject", subject), slog.String("message", message))
		return
	}
	if err := s.alerts.AlertOps(subject, message); err != nil {
		slog.ErrorContext(ctx, "yellowcard webhook: failed to send ops alert", slog.String("subject", subject), slog.Any("error", err))
	}
}
