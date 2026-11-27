package webhook

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/Shamba-Records-Limited/microvault/pkg/contracts"
	"github.com/Shamba-Records-Limited/microvault/pkg/logging"
	"github.com/Shamba-Records-Limited/microvault/pkg/payment/fonbnk"
	"github.com/Shamba-Records-Limited/microvault/pkg/payment/yellowcard"

	pkgErrors "github.com/Shamba-Records-Limited/microvault/pkg/errors"
)

// WithSettlement sets the checker that stops a repeated webhook from applying
// an outcome twice.
func (s *Service) WithSettlement(checker SettlementChecker) *Service {
	s.settlement = checker
	return s
}

// ProcessFonbnkEvent applies a verified Fonbnk order webhook. Only
// payout_successful and refund_successful are final; the USDC was deposited
// by the treasury, so a completed payout owes no vault repay and a refund
// returns it to the treasury.
func (s *Service) ProcessFonbnkEvent(ctx context.Context, event fonbnk.WebhookEvent) error {
	order := event.Data.Order
	seqID := order.MerchantOrderParams
	ctx = logging.With(ctx, slog.String(pkgErrors.AttrSequenceID, seqID), slog.String(pkgErrors.AttrOrderID, order.ID))

	if order.Type != fonbnk.OrderTypeOffRamp || seqID == "" {
		slog.DebugContext(ctx, "fonbnk webhook ignored", slog.String("type", order.Type))
		return nil
	}

	switch order.Status {
	case fonbnk.StatusPayoutSuccessful:
		if s.alreadySettled(ctx, seqID) {
			return nil
		}
		if err := s.disbursements.UpdateDisbursementStatus(ctx, seqID, yellowcard.DisbursementComplete); err != nil {
			return webhookErr("update_status").With("target_status", "complete").
				Code(pkgErrors.CodeStateWriteFailed).Wrapf(err, "could not write the disbursement status")
		}
		fin := contracts.CompletionFinancials{ConvertedAmountLocal: order.Payout.Cashout.AmountAfterFees}
		if err := s.disbursements.RecordDisbursementCompletion(ctx, seqID, fin); err != nil {
			slog.ErrorContext(ctx, "fonbnk webhook: failed to record completion financials", slog.Any("error", err))
		}
		if err := s.disbursements.NotifyDisbursementComplete(ctx, seqID); err != nil {
			slog.ErrorContext(ctx, "fonbnk webhook: failed to notify completion", slog.Any("error", err))
		}

	case fonbnk.StatusPayoutFailed, fonbnk.StatusRefundInitiated, fonbnk.StatusRefundPending:
		if err := s.disbursements.UpdateDisbursementStatus(ctx, seqID, yellowcard.DisbursementRefundPending); err != nil {
			return webhookErr("update_status").With("target_status", "refund_pending").
				Code(pkgErrors.CodeStateWriteFailed).Wrapf(err, "could not write the disbursement status")
		}

	case fonbnk.StatusRefundSuccessful:
		if s.alreadySettled(ctx, seqID) {
			return nil
		}
		if err := s.disbursements.UpdateDisbursementStatus(ctx, seqID, yellowcard.DisbursementFailed); err != nil {
			return webhookErr("update_status").With("target_status", "failed").
				Code(pkgErrors.CodeStateWriteFailed).Wrapf(err, "could not write the disbursement status")
		}
		if err := s.disbursements.NotifyDisbursementFailed(ctx, seqID); err != nil {
			slog.ErrorContext(ctx, "fonbnk webhook: failed to notify failure", slog.Any("error", err))
		}
		s.alertOps(ctx, seqID, "Fonbnk Disbursement Refunded",
			fmt.Sprintf("Order %s payout failed and the USDC was refunded; the vault is repaid from treasury.", order.ID))

	case fonbnk.StatusRefundFailed:
		s.alertOps(ctx, seqID, "Fonbnk Refund Failed",
			fmt.Sprintf("Order %s payout failed and the refund failed. Manual intervention required.", order.ID))

	case fonbnk.StatusDepositInvalid:
		s.alertOps(ctx, seqID, "Fonbnk Deposit Invalid",
			fmt.Sprintf("Order %s rejected the treasury deposit. Check the order in the Fonbnk dashboard.", order.ID))

	default:
		slog.DebugContext(ctx, "fonbnk webhook: non-final status", slog.String("status", order.Status))
	}
	return nil
}

// alreadySettled reports whether the outcome was applied before. A failed
// lookup proceeds: the status write is idempotent, a duplicate SMS is the
// lesser harm than a dropped outcome.
func (s *Service) alreadySettled(ctx context.Context, seqID string) bool {
	if s.settlement == nil {
		return false
	}
	settled, err := s.settlement.DisbursementSettled(ctx, seqID)
	if err != nil {
		slog.WarnContext(ctx, "could not read the disbursement outcome; applying the event", slog.Any("error", err))
		return false
	}
	if settled {
		slog.InfoContext(ctx, "disbursement outcome already applied")
	}
	return settled
}
