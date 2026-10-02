// Package accountheal is the account chain reconciler: it heals accounts whose
// sponsored on-chain creation failed, stalled in pending, or predates
// chain_status, through the same EnsureOnChainAccount the loan path uses.
// Accounts in conflict (a reused derivation index) are never selected.
package accountheal

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/samber/oops"

	"github.com/Shamba-Records-Limited/microvault/pkg/account"
	pkgErrors "github.com/Shamba-Records-Limited/microvault/pkg/errors"
	"github.com/Shamba-Records-Limited/microvault/pkg/models"
	"github.com/Shamba-Records-Limited/microvault/pkg/repository"
	"github.com/Shamba-Records-Limited/microvault/pkg/services/mgpoller"
	"github.com/Shamba-Records-Limited/microvault/pkg/stellar"
)

// Ensurer creates the account on-chain if it is missing.
// *ussdadapters.UserServiceAdapter satisfies it.
type Ensurer interface {
	EnsureOnChainAccount(ctx context.Context, accountIndex int, address string) error
}

// Repo is the part of repository.AccountRepository the reconciler uses.
type Repo interface {
	GetDueChainHeals(ctx context.Context, due repository.ChainHealDue, limit int) ([]*models.Account, error)
	RecordChainCheck(ctx context.Context, id string, attempts int, checkedAt time.Time) error
	UpdateChainStatus(ctx context.Context, id string, chainStatus string) error
}

// Driver heals one account per Drive call.
type Driver struct {
	ensurer     Ensurer
	repo        Repo
	alerts      mgpoller.AlertService
	logger      *slog.Logger
	maxAttempts int
	now         func() time.Time
}

// Deps are the reconciler's collaborators and schedule. Alerts and Logger are
// optional; DB enables the advisory lock that keeps a second replica from
// running the same tick.
type Deps struct {
	Ensurer Ensurer
	Repo    Repo
	DB      *sql.DB
	Alerts  mgpoller.AlertService
	Logger  *slog.Logger

	Interval     time.Duration
	RetryAfter   time.Duration
	RetryBackoff time.Duration
	PendingAfter time.Duration
	MaxAttempts  int
}

// NewRunner pairs the driver with its due-set query.
func NewRunner(deps Deps) (*mgpoller.Runner[*models.Account], error) {
	if deps.Ensurer == nil || deps.Repo == nil {
		return nil, oops.In(pkgErrors.DomainStellarClassic).Tags("account-heal").
			Code(pkgErrors.CodeMissingDependency).Errorf("ensurer and repository are required")
	}
	logger := deps.Logger
	if logger == nil {
		logger = slog.Default()
	}
	maxAttempts := deps.MaxAttempts
	if maxAttempts <= 0 {
		maxAttempts = 5
	}
	driver := &Driver{
		ensurer:     deps.Ensurer,
		repo:        deps.Repo,
		alerts:      deps.Alerts,
		logger:      logger.With("component", "account_chain_reconciler"),
		maxAttempts: maxAttempts,
		now:         time.Now,
	}
	return mgpoller.NewRunner[*models.Account](mgpoller.RunnerDeps[*models.Account]{
		Direction: "account-chain-reconciler",
		Interval:  deps.Interval,
		MaxBatch:  20,
		Fetcher: mgpoller.FetchFunc[*models.Account](func(ctx context.Context, limit int) ([]*models.Account, error) {
			return deps.Repo.GetDueChainHeals(ctx, repository.ChainHealDue{
				Now:          time.Now(),
				RetryAfter:   deps.RetryAfter,
				RetryBackoff: deps.RetryBackoff,
				PendingAfter: deps.PendingAfter,
				MaxAttempts:  maxAttempts,
			}, limit)
		}),
		Driver: driver,
		DB:     deps.DB,
		Logger: logger,
	}), nil
}

// Drive heals acct. An outage (RPC or database unreachable) records the check
// without spending an attempt or touching chain_status, so a network blip
// never marks healthy accounts failed. A rejection that rebuilding cannot fix
// goes straight to the cap.
func (d *Driver) Drive(ctx context.Context, acct *models.Account) {
	if acct == nil {
		return
	}
	now := d.now()
	err := d.ensurer.EnsureOnChainAccount(ctx, acct.AccountIndex, acct.PublicKey)
	if err == nil {
		d.record(ctx, acct, 0, now)
		d.logger.InfoContext(ctx, "account chain healed",
			"account_id", acct.ID, "address", acct.PublicKey, "previous_status", acct.ChainStatus)
		return
	}

	switch {
	case errors.Is(err, account.ErrDerivationConflict):
		d.logger.WarnContext(ctx, "account turned out to be a derivation conflict, skipping",
			"account_id", acct.ID, "address", acct.PublicKey)
		return
	case errors.Is(err, account.ErrChainCheckUnavailable):
		d.record(ctx, acct, acct.ChainAttempts, now)
		d.logger.WarnContext(ctx, "account heal deferred by an outage",
			"account_id", acct.ID, "address", acct.PublicKey, "error", err)
		return
	}

	attempts := acct.ChainAttempts + 1
	if permanent(err) {
		attempts = max(attempts, d.maxAttempts)
	}
	d.record(ctx, acct, attempts, now)
	if acct.ChainStatus != models.ChainStatusFailed {
		if uerr := d.repo.UpdateChainStatus(ctx, acct.ID, models.ChainStatusFailed); uerr != nil {
			d.logger.ErrorContext(ctx, "could not mark account chain status failed", "account_id", acct.ID, "error", uerr)
		}
	}
	d.logger.ErrorContext(ctx, "account heal failed",
		"account_id", acct.ID, "address", acct.PublicKey,
		"attempts", attempts, "max_attempts", d.maxAttempts, "error", err)

	if acct.ChainAttempts < d.maxAttempts && attempts >= d.maxAttempts {
		d.alertOps(ctx, "Stellar account creation exhausted",
			fmt.Sprintf("Account %s (%s, index %d) still has no on-chain account after %d heal attempts; "+
				"the reconciler keeps retrying on its backoff. Last error: %v",
				acct.ID, acct.PublicKey, acct.AccountIndex, attempts, err))
	}
}

func (d *Driver) record(ctx context.Context, acct *models.Account, attempts int, now time.Time) {
	if err := d.repo.RecordChainCheck(ctx, acct.ID, attempts, now); err != nil {
		d.logger.ErrorContext(ctx, "could not record account heal attempt", "account_id", acct.ID, "error", err)
	}
}

func (d *Driver) alertOps(ctx context.Context, subject, message string) {
	if d.alerts == nil {
		d.logger.WarnContext(ctx, "ops alert", "subject", subject, "message", message)
		return
	}
	if err := d.alerts.AlertOps(subject, message); err != nil {
		d.logger.WarnContext(ctx, "failed to send ops alert", "subject", subject, "error", err)
	}
}

// permanent reports a failure that retrying the same heal cannot clear: the
// network refused the envelope itself, or the stored address does not match
// its index.
func permanent(err error) bool {
	return errors.Is(err, stellar.ErrTransactionRejectedPermanent) || errors.Is(err, account.ErrDerivedAddressMismatch)
}
