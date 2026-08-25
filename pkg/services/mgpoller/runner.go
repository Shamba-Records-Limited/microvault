package mgpoller

import (
	"context"
	"log/slog"
	"time"

	"github.com/samber/lo"
	"github.com/samber/oops"

	pkgErrors "github.com/Shamba-Records-Limited/microvault/pkg/errors"
)

// This file holds the parts of the poller that do not care which direction
// money is moving. Both the withdrawal state machine (cash out, in
// withdrawal.go) and the deposit state machine (cash in) are the same shape at
// this level: wake on a cadence, ask for a batch of records that need
// attention, drive each one, never let a single record's failure abort the
// batch.

// errDomain is the oops domain for both poller directions.
const errDomain = pkgErrors.DomainMoneyGramPoller

// missingDep is the one error every constructor in this package returns for an
// absent collaborator. The dependency name is an attribute rather than part of
// the message, so all of them group as a single error in APM.
func missingDep(direction, dependency string) error {
	return oops.
		In(errDomain).
		Code(pkgErrors.CodeMissingDependency).
		With(pkgErrors.AttrDirection, direction).
		With(pkgErrors.AttrDependency, dependency).
		Errorf("required dependency is missing")
}

// dep pairs a dependency's name with whether it is absent, so constructors can
// validate a whole set in one pass instead of a ladder of if-statements.
type dep struct {
	name    string
	missing bool
}

// firstMissing reports the first absent dependency, if any.
func firstMissing(deps []dep) (dep, bool) {
	return lo.Find(deps, func(d dep) bool { return d.missing })
}

// Fetcher supplies one batch of records that need driving. Implementations
// decide what "needs driving" means — the withdrawal side selects loans with a
// live MoneyGram withdrawal, the deposit side selects rows whose next poll is
// due.
type Fetcher[T any] interface {
	Fetch(ctx context.Context, limit int) ([]T, error)
}

// FetchFunc adapts an ordinary function to Fetcher, so an existing
// repository method can be handed to a Runner without a wrapper type.
type FetchFunc[T any] func(ctx context.Context, limit int) ([]T, error)

// Fetch implements Fetcher.
func (f FetchFunc[T]) Fetch(ctx context.Context, limit int) ([]T, error) {
	return f(ctx, limit)
}

// Driver applies the state transition for a single record.
//
// Drive returns nothing on purpose. One record's failure must not abort the
// batch — every record gets its turn each tick — so a driver logs what went
// wrong and returns, rather than handing the runner an error it could only
// discard anyway.
type Driver[T any] interface {
	Drive(ctx context.Context, rec T)
}

// Runner ticks a Fetcher/Driver pair until its context is cancelled.
type Runner[T any] struct {
	direction string
	interval  time.Duration
	maxBatch  int
	fetcher   Fetcher[T]
	driver    Driver[T]
	logger    *slog.Logger
}

// RunnerDeps are the collaborators and settings a Runner needs. Logger is
// optional; everything else is required.
type RunnerDeps[T any] struct {
	// Direction names the runner in its logs, which is what tells two runners
	// in the same process apart.
	Direction string
	Interval  time.Duration
	MaxBatch  int
	Fetcher   Fetcher[T]
	Driver    Driver[T]
	Logger    *slog.Logger
}

// NewRunner pairs a fetcher and a driver on one cadence.
func NewRunner[T any](deps RunnerDeps[T]) *Runner[T] {
	direction, interval, maxBatch := deps.Direction, deps.Interval, deps.MaxBatch
	fetcher, driver := deps.Fetcher, deps.Driver

	logger := deps.Logger
	if logger == nil {
		logger = slog.Default()
	}
	return &Runner[T]{
		direction: direction,
		interval:  interval,
		maxBatch:  maxBatch,
		fetcher:   fetcher,
		driver:    driver,
		logger:    logger.With(pkgErrors.AttrDirection, direction),
	}
}

// Start runs the runner until ctx is cancelled. Mirrors RefundPoller.Start.
func (r *Runner[T]) Start(ctx context.Context) {
	ticker := time.NewTicker(r.interval)
	defer ticker.Stop()

	r.logger.Info("starting", "interval", r.interval, "max_batch", r.maxBatch)

	// Run once immediately for boot-time resume so we don't wait one full
	// interval before catching up on records that moved during downtime.
	r.poll(ctx)

	for {
		select {
		case <-ctx.Done():
			r.logger.Info("shutting down")
			return
		case <-ticker.C:
			r.poll(ctx)
		}
	}
}

// poll runs a single cycle: fetch a batch and drive each record in it.
func (r *Runner[T]) poll(ctx context.Context) {
	recs, err := r.fetcher.Fetch(ctx, r.maxBatch)
	if err != nil {
		r.logger.Error("failed to fetch active loans", "error", err)
		return
	}
	if len(recs) == 0 {
		return
	}
	r.logger.Info("polling tick", "active_loans", len(recs))

	for _, rec := range recs {
		if ctx.Err() != nil {
			return
		}
		r.driver.Drive(ctx, rec)
	}
}

// alertOps sends an ops alert, degrading to a log line when no AlertService is
// configured. Shared by both directions; alerts is optional everywhere.
func alertOps(alerts AlertService, logger *slog.Logger, subject, message string) {
	if alerts == nil {
		logger.Warn("ops alert", "subject", subject, "message", message)
		return
	}
	if err := alerts.AlertOps(subject, message); err != nil {
		logger.Warn("failed to send ops alert", "subject", subject, "error", err)
	}
}
