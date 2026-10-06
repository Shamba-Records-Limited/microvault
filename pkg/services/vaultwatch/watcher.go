package vaultwatch

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/Shamba-Records-Limited/microvault/pkg/telemetry"

	protocol "github.com/stellar/go-stellar-sdk/protocols/rpc"

	"github.com/Shamba-Records-Limited/microvault/pkg/alerts"
	pkgErrors "github.com/Shamba-Records-Limited/microvault/pkg/errors"
	"github.com/Shamba-Records-Limited/microvault/pkg/models"
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

// allowlistChecker is the soroban.Service views Watcher needs.
type allowlistChecker interface {
	IsAllowed(ctx context.Context, userAddress string) (bool, error)
	AllowlistEnforced(ctx context.Context) (bool, error)
	ExitDeadline(ctx context.Context, address string) (*time.Time, error)
}

// exitDeadlineStore mirrors on-chain exit deadlines into the counterparty
// tables and drives the "window closing" alerts.
type exitDeadlineStore interface {
	SetExitDeadline(ctx context.Context, address string, deadline *time.Time) error
	ListExitWindowsClosing(ctx context.Context, before time.Time, level, limit int) ([]*models.CounterpartyAddress, error)
	SetExitWarningLevel(ctx context.Context, id string, level int) error
}

// Ops alert subjects. The frozen one pages: the contract should make it
// impossible.
const (
	SubjectExitWindowClosing       = "Depositor exit window closing"
	SubjectFrozenDepositorWithdrew = "Frozen depositor withdrew"
)

// exitWarnings are checked nearest first, so an address first seen inside
// a day gets only the one-day alert.
var exitWarnings = []struct {
	level  int
	within time.Duration
	label  string
}{
	{2, 24 * time.Hour, "1 day"},
	{1, 7 * 24 * time.Hour, "7 days"},
}

// maxEventsPerTick bounds a single getEvents call. See FetchVaultEvents's
// doc comment on why a full page is logged rather than paginated further.
const maxEventsPerTick = 200

// Watcher is the compliance watcher described in doc.go.
type Watcher struct {
	client     eventsClient
	allowlist  allowlistChecker
	deadlines  exitDeadlineStore
	alerts     alerts.Service
	cursor     repository.VaultWatchCursorRepository
	contractID string
	interval   time.Duration
	logger     *slog.Logger
}

// WatcherDeps are Watcher's collaborators; all required except Logger.
type WatcherDeps struct {
	Client     eventsClient
	Allowlist  allowlistChecker
	Deadlines  exitDeadlineStore
	Alerts     alerts.Service
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
		deadlines:  deps.Deadlines,
		alerts:     deps.Alerts,
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

	w.logger.InfoContext(ctx, "starting", "interval", w.interval)
	w.tick(ctx)

	for {
		select {
		case <-ctx.Done():
			w.logger.InfoContext(ctx, "shutting down")
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
	ctx, span := telemetry.StartRoot(ctx, "vaultwatch.tick")
	defer span.End()
	from, err := w.cursor.Get(ctx)
	if err != nil {
		w.logger.ErrorContext(ctx, "could not read the watch cursor", "error", err)
		return
	}

	// An unseeded cursor (0) has no meaningful ledger to start from — begin
	// watching from the current tip rather than requesting an invalid range.
	// Historical enumeration is the backfill script's job, not this loop's.
	if from == 0 {
		latest, err := w.client.GetLatestLedger(ctx)
		if err != nil {
			w.logger.ErrorContext(ctx, "could not read the latest ledger to seed the watch cursor", "error", err)
			return
		}
		if err := w.cursor.Advance(ctx, latest.Sequence); err != nil {
			w.logger.ErrorContext(ctx, "could not seed the watch cursor", "error", err)
		}
		return
	}

	resp, err := rpc.FetchVaultEvents(ctx, w.client, w.contractID, from, maxEventsPerTick)
	if err != nil {
		w.logger.ErrorContext(ctx, "could not fetch vault events, window will be retried next tick",
			"from_ledger", from, "error", err)
		return
	}
	if len(resp.Events) == maxEventsPerTick {
		w.logger.WarnContext(ctx, "vault event page was full — some events in this window may not have been scanned",
			"from_ledger", from, "max_events_per_tick", maxEventsPerTick)
	}

	nextLedger := from
	for _, info := range resp.Events {
		w.inspect(ctx, info)
		if info.Ledger < 0 {
			// Never emitted by a real network — guard against a negative
			// int32 wrapping to a huge uint32 and corrupting the cursor.
			w.logger.ErrorContext(ctx, "vault event had a negative ledger sequence, skipping for cursor purposes",
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
		w.logger.ErrorContext(ctx, "could not advance the watch cursor", "error", err)
	}
	w.checkExitWindows(ctx)
}

// inspect decodes one event and, for the kinds that move money or shares,
// confirms every participant is currently allowlisted. A mismatch means the
// contract's own gating let something through — see doc.go on why that is
// logged as an alert, not corrected here.
func (w *Watcher) inspect(ctx context.Context, info protocol.EventInfo) {
	event, err := soroban.DecodeVaultEvent(info)
	if err != nil {
		w.logger.ErrorContext(ctx, "could not decode a vault event", "event_id", info.ID, "error", err)
		return
	}
	switch event.Kind {
	case soroban.VaultEventDeposit, soroban.VaultEventTransfer:
	case soroban.VaultEventWithdraw:
		w.checkFrozenWithdraw(ctx, info, event.Addresses[2])
		return
	case soroban.VaultEventExitDeadlineSet:
		w.syncExitDeadline(ctx, event.Addresses[0], &event.Deadline)
		return
	case soroban.VaultEventExitDeadlineCleared:
		w.syncExitDeadline(ctx, event.Addresses[0], nil)
		return
	default:
		// Allow/disallow events name an address whose membership just
		// changed by design; checking them would flag every revocation.
		return
	}

	for _, addr := range event.Addresses {
		allowed, err := w.allowlist.IsAllowed(ctx, addr)
		if err != nil {
			w.logger.ErrorContext(ctx, "could not check allowlist membership during watch",
				"event_id", info.ID, "kind", string(event.Kind), "address", addr, "error", err)
			continue
		}
		if !allowed {
			w.logger.ErrorContext(ctx, "compliance canary: unallowlisted address participated in a vault event",
				pkgErrors.AttrAddress, addr,
				"event_id", info.ID,
				"kind", string(event.Kind),
				"tx_hash", info.TransactionHash,
				"ledger", info.Ledger,
			)
		}
	}
}

// checkFrozenWithdraw pages when a withdraw or redeem landed at or after the
// owner's exit deadline while enforcement was on: the freeze did not hold.
func (w *Watcher) checkFrozenWithdraw(ctx context.Context, info protocol.EventInfo, owner string) {
	closedAt, err := time.Parse(time.RFC3339, info.LedgerClosedAt)
	if err != nil {
		w.logger.ErrorContext(ctx, "vault withdraw event has an unreadable close time", "event_id", info.ID, "error", err)
		return
	}
	enforced, err := w.allowlist.AllowlistEnforced(ctx)
	if err != nil || !enforced {
		return
	}
	allowed, err := w.allowlist.IsAllowed(ctx, owner)
	if err != nil || allowed {
		return
	}
	deadline, err := w.allowlist.ExitDeadline(ctx, owner)
	if err != nil {
		w.logger.ErrorContext(ctx, "could not read the exit deadline during watch", pkgErrors.AttrAddress, owner, "error", err)
		return
	}
	if deadline == nil || closedAt.Before(*deadline) {
		return
	}
	alerts.Raise(ctx, w.alerts, w.logger, SubjectFrozenDepositorWithdrew,
		fmt.Sprintf("Owner %s withdrew in tx %s at %s, after its exit deadline %s. The vault freeze did not hold.",
			owner, info.TransactionHash, closedAt.Format(time.RFC3339), deadline.Format(time.RFC3339)))
}

func (w *Watcher) syncExitDeadline(ctx context.Context, address string, deadline *time.Time) {
	if w.deadlines == nil {
		return
	}
	if err := w.deadlines.SetExitDeadline(ctx, address, deadline); err != nil {
		w.logger.ErrorContext(ctx, "could not mirror the exit deadline", pkgErrors.AttrAddress, address, "error", err)
	}
}

// checkExitWindows raises one digest alert per address as its deadline comes
// within seven days and again within one, so ops can contact the depositor
// or extend.
func (w *Watcher) checkExitWindows(ctx context.Context) {
	if w.deadlines == nil {
		return
	}
	now := time.Now()
	for _, warn := range exitWarnings {
		addrs, err := w.deadlines.ListExitWindowsClosing(ctx, now.Add(warn.within), warn.level, maxEventsPerTick)
		if err != nil {
			w.logger.ErrorContext(ctx, "could not list closing exit windows", "error", err)
			return
		}
		for _, addr := range addrs {
			alerts.Raise(ctx, w.alerts, w.logger, SubjectExitWindowClosing,
				fmt.Sprintf("Depositor %s can withdraw until %s (within %s); its shares freeze after that. Contact the counterparty or extend the deadline.",
					addr.Address, addr.ExitDeadline.UTC().Format(time.RFC3339), warn.label))
			if err := w.deadlines.SetExitWarningLevel(ctx, addr.ID, warn.level); err != nil {
				w.logger.ErrorContext(ctx, "could not record the exit warning", pkgErrors.AttrAddress, addr.Address, "error", err)
			}
		}
	}
}
