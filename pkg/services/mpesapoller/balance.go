package mpesapoller

import (
	"context"
	"log/slog"
	"time"

	"github.com/Shamba-Records-Limited/microvault/pkg/payment/mpesa"
	"github.com/Shamba-Records-Limited/microvault/pkg/repository"
)

// balanceQuerier is the part of the client BalancePoller needs, so tests can
// stand in for Daraja without HTTP.
type balanceQuerier interface {
	AccountBalance(ctx context.Context, req mpesa.AccountBalanceRequest) (*mpesa.AsyncAck, error)
}

// BalancePoller asks Daraja for the collection and disbursement shortcodes'
// balances on a cadence. The figures land later, on the async callback route
// (DarajaCallbackController.async), which resolves the shortcode through the
// correlation this poller writes at request time — Account Balance is async,
// so nothing here ever sees the figure it asked for.
type BalancePoller struct {
	client                balanceQuerier
	queries               repository.MpesaBalanceRepository
	collectionShortcode   uint
	disbursementShortcode uint
	interval              time.Duration
	logger                *slog.Logger
}

// BalancePollerDeps are the collaborators BalancePoller needs; all required
// except Logger and DisbursementShortcode (zero skips that query).
type BalancePollerDeps struct {
	Client                balanceQuerier
	Queries               repository.MpesaBalanceRepository
	CollectionShortcode   uint
	DisbursementShortcode uint
	Interval              time.Duration
	Logger                *slog.Logger
}

// NewBalancePoller builds the poller.
func NewBalancePoller(deps BalancePollerDeps) *BalancePoller {
	logger := deps.Logger
	if logger == nil {
		logger = slog.Default()
	}
	return &BalancePoller{
		client:                deps.Client,
		queries:               deps.Queries,
		collectionShortcode:   deps.CollectionShortcode,
		disbursementShortcode: deps.DisbursementShortcode,
		interval:              deps.Interval,
		logger:                logger.With("component", "mpesa_balance_poller"),
	}
}

// Start runs the poller until ctx is cancelled.
func (p *BalancePoller) Start(ctx context.Context) {
	ticker := time.NewTicker(p.interval)
	defer ticker.Stop()

	p.logger.Info("starting", "interval", p.interval)
	p.tick(ctx)

	for {
		select {
		case <-ctx.Done():
			p.logger.Info("shutting down")
			return
		case <-ticker.C:
			p.tick(ctx)
		}
	}
}

func (p *BalancePoller) tick(ctx context.Context) {
	p.query(ctx, p.collectionShortcode)
	if p.disbursementShortcode != 0 && p.disbursementShortcode != p.collectionShortcode {
		p.query(ctx, p.disbursementShortcode)
	}
}

func (p *BalancePoller) query(ctx context.Context, shortcode uint) {
	if shortcode == 0 {
		return
	}
	ack, err := p.client.AccountBalance(ctx, mpesa.AccountBalanceRequest{PartyA: shortcode})
	if err != nil {
		p.logger.Error("account balance request failed", "shortcode", shortcode, "error", err)
		return
	}
	if !ack.Accepted() {
		p.logger.Warn("account balance request declined", "shortcode", shortcode, "response", ack.ResponseDescription)
		return
	}
	if err := p.queries.RecordQuery(ctx, ack.OriginatorConversationID, shortcode); err != nil {
		p.logger.Error("could not record balance query correlation", "shortcode", shortcode, "error", err)
	}
}
