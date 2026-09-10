package compliance

import (
	"context"
	"log/slog"
	"time"

	"github.com/Shamba-Records-Limited/microvault/pkg/models"
	"github.com/Shamba-Records-Limited/microvault/pkg/repository"
)

// onchainWriterBatchSize bounds one tick's work — the compliance-role
// address population is small (institutional depositors, not retail), so
// this is a generous ceiling, not a real constraint in practice.
const onchainWriterBatchSize = 50

// OnchainSigner is the two vault-contract calls the writer needs — narrow
// enough that soroban.Service satisfies it directly, and tests can fake it
// without a real signing key. See soroban.Service.WithComplianceRole's doc
// comment on why these calls need the compliance role key, not the admin
// key the rest of this backend already holds.
type OnchainSigner interface {
	AllowDepositor(ctx context.Context, address string) error
	DisallowDepositor(ctx context.Context, address string) error
}

// OnchainWriter is the separate worker the source design doc §14 calls
// for: "the admin writes intent to counterparty_addresses and a separate
// worker performs the on-chain write and reconciles onchain_state — which
// keeps signing keys out of the web process." It runs in the credit
// backend (which already holds the treasury/admin keys), not cmd/admin.
type OnchainWriter struct {
	repo     repository.CounterpartyRepository
	signer   OnchainSigner
	interval time.Duration
	logger   *slog.Logger
}

// OnchainWriterDeps are OnchainWriter's collaborators; all required except
// Logger.
type OnchainWriterDeps struct {
	Repo     repository.CounterpartyRepository
	Signer   OnchainSigner
	Interval time.Duration
	Logger   *slog.Logger
}

// NewOnchainWriter builds the worker.
func NewOnchainWriter(deps OnchainWriterDeps) *OnchainWriter {
	logger := deps.Logger
	if logger == nil {
		logger = slog.Default()
	}
	return &OnchainWriter{
		repo:     deps.Repo,
		signer:   deps.Signer,
		interval: deps.Interval,
		logger:   logger.With("component", "onchain_writer"),
	}
}

// Start runs the write loop until ctx is cancelled — the same
// tick-immediately-then-on-interval shape as every other ticker in this
// codebase (pkg/services/mpesapoller, pkg/services/vaultwatch).
func (w *OnchainWriter) Start(ctx context.Context) {
	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()

	w.logger.Info("starting", "interval", w.interval)
	w.tick(ctx)

	for {
		select {
		case <-ctx.Done():
			w.logger.Info("shutting down")
			return
		case <-ticker.C:
			w.tick(ctx)
		}
	}
}

func (w *OnchainWriter) tick(ctx context.Context) {
	w.processAllows(ctx)
	w.processRevokes(ctx)
}

// processAllows submits allow_depositor for every address the database
// intends to allow but the chain hasn't confirmed yet (onchain_state
// "pending"). A failed submission leaves the row exactly where it was —
// retried next tick — rather than guessing at a resolution.
func (w *OnchainWriter) processAllows(ctx context.Context) {
	addrs, err := w.repo.ListAddressesNeedingOnchainAllow(ctx, onchainWriterBatchSize)
	if err != nil {
		w.logger.Error("could not list addresses needing an on-chain allow", "error", err)
		return
	}
	for _, addr := range addrs {
		if err := w.signer.AllowDepositor(ctx, addr.Address); err != nil {
			w.logger.Error("allow_depositor failed, will retry next tick",
				"address_id", addr.ID, "address", addr.Address, "error", err)
			continue
		}
		// AllowList::allow_user is idempotent on the contract side, so a
		// failure here (recording what already happened on-chain) is safe
		// to retry — the next tick's allow_depositor call is a harmless
		// no-op, not a double-effect. This is exactly the "database says
		// pending, contract says approved" drift pkg/services/vaultwatch
		// exists to catch as a second layer.
		if err := w.repo.SetOnchainState(ctx, addr.ID, models.OnchainStateApproved); err != nil {
			w.logger.Error("allow_depositor succeeded on-chain but the database write failed",
				"address_id", addr.ID, "address", addr.Address, "error", err)
		}
	}
}

// processRevokes is processAllows's mirror for disallow_depositor —
// addresses with a recorded revocation intent (RevokedAt set) whose
// onchain_state the chain hasn't confirmed as revoked yet.
func (w *OnchainWriter) processRevokes(ctx context.Context) {
	addrs, err := w.repo.ListAddressesNeedingOnchainRevoke(ctx, onchainWriterBatchSize)
	if err != nil {
		w.logger.Error("could not list addresses needing an on-chain revoke", "error", err)
		return
	}
	for _, addr := range addrs {
		if err := w.signer.DisallowDepositor(ctx, addr.Address); err != nil {
			w.logger.Error("disallow_depositor failed, will retry next tick",
				"address_id", addr.ID, "address", addr.Address, "error", err)
			continue
		}
		if err := w.repo.SetOnchainState(ctx, addr.ID, models.OnchainStateRevoked); err != nil {
			w.logger.Error("disallow_depositor succeeded on-chain but the database write failed",
				"address_id", addr.ID, "address", addr.Address, "error", err)
		}
	}
}
