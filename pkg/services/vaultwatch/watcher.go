package vaultwatch

import (
	"context"
	"log/slog"
	"time"

	protocol "github.com/stellar/go-stellar-sdk/protocols/rpc"

	pkgErrors "github.com/Shamba-Records-Limited/microvault/pkg/errors"
	"github.com/Shamba-Records-Limited/microvault/pkg/repository"
	"github.com/Shamba-Records-Limited/microvault/pkg/stellar/rpc"
	"github.com/Shamba-Records-Limited/microvault/pkg/stellar/soroban"
)

// eventsClient is the part of the RPC client Watcher needs — narrow enough
// that tests can stand in for it without HTTP, matching pullQuerier in
// pkg/services/mpesapoller.
type eventsClient interface {
	rpc.EventsGetter
	GetLatestLedger(ctx context.Context) (protocol.GetLatestLedgerResponse, error)
}

// allowlistChecker is the one soroban.Service method Watcher needs.
type allowlistChecker interface {
	IsAllowed(ctx context.Context, userAddress string) (bool, error)
}

// maxEventsPerTick bounds a single getEvents call. See FetchVaultEvents's
// doc comment on why a full page is logged rather than paginated further.
const maxEventsPerTick = 200

// Watcher is the compliance watcher described in doc.go.
type Watcher struct {
	client     eventsClient
	allowlist  allowlistChecker
	cursor     repository.VaultWatchCursorRepository
	contractID string
	interval   time.Duration
	logger     *slog.Logger
}

// WatcherDeps are Watcher's collaborators; all required except Logger.
type WatcherDeps struct {
	Client     eventsClient
	Allowlist  allowlistChecker
	Cursor     repository.VaultWatchCursorRepository
	ContractID string
	Interval   time.Duration
	Logger     *slog.Logger
}

// NewWatcher builds the watcher.
func NewWatcher(deps WatcherDeps) *Watcher {
	logger := deps.Logger
	if logger == nil {
		logger = slog.Default()
	}
	return &Watcher{
		client:     deps.Client,
		allowlist:  deps.Allowlist,
		cursor:     deps.Cursor,
		contractID: deps.ContractID,
		interval:   deps.Interval,
		logger:     logger.With("component", "vault_watcher"),
	}
}

// Start runs the watch loop until ctx is cancelled.
func (w *Watcher) Start(ctx context.Context) {
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

// tick scans one window of new events. A failed window leaves the cursor
// where it was, so the same window is retried next tick — matching the Pull
// sweep's precedent in pkg/services/mpesapoller.
func (w *Watcher) tick(ctx context.Context) {
	from, err := w.cursor.Get(ctx)
	if err != nil {
		w.logger.Error("could not read the watch cursor", "error", err)
		return
	}

	// An unseeded cursor (0) has no meaningful ledger to start from — begin
	// watching from the current tip rather than requesting an invalid range.
	// Historical enumeration is the backfill script's job, not this loop's.
	if from == 0 {
		latest, err := w.client.GetLatestLedger(ctx)
		if err != nil {
			w.logger.Error("could not read the latest ledger to seed the watch cursor", "error", err)
			return
		}
		if err := w.cursor.Advance(ctx, latest.Sequence); err != nil {
			w.logger.Error("could not seed the watch cursor", "error", err)
		}
		return
	}

	resp, err := rpc.FetchVaultEvents(ctx, w.client, w.contractID, from, maxEventsPerTick)
	if err != nil {
		w.logger.Error("could not fetch vault events, window will be retried next tick",
			"from_ledger", from, "error", err)
		return
	}
	if len(resp.Events) == maxEventsPerTick {
		w.logger.Warn("vault event page was full — some events in this window may not have been scanned",
			"from_ledger", from, "max_events_per_tick", maxEventsPerTick)
	}

	nextLedger := from
	for _, info := range resp.Events {
		w.inspect(ctx, info)
		if info.Ledger < 0 {
			// Never emitted by a real network — guard against a negative
			// int32 wrapping to a huge uint32 and corrupting the cursor.
			w.logger.Error("vault event had a negative ledger sequence, skipping for cursor purposes",
				"event_id", info.ID, "ledger", info.Ledger)
			continue
		}
		if ledger := uint32(info.Ledger); ledger >= nextLedger {
			nextLedger = ledger + 1
		}
	}
	// No events this window: still advance to the RPC's own high-water mark
	// so the cursor does not fall permanently behind on a quiet vault.
	if len(resp.Events) == 0 && resp.LatestLedger > 0 {
		nextLedger = resp.LatestLedger + 1
	}

	if err := w.cursor.Advance(ctx, nextLedger); err != nil {
		w.logger.Error("could not advance the watch cursor", "error", err)
	}
}

// inspect decodes one event and, for the kinds that move money or shares,
// confirms every participant is currently allowlisted. A mismatch means the
// contract's own gating let something through — see doc.go on why that is
// logged as an alert, not corrected here.
func (w *Watcher) inspect(ctx context.Context, info protocol.EventInfo) {
	event, err := soroban.DecodeVaultEvent(info)
	if err != nil {
		w.logger.Error("could not decode a vault event", "event_id", info.ID, "error", err)
		return
	}
	if event.Kind == soroban.VaultEventUnrecognized {
		return
	}

	for _, addr := range event.Addresses {
		allowed, err := w.allowlist.IsAllowed(ctx, addr)
		if err != nil {
			w.logger.Error("could not check allowlist membership during watch",
				"event_id", info.ID, "kind", string(event.Kind), "address", addr, "error", err)
			continue
		}
		if !allowed {
			w.logger.Error("compliance canary: unallowlisted address participated in a vault event",
				pkgErrors.AttrAddress, addr,
				"event_id", info.ID,
				"kind", string(event.Kind),
				"tx_hash", info.TransactionHash,
				"ledger", info.Ledger,
			)
		}
	}
}
