// Package webhook provides services for processing payment provider webhook events.
package webhook

import (
	"context"
	"fmt"
	"log"

	"github.com/Shamba-Records-Limited/microvault/pkg/payment/yellowcard"
)

// WebhookEventHandler processes incoming YellowCard webhook events.
type WebhookEventHandler interface {
	// ProcessYellowCardEvent handles a single webhook event, mapping it to the
	// appropriate disbursement status update and triggering any side effects.
	ProcessYellowCardEvent(event yellowcard.WebhookEvent) error
}

// DisbursementUpdater is the interface for updating loan disbursement status.
// Implemented by the loan/disbursement service.
type DisbursementUpdater interface {
	// UpdateDisbursementStatus updates the disbursement status for a loan identified by sequenceID.
	UpdateDisbursementStatus(sequenceID string, status string) error

	// NotifyDisbursementComplete sends a notification (SMS) to the user that their disbursement completed.
	NotifyDisbursementComplete(sequenceID string) error

	// NotifyDisbursementFailed sends a notification (SMS) to the user that their disbursement failed.
	NotifyDisbursementFailed(sequenceID string) error

	// RepayVault returns borrowed USDC from treasury to the vault pool for the
	// loan identified by sequenceID. No-op if already repaid.
	RepayVault(sequenceID string) error
}

// AlertService is the interface for sending operational alerts.
type AlertService interface {
	// AlertOps sends an alert to the operations team.
	AlertOps(subject string, message string) error
}

// TransactionRecorder records and updates transaction records for disbursement events.
type TransactionRecorder interface {
	// UpdateTransactionByExternalID updates an existing transaction found by its external ID.
	UpdateTransactionByExternalID(ctx context.Context, externalID string, status string, externalStatus string) error

	// RecordFiatFailover records a fiat failover transaction after a direct settlement refund.
	RecordFiatFailover(ctx context.Context, rec RefundPendingRecord, newRequestID string) error
}

// Service processes incoming payment provider webhook events.
type Service struct {
	disbursements DisbursementUpdater
	alerts        AlertService
	transactions  TransactionRecorder
}

// NewService creates a new webhook processing service.
func NewService(disbursements DisbursementUpdater, alerts AlertService, transactions TransactionRecorder) *Service {
	return &Service{
		disbursements: disbursements,
		alerts:        alerts,
		transactions:  transactions,
	}
}

var _ WebhookEventHandler = (*Service)(nil)

// ProcessYellowCardEvent handles a single YellowCard webhook event by mapping
// it to the appropriate disbursement status update and side effects.
//
// Event → Action mapping:
//
//	DISBURSEMENT.COMPLETE / PAYMENT.COMPLETE → DisbursementComplete + notify user
//	DISBURSEMENT.FAILED / PAYMENT.FAILED → if direct: DisbursementRefundPending; if fiat: DisbursementFailed + alert ops
//	PENDING_LIQUIDITY → alert ops (YC balance low, auto-retries for 2hrs)
//	REFUNDED → DisbursementRefundReceived (RefundPoller handles fiat failover)
//	REFUND_FAILED → DisbursementFailed + alert ops
//	EXPIRED / CANCELLED → treat as FAILED
//	PROCESSING / PENDING / PROCESS → DisbursementProcessing
func (s *Service) ProcessYellowCardEvent(event yellowcard.WebhookEvent) error {
	seqID := event.Data.SequenceID
	paymentID := event.Data.PaymentID
	status := event.Data.Status
	isDirect := event.Data.DirectSettlement

	log.Printf("yellowcard webhook: event=%s payment=%s sequence=%s status=%s direct=%v",
		event.Event, paymentID, seqID, status, isDirect)

	switch event.Event {
	case yellowcard.EventDisbursementComplete, yellowcard.EventPaymentComplete:
		if err := s.disbursements.UpdateDisbursementStatus(seqID, yellowcard.DisbursementComplete); err != nil {
			return fmt.Errorf("update status to complete: %w", err)
		}
		if err := s.disbursements.NotifyDisbursementComplete(seqID); err != nil {
			log.Printf("yellowcard webhook: failed to notify user of completion for %s: %v", seqID, err)
		}

	case yellowcard.EventDisbursementFailed, yellowcard.EventPaymentFailed:
		return s.handleFailedEvent(seqID, paymentID, status, isDirect)

	default:
		// Route by status for events that don't have specific event constants.
		return s.handleByStatus(seqID, paymentID, status, isDirect)
	}

	return nil
}

// handleFailedEvent processes FAILED events with mode-specific logic.
func (s *Service) handleFailedEvent(seqID, paymentID, status string, isDirect bool) error {
	if isDirect {
		// Direct settlement failed after USDC was sent → YC will refund crypto.
		// Set to refund_pending; RefundPoller will detect the refund and trigger fiat failover.
		if err := s.disbursements.UpdateDisbursementStatus(seqID, yellowcard.DisbursementRefundPending); err != nil {
			return fmt.Errorf("update status to refund_pending: %w", err)
		}
		s.alertOps("Direct Settlement Failed",
			fmt.Sprintf("Payment %s (seq: %s) failed after USDC sent. Awaiting crypto refund.", paymentID, seqID))
	} else {
		// Fiat disbursement failed → terminal failure.
		if err := s.disbursements.UpdateDisbursementStatus(seqID, yellowcard.DisbursementFailed); err != nil {
			return fmt.Errorf("update status to failed: %w", err)
		}
		if err := s.disbursements.NotifyDisbursementFailed(seqID); err != nil {
			log.Printf("yellowcard webhook: failed to notify user of failure for %s: %v", seqID, err)
		}
		s.alertOps("Fiat Disbursement Failed",
			fmt.Sprintf("Payment %s (seq: %s) fiat disbursement failed.", paymentID, seqID))
	}
	return nil
}

// handleByStatus routes events based on the payment status field.
func (s *Service) handleByStatus(seqID, paymentID, status string, isDirect bool) error {
	switch status {
	case yellowcard.StatusComplete:
		if err := s.disbursements.UpdateDisbursementStatus(seqID, yellowcard.DisbursementComplete); err != nil {
			return fmt.Errorf("update status to complete: %w", err)
		}
		if err := s.disbursements.NotifyDisbursementComplete(seqID); err != nil {
			log.Printf("yellowcard webhook: failed to notify completion for %s: %v", seqID, err)
		}

	case yellowcard.StatusFailed, yellowcard.StatusExpired, yellowcard.StatusCancelled:
		return s.handleFailedEvent(seqID, paymentID, status, isDirect)

	case yellowcard.StatusPendingLiquidity:
		// YC balance is low; YC auto-retries for ~2 hours. Alert ops.
		s.alertOps("YellowCard Pending Liquidity",
			fmt.Sprintf("Payment %s (seq: %s) is pending liquidity. YC will auto-retry.", paymentID, seqID))

	case yellowcard.StatusPendingRefund, yellowcard.StatusRefundProcessing:
		// Crypto refund in progress — RefundPoller handles this.
		if err := s.disbursements.UpdateDisbursementStatus(seqID, yellowcard.DisbursementRefundPending); err != nil {
			return fmt.Errorf("update status to refund_pending: %w", err)
		}

	case yellowcard.StatusRefunded:
		// Crypto has been refunded. RefundPoller will detect this and trigger fiat failover.
		if err := s.disbursements.UpdateDisbursementStatus(seqID, yellowcard.DisbursementRefundReceived); err != nil {
			return fmt.Errorf("update status to refund_received: %w", err)
		}

	case yellowcard.StatusRefundFailed:
		// Refund failed — needs manual intervention.
		if err := s.disbursements.UpdateDisbursementStatus(seqID, yellowcard.DisbursementFailed); err != nil {
			return fmt.Errorf("update status to failed: %w", err)
		}
		s.alertOps("YellowCard Refund Failed",
			fmt.Sprintf("CRITICAL: Refund failed for payment %s (seq: %s). Manual intervention required.", paymentID, seqID))

	case yellowcard.StatusProcess, yellowcard.StatusProcessing, yellowcard.StatusPending,
		yellowcard.StatusCreated, yellowcard.StatusPendingApproval:
		if err := s.disbursements.UpdateDisbursementStatus(seqID, yellowcard.DisbursementProcessing); err != nil {
			return fmt.Errorf("update status to processing: %w", err)
		}

	case yellowcard.StatusPendingSettlement:
		// Direct settlement only: waiting for crypto payment. This is expected.
		if err := s.disbursements.UpdateDisbursementStatus(seqID, yellowcard.DisbursementDirectSubmitted); err != nil {
			return fmt.Errorf("update status to direct_submitted: %w", err)
		}

	default:
		log.Printf("yellowcard webhook: unhandled status %q for payment %s (seq: %s)", status, paymentID, seqID)
	}

	return nil
}

// alertOps sends an alert to the operations team, logging on failure.
func (s *Service) alertOps(subject, message string) {
	if s.alerts == nil {
		log.Printf("yellowcard webhook alert [%s]: %s", subject, message)
		return
	}
	if err := s.alerts.AlertOps(subject, message); err != nil {
		log.Printf("yellowcard webhook: failed to send ops alert [%s]: %v", subject, err)
	}
}
