package mpesapoller

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/Shamba-Records-Limited/microvault/pkg/models"
	"github.com/Shamba-Records-Limited/microvault/pkg/payment/mpesa"
	"github.com/Shamba-Records-Limited/microvault/pkg/repository"
)

// pullQuerier is the part of the client PullSweeper needs, so tests can stand
// in for Daraja without HTTP.
type pullQuerier interface {
	PullAll(ctx context.Context, from, to time.Time, shortcode uint) ([]mpesa.PulledTransaction, error)
}

// PullSweeper reconciles Daraja's Pull API against mpesa_transactions on a
// wall-clock cadence. See doc.go for why this is a ticker rather than an
// mgpoller.Runner[T].
type PullSweeper struct {
	client    pullQuerier
	repo      repository.MpesaTransactionRepository
	cursor    repository.MpesaPullCursorRepository
	shortcode uint
	interval  time.Duration
	logger    *slog.Logger
	now       func() time.Time
}

// PullSweeperDeps are the collaborators PullSweeper needs; all required
// except Logger.
type PullSweeperDeps struct {
	Client    pullQuerier
	Repo      repository.MpesaTransactionRepository
	Cursor    repository.MpesaPullCursorRepository
	Shortcode uint
	Interval  time.Duration
	Logger    *slog.Logger
}

// NewPullSweeper builds the sweeper.
func NewPullSweeper(deps PullSweeperDeps) *PullSweeper {
	logger := deps.Logger
	if logger == nil {
		logger = slog.Default()
	}
	return &PullSweeper{
		client:    deps.Client,
		repo:      deps.Repo,
		cursor:    deps.Cursor,
		shortcode: deps.Shortcode,
		interval:  deps.Interval,
		logger:    logger.With("component", "mpesa_pull_sweeper"),
		now:       time.Now,
	}
}

// Start runs the sweep until ctx is cancelled.
func (s *PullSweeper) Start(ctx context.Context) {
	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()

	s.logger.Info("starting", "interval", s.interval)
	s.sweep(ctx)

	for {
		select {
		case <-ctx.Done():
			s.logger.Info("shutting down")
			return
		case <-ticker.C:
			s.sweep(ctx)
		}
	}
}

// sweep walks one window: from the cursor to now. A failed window leaves the
// cursor where it was, so the same window is retried next tick instead of
// silently skipped — Daraja's 48-hour retention gives plenty of room for a
// few missed ticks to catch up.
func (s *PullSweeper) sweep(ctx context.Context) {
	from, err := s.cursor.Get(ctx)
	if err != nil {
		s.logger.Error("could not read the pull cursor", "error", err)
		return
	}
	to := s.now()
	if !to.After(from) {
		return
	}

	txs, err := s.client.PullAll(ctx, from, to, s.shortcode)
	if err != nil {
		s.logger.Error("pull query failed, window will be retried next tick",
			"from", from, "to", to, "error", err)
		return
	}

	for _, pt := range txs {
		s.reconcile(ctx, pt)
	}

	if err := s.cursor.Advance(ctx, to); err != nil {
		s.logger.Error("could not advance the pull cursor", "error", err)
	}
}

// reconcile writes one pulled transaction. Pull is itself the independent
// check §7 of the callback threat model requires — a transaction it reports
// is real by construction, whether or not its reference resolves to a loan.
// An unattributed row is an orphan for the reversal queue, not an
// unconfirmed one, so Confirmed is always true here.
func (s *PullSweeper) reconcile(ctx context.Context, pt mpesa.PulledTransaction) {
	loanID, err := s.repo.GetLoanIDByReference(ctx, pt.BillReference)
	if err != nil {
		s.logger.Warn("could not resolve loan reference during pull sweep",
			"trans_id", pt.TransactionID, "error", err)
	}

	payload, err := json.Marshal(pt)
	if err != nil {
		s.logger.Error("could not encode pulled transaction", "trans_id", pt.TransactionID, "error", err)
		return
	}

	via := models.MpesaConfirmViaPull
	msisdn := pt.MSISDN
	tx := &models.MpesaTransaction{
		TransID:       pt.TransactionID,
		Source:        models.MpesaSourcePull,
		Confirmed:     true,
		ConfirmedVia:  &via,
		BillRefNumber: pt.BillReference,
		AmountKes:     pt.AmountMinor,
		MsidnFull:     &msisdn,
		TransTime:     pt.CompletedAt,
		RawPayload:    payload,
	}
	if loanID != "" {
		tx.LoanID = &loanID
	}

	if err := s.repo.UpsertFromPull(ctx, tx); err != nil {
		s.logger.Error("could not upsert pulled transaction", "trans_id", pt.TransactionID, "error", err)
	}
}
