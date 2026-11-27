package chippermon

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/govalues/decimal"

	"github.com/Shamba-Records-Limited/microvault/pkg/alerts"
	"github.com/Shamba-Records-Limited/microvault/pkg/payment/chipper"
)

// LedgerReader is the Chipper ledger surface the reconciler reads.
type LedgerReader interface {
	ListLedger(ctx context.Context, f chipper.LedgerFilter) (*chipper.Page[chipper.LedgerEntry], error)
}

// SettledPayout is a Chipper payout whose fee was recorded on its loan, in
// US cents.
type SettledPayout struct {
	LoanID          string
	LoanReference   string
	PayoutID        string
	ServiceFeeCents int64
}

// FeeStore lists the payouts to reconcile.
type FeeStore interface {
	SettledChipperPayouts(ctx context.Context, since time.Time, limit int) ([]SettledPayout, error)
}

// FeeReconcilerConfig sets the pacing. Window is how far back each run looks;
// keep it above Interval so no payout is skipped.
type FeeReconcilerConfig struct {
	Interval  time.Duration
	Window    time.Duration
	BatchSize int
}

// FeeReconciler compares each loan's recorded service fee with the fee
// Chipper debited for its payout, and alerts on any difference beyond a cent.
type FeeReconciler struct {
	ledger LedgerReader
	store  FeeStore
	alerts alerts.Service
	logger *slog.Logger
	cfg    FeeReconcilerConfig
	now    func() time.Time
}

// Mismatch is one payout whose recorded fee disagrees with Chipper's ledger.
type Mismatch struct {
	SettledPayout
	LedgerFee decimal.Decimal
}

// NewFeeReconciler builds a reconciler; Interval defaults to 24 h, Window to
// Interval plus an hour, BatchSize to 200.
func NewFeeReconciler(ledger LedgerReader, store FeeStore, svc alerts.Service, logger *slog.Logger, cfg FeeReconcilerConfig) *FeeReconciler {
	if cfg.Interval <= 0 {
		cfg.Interval = 24 * time.Hour
	}
	if cfg.Window <= cfg.Interval {
		cfg.Window = cfg.Interval + time.Hour
	}
	if cfg.BatchSize <= 0 {
		cfg.BatchSize = 200
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &FeeReconciler{ledger: ledger, store: store, alerts: svc, logger: logger.With("component", "chipper_fee_reconciler"), cfg: cfg, now: time.Now}
}

// Run reconciles until ctx is cancelled.
func (r *FeeReconciler) Run(ctx context.Context) {
	r.logger.InfoContext(ctx, "chipper fee reconciler started", "interval", r.cfg.Interval.String())
	ticker := time.NewTicker(r.cfg.Interval)
	defer ticker.Stop()
	for {
		if _, err := r.Reconcile(ctx); err != nil {
			r.logger.ErrorContext(ctx, "chipper fee reconciliation failed", "error", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// Reconcile checks the payouts settled within the window once and alerts on
// mismatches.
func (r *FeeReconciler) Reconcile(ctx context.Context) ([]Mismatch, error) {
	payouts, err := r.store.SettledChipperPayouts(ctx, r.now().Add(-r.cfg.Window), r.cfg.BatchSize)
	if err != nil {
		return nil, err
	}

	var (
		mismatches []Mismatch
		unread     int
	)
	for _, p := range payouts {
		fee, err := r.ledgerFee(ctx, p.PayoutID)
		if err != nil {
			unread++
			r.logger.WarnContext(ctx, "chipper fee ledger unreadable", "loan_id", p.LoanID, "payout_id", p.PayoutID, "error", err)
			continue
		}
		recorded := decimal.MustNew(p.ServiceFeeCents, 2)
		diff, err := fee.Sub(recorded)
		if err != nil {
			continue
		}
		if diff.Abs().Cmp(oneCent) > 0 {
			mismatches = append(mismatches, Mismatch{SettledPayout: p, LedgerFee: fee})
		}
	}

	r.logger.InfoContext(ctx, "chipper fees reconciled", "payouts", len(payouts), "mismatches", len(mismatches), "unread", unread)
	if len(mismatches) > 0 {
		alerts.Raise(ctx, r.alerts, r.logger, SubjectFeeMismatch, mismatchMessage(mismatches))
	}
	return mismatches, nil
}

var oneCent = decimal.MustNew(1, 2)

// ledgerFee sums the fee debits, less any fee refunds, booked against a
// payout.
func (r *FeeReconciler) ledgerFee(ctx context.Context, payoutID string) (decimal.Decimal, error) {
	page, err := r.ledger.ListLedger(ctx, chipper.LedgerFilter{ReferenceID: payoutID, Limit: 100})
	if err != nil {
		return decimal.Zero, err
	}
	total := decimal.Zero
	for _, e := range page.Data {
		switch e.Type {
		case chipper.LedgerFeeDebit:
			total, err = total.Add(e.Amount.Abs())
		case chipper.LedgerFeeRefund:
			total, err = total.Sub(e.Amount.Abs())
		}
		if err != nil {
			return decimal.Zero, err
		}
	}
	return total, nil
}

func mismatchMessage(ms []Mismatch) string {
	lines := make([]string, 0, len(ms))
	for _, m := range ms {
		lines = append(lines, fmt.Sprintf("%s (payout %s): recorded %s, Chipper %s",
			m.LoanReference, m.PayoutID, decimal.MustNew(m.ServiceFeeCents, 2).String(), m.LedgerFee.String()))
	}
	return fmt.Sprintf("%d Chipper payout fee(s) disagree with the loan's service fee by more than a cent; the borrower repays the recorded figure:\n%s",
		len(ms), strings.Join(lines, "\n"))
}
