package vaultwatch

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/stellar/go-stellar-sdk/keypair"
	protocol "github.com/stellar/go-stellar-sdk/protocols/rpc"
	"github.com/stellar/go-stellar-sdk/xdr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Shamba-Records-Limited/microvault/pkg/models"
)

// testAddress returns a fresh, valid Stellar account address for fixtures.
func testAddress(t *testing.T) string {
	t.Helper()
	return keypair.MustRandom().Address()
}

// vaultTopicXDR encodes a symbol name followed by addresses into the base64
// XDR topic strings a getEvents response would carry — a package-local copy
// of the identical helper in pkg/stellar/soroban's own tests, since that
// package's encoder is unexported.
func vaultTopicXDR(t *testing.T, symbol string, addresses ...string) []string {
	t.Helper()
	sym := xdr.ScSymbol(symbol)
	symVal := xdr.ScVal{Type: xdr.ScValTypeScvSymbol, Sym: &sym}
	symXDR, err := xdr.MarshalBase64(symVal)
	require.NoError(t, err)

	topics := []string{symXDR}
	for _, addr := range addresses {
		accountID := xdr.MustAddress(addr)
		scAddr, err := xdr.NewScAddress(xdr.ScAddressTypeScAddressTypeAccount, accountID)
		require.NoError(t, err)
		val := xdr.ScVal{Type: xdr.ScValTypeScvAddress, Address: &scAddr}
		out, err := xdr.MarshalBase64(val)
		require.NoError(t, err)
		topics = append(topics, out)
	}
	return topics
}

type fakeEventsClient struct {
	events       []protocol.EventInfo
	latestLedger uint32
	latestErr    error
	eventsErr    error
	gotRequest   protocol.GetEventsRequest
}

func (f *fakeEventsClient) GetEvents(_ context.Context, req protocol.GetEventsRequest) (protocol.GetEventsResponse, error) {
	f.gotRequest = req
	if f.eventsErr != nil {
		return protocol.GetEventsResponse{}, f.eventsErr
	}
	return protocol.GetEventsResponse{Events: f.events, LatestLedger: f.latestLedger}, nil
}

func (f *fakeEventsClient) GetLatestLedger(_ context.Context) (protocol.GetLatestLedgerResponse, error) {
	if f.latestErr != nil {
		return protocol.GetLatestLedgerResponse{}, f.latestErr
	}
	return protocol.GetLatestLedgerResponse{Sequence: f.latestLedger}, nil
}

type fakeAllowlist struct {
	allowed   map[string]bool
	enforced  bool
	deadlines map[string]time.Time
	checked   []string
}

func (f *fakeAllowlist) IsAllowed(_ context.Context, address string) (bool, error) {
	f.checked = append(f.checked, address)
	return f.allowed[address], nil
}

func (f *fakeAllowlist) AllowlistEnforced(context.Context) (bool, error) { return f.enforced, nil }

func (f *fakeAllowlist) ExitDeadline(_ context.Context, address string) (*time.Time, error) {
	if d, ok := f.deadlines[address]; ok {
		return &d, nil
	}
	return nil, nil
}

type fakeCursor struct {
	ledger uint32
}

func (f *fakeCursor) Get(_ context.Context) (uint32, error) { return f.ledger, nil }
func (f *fakeCursor) Advance(_ context.Context, ledger uint32) error {
	f.ledger = ledger
	return nil
}

func newTestWatcher(client eventsClient, allowlist allowlistChecker, cursor *fakeCursor) *Watcher {
	return NewWatcher(WatcherDeps{
		Client:     client,
		Allowlist:  allowlist,
		Cursor:     cursor,
		ContractID: "CA7HVINR2AY532D7UPOE7MMOZAN2ZBZ327VWIO33K7JM7C4G6XYX5NOT",
		Logger:     slog.New(slog.DiscardHandler),
	})
}

func TestWatcherTick_SeedsUnsetCursorFromLatestLedger(t *testing.T) {
	client := &fakeEventsClient{latestLedger: 555}
	cursor := &fakeCursor{ledger: 0}
	w := newTestWatcher(client, &fakeAllowlist{}, cursor)

	w.tick(context.Background())

	assert.Equal(t, uint32(555), cursor.ledger)
}

func TestWatcherTick_CleanRunFindsNoMismatch(t *testing.T) {
	events, addrs := depositEventFixture(t)
	client := &fakeEventsClient{events: events, latestLedger: 200}
	cursor := &fakeCursor{ledger: 100}
	allowlist := &fakeAllowlist{allowed: map[string]bool{
		addrs[0]: true, addrs[1]: true, addrs[2]: true,
	}}
	w := newTestWatcher(client, allowlist, cursor)

	w.tick(context.Background())

	// Cursor advances past the event's ledger.
	assert.Equal(t, uint32(151), cursor.ledger)
}

func TestWatcherTick_QuietWindowAdvancesToLatestLedger(t *testing.T) {
	client := &fakeEventsClient{latestLedger: 300}
	cursor := &fakeCursor{ledger: 100}
	w := newTestWatcher(client, &fakeAllowlist{}, cursor)

	w.tick(context.Background())

	assert.Equal(t, uint32(301), cursor.ledger)
}

func TestWatcherTick_FailedFetchLeavesCursorUnmoved(t *testing.T) {
	client := &fakeEventsClient{eventsErr: assertErr("rpc down")}
	cursor := &fakeCursor{ledger: 100}
	w := newTestWatcher(client, &fakeAllowlist{}, cursor)

	w.tick(context.Background())

	assert.Equal(t, uint32(100), cursor.ledger)
}

// depositEventFixture returns a single decodable "deposit" event at ledger
// 150, plus the three addresses it carries (operator, from, receiver).
func depositEventFixture(t *testing.T) ([]protocol.EventInfo, []string) {
	t.Helper()
	a, b, c := testAddress(t), testAddress(t), testAddress(t)
	return []protocol.EventInfo{
		{
			ID:              "evt-1",
			Ledger:          150,
			TransactionHash: "deadbeef",
			TopicXDR:        vaultTopicXDR(t, "deposit", a, b, c),
		},
	}, []string{a, b, c}
}

type assertErrType string

func (e assertErrType) Error() string { return string(e) }
func assertErr(msg string) error      { return assertErrType(msg) }

func TestInspect_LogsMismatchWithoutPanicking(t *testing.T) {
	events, addrs := depositEventFixture(t)
	// Only two of the three participants are allowed — the third is a
	// mismatch the watcher should flag, not act on.
	allowlist := &fakeAllowlist{allowed: map[string]bool{addrs[0]: true, addrs[1]: true}}
	cursor := &fakeCursor{ledger: 100}
	w := newTestWatcher(&fakeEventsClient{}, allowlist, cursor)

	require.NotPanics(t, func() {
		w.inspect(context.Background(), events[0])
	})
}

type recordingAlerts struct{ subjects []string }

func (r *recordingAlerts) AlertOps(_ context.Context, subject, _ string) error {
	r.subjects = append(r.subjects, subject)
	return nil
}

type fakeDeadlines struct {
	byAddress map[string]*time.Time
	rows      []*models.CounterpartyAddress
}

func (f *fakeDeadlines) SetExitDeadline(_ context.Context, address string, deadline *time.Time) error {
	f.byAddress[address] = deadline
	return nil
}

func (f *fakeDeadlines) ListExitWindowsClosing(_ context.Context, before time.Time, level, _ int) ([]*models.CounterpartyAddress, error) {
	var out []*models.CounterpartyAddress
	for _, r := range f.rows {
		if r.ExitDeadline.After(time.Now()) && !r.ExitDeadline.After(before) && r.ExitWarningLevel < level {
			out = append(out, r)
		}
	}
	return out, nil
}

func (f *fakeDeadlines) SetExitWarningLevel(_ context.Context, id string, level int) error {
	for _, r := range f.rows {
		if r.ID == id {
			r.ExitWarningLevel = level
		}
	}
	return nil
}

func newExitWatcher(allowlist *fakeAllowlist, deadlines *fakeDeadlines, rec *recordingAlerts) *Watcher {
	return NewWatcher(WatcherDeps{
		Client:    &fakeEventsClient{},
		Allowlist: allowlist,
		Deadlines: deadlines,
		Alerts:    rec,
		Cursor:    &fakeCursor{ledger: 100},
		Logger:    slog.New(slog.DiscardHandler),
	})
}

func withdrawEvent(t *testing.T, owner string, closedAt time.Time) protocol.EventInfo {
	op := testAddress(t)
	return protocol.EventInfo{
		ID:             "w-1",
		LedgerClosedAt: closedAt.UTC().Format(time.RFC3339),
		TopicXDR:       vaultTopicXDR(t, "withdraw", op, op, owner),
	}
}

func TestInspect_WithdrawAfterDeadlinePages(t *testing.T) {
	owner := testAddress(t)
	deadline := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	rec := &recordingAlerts{}
	w := newExitWatcher(&fakeAllowlist{enforced: true, deadlines: map[string]time.Time{owner: deadline}}, nil, rec)

	w.inspect(context.Background(), withdrawEvent(t, owner, deadline.Add(-time.Hour)))
	assert.Empty(t, rec.subjects, "a withdraw inside the grace window is legitimate")

	w.inspect(context.Background(), withdrawEvent(t, owner, deadline))
	assert.Equal(t, []string{SubjectFrozenDepositorWithdrew}, rec.subjects)
}

func TestInspect_WithdrawIgnoredWhenEnforcementOffOrAllowed(t *testing.T) {
	owner := testAddress(t)
	deadline := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	rec := &recordingAlerts{}

	newExitWatcher(&fakeAllowlist{deadlines: map[string]time.Time{owner: deadline}}, nil, rec).
		inspect(context.Background(), withdrawEvent(t, owner, deadline.Add(time.Hour)))
	newExitWatcher(&fakeAllowlist{enforced: true, allowed: map[string]bool{owner: true}, deadlines: map[string]time.Time{owner: deadline}}, nil, rec).
		inspect(context.Background(), withdrawEvent(t, owner, deadline.Add(time.Hour)))

	assert.Empty(t, rec.subjects)
}

func TestInspect_DisallowEventIsNotACanaryHit(t *testing.T) {
	addr := testAddress(t)
	allowlist := &fakeAllowlist{enforced: true}
	w := newExitWatcher(allowlist, nil, &recordingAlerts{})

	w.inspect(context.Background(), protocol.EventInfo{ID: "d-1", TopicXDR: vaultTopicXDR(t, "user_disallowed", addr)})

	assert.Empty(t, allowlist.checked, "a revocation must not be checked against the allowlist")
}

func TestInspect_ExitDeadlineEventsSyncTheStore(t *testing.T) {
	addr := testAddress(t)
	store := &fakeDeadlines{byAddress: map[string]*time.Time{}}
	w := newExitWatcher(&fakeAllowlist{}, store, &recordingAlerts{})

	w.inspect(context.Background(), protocol.EventInfo{ID: "c-1", TopicXDR: vaultTopicXDR(t, "exit_deadline_cleared", addr)})

	got, ok := store.byAddress[addr]
	assert.True(t, ok)
	assert.Nil(t, got)
}

func TestCheckExitWindows_AlertsOncePerLevel(t *testing.T) {
	in3Days := time.Now().Add(72 * time.Hour)
	in12Hours := time.Now().Add(12 * time.Hour)
	store := &fakeDeadlines{rows: []*models.CounterpartyAddress{
		{ID: "a", Address: "GA", ExitDeadline: &in3Days},
		{ID: "b", Address: "GB", ExitDeadline: &in12Hours},
	}}
	rec := &recordingAlerts{}
	w := newExitWatcher(&fakeAllowlist{}, store, rec)

	w.checkExitWindows(context.Background())
	w.checkExitWindows(context.Background())

	assert.Equal(t, []string{SubjectExitWindowClosing, SubjectExitWindowClosing}, rec.subjects, "one alert each, not repeated")
	assert.Equal(t, 1, store.rows[0].ExitWarningLevel)
	assert.Equal(t, 2, store.rows[1].ExitWarningLevel, "first seen inside a day gets only the one-day alert")
}
