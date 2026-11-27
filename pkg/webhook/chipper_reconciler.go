package webhook

import (
	"context"
	"log/slog"
	"time"
)

// ChipperReconcilerConfig configures the Chipper backstop.
type ChipperReconcilerConfig struct {
	Interval    time.Duration
	InFlightAge time.Duration
	BatchSize   int
}

// DefaultChipperReconcilerConfig polls every 30 s and re-reads payouts with
// no outcome after 10 min.
func DefaultChipperReconcilerConfig() ChipperReconcilerConfig {
	return ChipperReconcilerConfig{Interval: 30 * time.Second, InFlightAge: 10 * time.Minute, BatchSize: 50}
}

// ChipperReconciler finishes what webhooks missed: it releases payouts left
// pending after a deposit, and applies outcomes of payouts whose webhook
// never arrived.
type ChipperReconciler struct {
	svc *Service
	cfg ChipperReconcilerConfig
	now func() time.Time
}

// NewChipperReconciler builds a reconciler over a Service configured with
// WithChipper.
func NewChipperReconciler(svc *Service, cfg ChipperReconcilerConfig) *ChipperReconciler {
	def := DefaultChipperReconcilerConfig()
	if cfg.Interval <= 0 {
		cfg.Interval = def.Interval
	}
	if cfg.InFlightAge <= 0 {
		cfg.InFlightAge = def.InFlightAge
	}
	if cfg.BatchSize <= 0 {
		cfg.BatchSize = def.BatchSize
	}
	return &ChipperReconciler{svc: svc, cfg: cfg, now: time.Now}
}

// Run reconciles until ctx is cancelled.
func (r *ChipperReconciler) Run(ctx context.Context) {
	ticker := time.NewTicker(r.cfg.Interval)
	defer ticker.Stop()
	for {
		r.Tick(ctx)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// Tick runs one reconciliation pass.
func (r *ChipperReconciler) Tick(ctx context.Context) {
	if r.svc.chipperPayouts == nil || r.svc.chipperStore == nil {
		return
	}
	r.releasePending(ctx)
	r.settleInFlight(ctx)
}

func (r *ChipperReconciler) releasePending(ctx context.Context) {
	pending, err := r.svc.chipperStore.PendingChipperReleases(ctx, r.cfg.BatchSize)
	if err != nil {
		slog.ErrorContext(ctx, "chipper reconciler: could not list pending payouts", slog.Any("error", err))
		return
	}
	for _, rel := range pending {
		if err := r.svc.release(ctx, rel); err != nil {
			slog.WarnContext(ctx, "chipper reconciler: pending payout not released yet",
				slog.String("loan_id", rel.LoanID), slog.Any("error", err))
		}
	}
}

func (r *ChipperReconciler) settleInFlight(ctx context.Context) {
	inFlight, err := r.svc.chipperStore.InFlightChipperPayouts(ctx, r.now().Add(-r.cfg.InFlightAge), r.cfg.BatchSize)
	if err != nil {
		slog.ErrorContext(ctx, "chipper reconciler: could not list in-flight payouts", slog.Any("error", err))
		return
	}
	for _, f := range inFlight {
		payout, err := r.svc.chipperPayouts.Payout(ctx, f.PayoutID)
		if err != nil {
			slog.WarnContext(ctx, "chipper reconciler: could not read payout",
				slog.String("loan_id", f.LoanID), slog.String("payout_id", f.PayoutID), slog.Any("error", err))
			continue
		}
		if err := r.svc.applyChipperPayout(ctx, payout); err != nil {
			slog.ErrorContext(ctx, "chipper reconciler: could not apply payout outcome",
				slog.String("loan_id", f.LoanID), slog.String("payout_id", f.PayoutID), slog.Any("error", err))
		}
	}
}
