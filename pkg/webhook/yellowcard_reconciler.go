package webhook

import (
	"context"
	"log/slog"
	"time"

	"github.com/Shamba-Records-Limited/microvault/pkg/payment/yellowcard"
)

// YellowCardInFlight is a YellowCard payout with no final outcome yet.
type YellowCardInFlight struct {
	SequenceID string
	LoanID     string
	PaymentID  string
}

// YellowCardStore lists YellowCard payouts awaiting an outcome.
type YellowCardStore interface {
	// InFlightYellowCardPayouts lists payouts last updated before cutoff that
	// have no final outcome.
	InFlightYellowCardPayouts(ctx context.Context, cutoff time.Time, limit int) ([]YellowCardInFlight, error)
}

// WithYellowCardStore enables the YellowCard reconciler.
func (s *Service) WithYellowCardStore(store YellowCardStore) *Service {
	s.yellowCardStore = store
	return s
}

// YellowCardReconcilerConfig configures the YellowCard backstop.
type YellowCardReconcilerConfig struct {
	Interval    time.Duration
	InFlightAge time.Duration
	BatchSize   int
}

// DefaultYellowCardReconcilerConfig polls every minute and re-reads payouts
// with no outcome after 15 min.
func DefaultYellowCardReconcilerConfig() YellowCardReconcilerConfig {
	return YellowCardReconcilerConfig{Interval: time.Minute, InFlightAge: 15 * time.Minute, BatchSize: 50}
}

// YellowCardReconciler applies the outcome of YellowCard payouts whose
// webhook never arrived. It reads each payout back and applies only final
// outcomes, through the same path as the webhook; payouts still in progress
// are left for the webhook or the next pass, and refunds stay with the
// RefundPoller.
type YellowCardReconciler struct {
	svc *Service
	cfg YellowCardReconcilerConfig
	now func() time.Time
}

// NewYellowCardReconciler builds a reconciler over a Service configured with
// a payment lookup, WithYellowCardStore and WithSettlement.
func NewYellowCardReconciler(svc *Service, cfg YellowCardReconcilerConfig) *YellowCardReconciler {
	def := DefaultYellowCardReconcilerConfig()
	if cfg.Interval <= 0 {
		cfg.Interval = def.Interval
	}
	if cfg.InFlightAge <= 0 {
		cfg.InFlightAge = def.InFlightAge
	}
	if cfg.BatchSize <= 0 {
		cfg.BatchSize = def.BatchSize
	}
	return &YellowCardReconciler{svc: svc, cfg: cfg, now: time.Now}
}

// Run reconciles until ctx is cancelled.
func (r *YellowCardReconciler) Run(ctx context.Context) {
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
func (r *YellowCardReconciler) Tick(ctx context.Context) {
	if r.svc.payments == nil || r.svc.yellowCardStore == nil {
		return
	}
	inFlight, err := r.svc.yellowCardStore.InFlightYellowCardPayouts(ctx, r.now().Add(-r.cfg.InFlightAge), r.cfg.BatchSize)
	if err != nil {
		slog.ErrorContext(ctx, "yellowcard reconciler: could not list in-flight payouts", slog.Any("error", err))
		return
	}
	for _, f := range inFlight {
		if ctx.Err() != nil {
			return
		}
		r.reconcile(ctx, f)
	}
}

func (r *YellowCardReconciler) reconcile(ctx context.Context, f YellowCardInFlight) {
	details, err := r.svc.payments.LookupPayment(ctx, f.PaymentID)
	if err != nil {
		slog.WarnContext(ctx, "yellowcard reconciler: could not read payout",
			slog.String("loan_id", f.LoanID), slog.String("payment_id", f.PaymentID), slog.Any("error", err))
		return
	}
	if !finalPayoutStatus(details.Status) {
		return
	}
	seqID := f.SequenceID
	if seqID == "" {
		seqID = details.SequenceID
	}
	slog.InfoContext(ctx, "yellowcard reconciler: applying an outcome the webhook did not deliver",
		slog.String("loan_id", f.LoanID), slog.String("payment_id", f.PaymentID), slog.String("status", details.Status))
	event := yellowcard.WebhookEvent{PaymentID: f.PaymentID, SequenceID: seqID, Status: details.Status}
	if err := r.svc.ProcessYellowCardEvent(ctx, event); err != nil {
		slog.ErrorContext(ctx, "yellowcard reconciler: could not apply payout outcome",
			slog.String("loan_id", f.LoanID), slog.String("payment_id", f.PaymentID), slog.Any("error", err))
	}
}

// finalPayoutStatus reports a send status the reconciler may apply.
func finalPayoutStatus(status string) bool {
	switch status {
	case yellowcard.StatusComplete, yellowcard.StatusFailed, yellowcard.StatusExpired, yellowcard.StatusCancelled:
		return true
	}
	return false
}
