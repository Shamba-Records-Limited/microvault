package airtelpoller

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/samber/lo"
	"gorm.io/datatypes"

	"github.com/Shamba-Records-Limited/microvault/pkg/models"
	"github.com/Shamba-Records-Limited/microvault/pkg/payment/airtel"
	"github.com/Shamba-Records-Limited/microvault/pkg/repository"
)

// summaryQuerier is the part of the client SummarySweeper needs, so tests can
// stand in for Airtel without HTTP.
type summaryQuerier interface {
	TransactionsSummary(ctx context.Context, req airtel.SummaryRequest) (*airtel.SummaryResponse, error)
}

// pageSize is how many entries one Summary request asks for. Airtel
// documents no ceiling, so this is conservative; the sweep pages until a
// short page tells it the window is exhausted.
const pageSize = 100

// maxPages bounds one sweep. A window that needs more than this is not
// walked to the end in a single tick — the cursor does not advance, and the
// next tick resumes the same window rather than skipping what it missed.
const maxPages = 50

// SummarySweeper reconciles Airtel's Transactions Summary against
// airtel_transactions on a wall-clock cadence.
type SummarySweeper struct {
	client   summaryQuerier
	repo     repository.AirtelTransactionRepository
	cursor   repository.AirtelSummaryCursorRepository
	interval time.Duration
	logger   *slog.Logger
	now      func() time.Time
}

// SummarySweeperDeps are the collaborators SummarySweeper needs; all required
// except Logger.
type SummarySweeperDeps struct {
	Client   summaryQuerier
	Repo     repository.AirtelTransactionRepository
	Cursor   repository.AirtelSummaryCursorRepository
	Interval time.Duration
	Logger   *slog.Logger
}

// NewSummarySweeper builds the sweeper.
func NewSummarySweeper(deps SummarySweeperDeps) *SummarySweeper {
	logger := deps.Logger
	if logger == nil {
		logger = slog.Default()
	}
	return &SummarySweeper{
		client:   deps.Client,
		repo:     deps.Repo,
		cursor:   deps.Cursor,
		interval: deps.Interval,
		logger:   logger.With("component", "airtel_summary_sweeper"),
		now:      time.Now,
	}
}

// Start runs the sweep until ctx is cancelled.
func (s *SummarySweeper) Start(ctx context.Context) {
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

// sweep walks one window, from the cursor to now.
//
// A failed window leaves the cursor where it was, so the next tick re-walks
// it. Re-walking is free: every write is an upsert keyed by the partner
// transaction id, so seeing the same settled payment twice updates one row
// rather than crediting twice.
func (s *SummarySweeper) sweep(ctx context.Context) {
	from, err := s.cursor.Get(ctx)
	if err != nil {
		s.logger.Warn("could not read the sweep cursor", "error", err)
		return
	}
	to := s.now()
	if !from.Before(to) {
		return
	}

	swept, err := s.walk(ctx, from, to)
	if err != nil {
		s.logger.Warn("sweep did not complete; the cursor stays put and the window will be re-walked",
			"from", from, "to", to, "recorded", swept, "error", err)
		return
	}

	if err := s.cursor.Advance(ctx, to); err != nil {
		s.logger.Warn("could not advance the sweep cursor; the window will be re-walked", "error", err)
		return
	}
	if swept > 0 {
		s.logger.Info("reconciled settled collections", "from", from, "to", to, "recorded", swept)
	}
}

// walk pages through the window, recording every settled entry.
func (s *SummarySweeper) walk(ctx context.Context, from, to time.Time) (int, error) {
	recorded := 0

	for page := range maxPages {
		resp, err := s.client.TransactionsSummary(ctx, airtel.SummaryRequest{
			From:   from,
			To:     to,
			Limit:  pageSize,
			Offset: page * pageSize,
		})
		if err != nil {
			return recorded, err
		}

		for _, entry := range resp.Settled() {
			if err := s.record(ctx, entry); err != nil {
				return recorded, err
			}
			recorded++
		}

		// A short page is the end of the window. Airtel exposes no total
		// beyond data.count, which counts the page rather than the window.
		if len(resp.Data.Transactions) < pageSize {
			return recorded, nil
		}
	}

	s.logger.Warn("sweep hit the page ceiling; the cursor will not advance and the window resumes next tick",
		"from", from, "to", to, "max_pages", maxPages)
	return recorded, nil
}

// record upserts one settled entry.
func (s *SummarySweeper) record(ctx context.Context, entry airtel.SummaryTransaction) error {
	minor, err := entry.AmountMinor()
	if err != nil {
		// A formatted amount that will not parse is not zero. Recording it
		// as zero would credit a loan nothing and mark the payment handled,
		// which is worse than leaving it for a human.
		s.logger.Warn("skipping a settled entry whose amount did not parse",
			"transaction_id", entry.Transaction.ID, "amount", entry.Transaction.Amount, "error", err)
		return nil
	}

	raw, err := json.Marshal(entry)
	if err != nil {
		return err
	}

	tx := models.AirtelTransaction{
		PartnerTxnID: entry.Transaction.ID,
		Source:       models.AirtelSourceSummary,
		StatusCode:   string(entry.TransactionStatus()),
		Confirmed:    true,
		ConfirmedVia: lo.ToPtr(models.AirtelConfirmViaSummary),
		Reference:    entry.Transaction.ReferenceNumber,
		AmountMinor:  minor,
		TransTime:    settledAt(entry, s.now()),
		RawPayload:   datatypes.JSON(raw),
	}
	if entry.Transaction.AirtelMoneyID != "" {
		tx.AirtelMoneyID = lo.ToPtr(entry.Transaction.AirtelMoneyID)
	}
	if entry.Payer.MSISDN != "" {
		tx.Msisdn = lo.ToPtr(entry.Payer.MSISDN)
	}

	return s.repo.UpsertFromSummary(ctx, &tx)
}

// settledAt reads the entry's own completion time, falling back to now when
// Airtel sends something unparseable rather than dropping the row.
func settledAt(entry airtel.SummaryTransaction, fallback time.Time) time.Time {
	parsed, err := time.Parse(time.RFC3339, entry.Transaction.CreatedAt)
	if err != nil {
		return fallback
	}
	return parsed
}
