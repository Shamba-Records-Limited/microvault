package webhook

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/govalues/decimal"

	"github.com/Shamba-Records-Limited/microvault/pkg/contracts"
	"github.com/Shamba-Records-Limited/microvault/pkg/logging"
	"github.com/Shamba-Records-Limited/microvault/pkg/payment/chipper"
	"github.com/Shamba-Records-Limited/microvault/pkg/payment/offramp"
	"github.com/Shamba-Records-Limited/microvault/pkg/payment/yellowcard"

	pkgErrors "github.com/Shamba-Records-Limited/microvault/pkg/errors"
)

// chipperRetrySuffix marks the payout repeated from the Chipper balance
// after the first one failed. The loan lookup resolves it to the original
// sequence id.
const chipperRetrySuffix = "_fiat"

const pendingReleaseLimit = 100

// SettlementChecker reports whether a disbursement already reached a
// terminal outcome, so a repeated webhook does not notify twice.
type SettlementChecker interface {
	DisbursementSettled(ctx context.Context, sequenceID string) (bool, error)
}

// ChipperPayouts is the Chipper adapter surface the webhook path needs.
type ChipperPayouts interface {
	ReleasePayout(ctx context.Context, rel chipper.Release) (*offramp.Result, error)
	ReissuePayout(ctx context.Context, failed *chipper.Payout, reference string) (*chipper.Payout, error)
	Payout(ctx context.Context, id string) (*chipper.Payout, error)
}

// ChipperInFlight is a Chipper loan with a payout that has no final outcome.
type ChipperInFlight struct {
	SequenceID string
	LoanID     string
	PayoutID   string
}

// ChipperStore reads and records Chipper disbursement state.
type ChipperStore interface {
	SettlementChecker
	// PendingChipperReleases lists loans whose deposit reached Chipper but
	// whose payout was never created.
	PendingChipperReleases(ctx context.Context, limit int) ([]chipper.Release, error)
	// InFlightChipperPayouts lists payouts created before cutoff that have no
	// final outcome yet.
	InFlightChipperPayouts(ctx context.Context, cutoff time.Time, limit int) ([]ChipperInFlight, error)
	// RecordChipperPayout points the loan at the payout now carrying it.
	RecordChipperPayout(ctx context.Context, sequenceID, payoutID string) error
	// IsDirectSettlement reports whether the treasury deposited the USDC.
	IsDirectSettlement(ctx context.Context, sequenceID string) (bool, error)
}

// WithChipper enables Chipper event handling.
func (s *Service) WithChipper(payouts ChipperPayouts, store ChipperStore) *Service {
	s.chipperPayouts, s.chipperStore = payouts, store
	return s
}

// ProcessChipperEvent applies a verified Chipper webhook.
func (s *Service) ProcessChipperEvent(ctx context.Context, event chipper.Event) error {
	errb := webhookErr("chipper_event").With("event", event.Event)
	if s.chipperPayouts == nil || s.chipperStore == nil {
		return errb.Code(pkgErrors.CodeMissingDependency).Errorf("chipper handling is not configured")
	}

	switch event.Event {
	case chipper.EventPayoutCompleted, chipper.EventPayoutFailed:
		payout, err := event.Payout()
		if err != nil {
			return err
		}
		return s.applyChipperPayout(ctx, payout)
	case chipper.EventCollectionCompleted, chipper.EventAccountCredited:
		return s.releaseForDeposit(ctx, event)
	case chipper.EventWebhookTest:
		slog.InfoContext(ctx, "chipper webhook test received")
	default:
		slog.DebugContext(ctx, "chipper webhook ignored", slog.String("event", event.Event))
	}
	return nil
}

// applyChipperPayout settles a payout that reached completed or failed.
func (s *Service) applyChipperPayout(ctx context.Context, payout *chipper.Payout) error {
	seqID := payout.ExternalReference
	ctx = logging.With(ctx, slog.String(pkgErrors.AttrSequenceID, seqID), slog.String("payout_id", payout.ID))

	if !payout.IsTerminal() {
		return nil
	}
	settled, err := s.chipperStore.DisbursementSettled(ctx, seqID)
	if err != nil {
		return webhookErr("chipper_settled").With(pkgErrors.AttrSequenceID, seqID).
			Code(pkgErrors.CodeLoanLoadFailed).Wrapf(err, "could not read the disbursement outcome")
	}
	if settled {
		slog.InfoContext(ctx, "chipper payout outcome already applied", slog.String("status", payout.Status))
		return nil
	}

	if err := s.chipperStore.RecordChipperPayout(ctx, seqID, payout.ID); err != nil {
		slog.WarnContext(ctx, "could not record the chipper payout id", slog.Any("error", err))
	}

	if payout.Status == chipper.PayoutCompleted {
		if err := s.disbursements.UpdateDisbursementStatus(ctx, seqID, yellowcard.DisbursementComplete); err != nil {
			return webhookErr("update_status").With("target_status", "complete").
				Code(pkgErrors.CodeStateWriteFailed).Wrapf(err, "could not write the disbursement status")
		}
		if err := s.disbursements.RecordDisbursementCompletion(ctx, seqID, chipperFinancials(payout)); err != nil {
			slog.ErrorContext(ctx, "chipper webhook: failed to record completion financials", slog.Any("error", err))
		}
		if err := s.disbursements.NotifyDisbursementComplete(ctx, seqID); err != nil {
			slog.ErrorContext(ctx, "chipper webhook: failed to notify completion", slog.Any("error", err))
		}
		return nil
	}
	return s.handleChipperFailure(ctx, payout)
}

// handleChipperFailure repeats a failed direct payout once from the Chipper
// balance, where the deposit still sits. A failed repeat, or a failure of a
// payout the balance funded from the start, fails the disbursement.
func (s *Service) handleChipperFailure(ctx context.Context, payout *chipper.Payout) error {
	seqID := payout.ExternalReference
	reason := reasonOf(payout.StatusMessage)

	direct, err := s.chipperStore.IsDirectSettlement(ctx, seqID)
	if err != nil {
		return webhookErr("chipper_settlement_method").With(pkgErrors.AttrSequenceID, seqID).
			Code(pkgErrors.CodeLoanLoadFailed).Wrapf(err, "could not look up the settlement method")
	}

	if direct && !strings.HasSuffix(seqID, chipperRetrySuffix) {
		retry, err := s.chipperPayouts.ReissuePayout(ctx, payout, seqID+chipperRetrySuffix)
		if err == nil {
			if err := s.chipperStore.RecordChipperPayout(ctx, seqID, retry.ID); err != nil {
				slog.WarnContext(ctx, "could not record the repeated chipper payout id", slog.Any("error", err))
			}
			s.alertOps(ctx, seqID, "Chipper payout repeated",
				fmt.Sprintf("Payout %s failed (%s); repeated from the Chipper balance as %s.", payout.ID, reason, retry.ID))
			return nil
		}
		slog.ErrorContext(ctx, "chipper payout could not be repeated", slog.Any("error", err))
	}

	if err := s.disbursements.UpdateDisbursementStatus(ctx, seqID, yellowcard.DisbursementFailed); err != nil {
		return webhookErr("update_status").With("target_status", "failed").
			Code(pkgErrors.CodeStateWriteFailed).Wrapf(err, "could not write the disbursement status")
	}
	if err := s.disbursements.NotifyDisbursementFailed(ctx, seqID); err != nil {
		slog.ErrorContext(ctx, "chipper webhook: failed to notify failure", slog.Any("error", err))
	}
	message := fmt.Sprintf("Payout %s failed (%s). The disbursement is marked failed and the vault repaid from treasury.", payout.ID, reason)
	if direct {
		message += " The deposited USDC remains on the Chipper balance."
	}
	s.alertOps(ctx, seqID, "Chipper Disbursement Failed", message)
	return nil
}

// releaseForDeposit finishes the pending payout funded by a crypto deposit.
func (s *Service) releaseForDeposit(ctx context.Context, event chipper.Event) error {
	var col chipper.Collection
	if err := decodeData(event, &col); err != nil {
		return err
	}
	details, ok := col.Crypto()
	if !ok || col.Status != chipper.CollectionCompleted {
		return nil
	}

	pending, err := s.chipperStore.PendingChipperReleases(ctx, pendingReleaseLimit)
	if err != nil {
		return webhookErr("chipper_pending").Code(pkgErrors.CodeLoanLoadFailed).Wrapf(err, "could not list pending chipper payouts")
	}
	for _, rel := range pending {
		if strings.EqualFold(rel.DepositTxHash, details.Hash) {
			return s.release(ctx, rel)
		}
	}
	return nil
}

func (s *Service) release(ctx context.Context, rel chipper.Release) error {
	ctx = logging.WithLoan(ctx, rel.LoanID, "")
	res, err := s.chipperPayouts.ReleasePayout(ctx, rel)
	if err != nil {
		return webhookErr("chipper_release").With(pkgErrors.AttrLoanID, rel.LoanID).With(pkgErrors.AttrTxHash, rel.DepositTxHash).
			Wrapf(err, "could not release the pending chipper payout")
	}
	if err := s.chipperStore.RecordChipperPayout(ctx, rel.Reference, res.RequestID); err != nil {
		slog.WarnContext(ctx, "could not record the released chipper payout id", slog.Any("error", err))
	}
	slog.InfoContext(ctx, "pending chipper payout released", slog.String("payout_id", res.RequestID))
	return nil
}

// chipperFinancials maps a completed payout onto completion financials. The
// fee was taken from our Chipper balance, so it is the service fee the
// borrower repays.
func chipperFinancials(p *chipper.Payout) contracts.CompletionFinancials {
	fin := contracts.CompletionFinancials{ConvertedAmountLocal: decimalFloat(p.To.Amount)}
	if p.Fee == nil {
		return fin
	}
	if !strings.EqualFold(p.Fee.Currency, "USDC") && !strings.EqualFold(p.Fee.Currency, "USD") {
		fin.ServiceFeeAmountLocal = decimalFloat(p.Fee.Amount)
		return fin
	}
	fin.ServiceFeeAmountUSD = decimalFloat(p.Fee.Amount)
	if p.Rate != nil {
		if local, err := p.Fee.Amount.Mul(*p.Rate); err == nil {
			fin.ServiceFeeAmountLocal = decimalFloat(local)
		}
	}
	return fin
}

func decodeData(event chipper.Event, out any) error {
	if err := json.Unmarshal(event.Data, out); err != nil {
		return webhookErr("chipper_decode").With("event", event.Event).
			Code(pkgErrors.CodeDecodeFailed).Wrapf(err, "could not decode the event resource")
	}
	return nil
}

func decimalFloat(d decimal.Decimal) float64 {
	f, _ := d.Float64()
	return f
}

func reasonOf(s *string) string {
	if s == nil || *s == "" {
		return "no reason given"
	}
	return *s
}
