package mpesapoller

import (
	"context"
	"log/slog"
	"time"

	"github.com/Shamba-Records-Limited/microvault/pkg/config"
	"github.com/Shamba-Records-Limited/microvault/pkg/models"
	"github.com/Shamba-Records-Limited/microvault/pkg/payment/mpesa"
	"github.com/Shamba-Records-Limited/microvault/pkg/repository"
	"github.com/Shamba-Records-Limited/microvault/pkg/services/mgpoller"
)

// expressQuerier is the part of the express client the driver uses, so tests
// can stand in for Daraja without HTTP.
type expressQuerier interface {
	ExpressQuery(ctx context.Context, checkoutRequestID string, shortcode uint) (*mpesa.ExpressQueryResponse, error)
}

// stkDriver resolves STK observations with an express query.
type stkDriver struct {
	repo    repository.MpesaTransactionRepository
	client  expressQuerier
	now     func() time.Time
	backoff time.Duration
	logger  *slog.Logger
}

// NewSTKDriver pairs the repository and the express client.
func NewSTKDriver(repo repository.MpesaTransactionRepository, client *mpesa.Client, interval time.Duration, logger *slog.Logger) mgpoller.Driver[models.MpesaTransaction] {
	if logger == nil {
		logger = slog.Default()
	}
	return &stkDriver{repo: repo, client: client, now: time.Now, backoff: interval, logger: logger}
}

// Drive processes one observation.
//
// A pending checkout errors rather than resolving; read the error as
// "not yet" and park it, never as a failure. A terminal failure is never read
// from the callback alone — the callback only put the receipt on the queue.
func (d *stkDriver) Drive(ctx context.Context, tx models.MpesaTransaction) {
	if tx.Source != models.MpesaSourceSTKCallback {
		d.park(ctx, tx.TransID)
		return
	}
	if tx.CheckoutRequestID == nil || *tx.CheckoutRequestID == "" {
		d.park(ctx, tx.TransID)
		return
	}

	resp, err := d.client.ExpressQuery(ctx, *tx.CheckoutRequestID, 0)
	if err != nil {
		// Daraja errors rather than reporting a pending checkout, so an error is
		// "not yet resolved", never terminal.
		d.park(ctx, tx.TransID)
		return
	}

	code, outcome := resp.Outcome()
	switch {
	case code == 0:
		if err := d.repo.Confirm(ctx, tx.TransID, models.MpesaConfirmViaSTKQuery, ""); err != nil {
			d.logger.Error("confirm stk", "trans_id", tx.TransID, "error", err)
			d.park(ctx, tx.TransID)
		}
	case outcome.Retryable:
		// A retryable failure — wrong PIN, cancelled, unreachable — is asked
		// again later, because the state can still change or the query may
		// simply be too early.
		d.park(ctx, tx.TransID)
	default:
		// An operational, non-retryable failure is terminal. Stop asking Daraja
		// about it and surface it: re-poling a dead prompt forever would spend
		// a query per tick for nothing.
		if err := d.repo.StopPoll(ctx, tx.TransID); err != nil {
			d.logger.Error("stop poll", "trans_id", tx.TransID, "error", err)
		}
		d.logger.Error("stk terminal failure", "trans_id", tx.TransID,
			"checkout_request_id", *tx.CheckoutRequestID, "result", code, "outcome", outcome.Message)
	}
}

// park reschedules a pending observation for one more interval.
func (d *stkDriver) park(ctx context.Context, transID string) {
	if err := d.repo.UpdatePoll(ctx, transID, d.now().Add(d.backoff)); err != nil {
		d.logger.Error("park observation", "trans_id", transID, "error", err)
	}
}

// NewSTKRunner pairs the poller cadence, fetcher and driver.
func NewSTKRunner(repo repository.MpesaTransactionRepository, client *mpesa.Client, cfg config.MpesaConfig, logger *slog.Logger) *mgpoller.Runner[models.MpesaTransaction] {
	if logger == nil {
		logger = slog.Default()
	}
	return mgpoller.NewRunner[models.MpesaTransaction](mgpoller.RunnerDeps[models.MpesaTransaction]{
		Direction: "mpesa-stk",
		Interval:  cfg.STKPollInterval,
		MaxBatch:  100,
		Fetcher: mgpoller.FetchFunc[models.MpesaTransaction](func(ctx context.Context, limit int) ([]models.MpesaTransaction, error) {
			due, err := repo.DuePoll(ctx, limit)
			if err != nil {
				return nil, err
			}
			out := make([]models.MpesaTransaction, 0, len(due))
			for _, tx := range due {
				out = append(out, *tx)
			}
			return out, nil
		}),
		Driver: NewSTKDriver(repo, client, cfg.STKPollInterval, logger),
		Logger: logger,
	})
}
