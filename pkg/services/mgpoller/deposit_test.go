package mgpoller

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/stellar/go-stellar-sdk/keypair"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Shamba-Records-Limited/microvault/pkg/payment/moneygram"
	"github.com/Shamba-Records-Limited/microvault/pkg/payment/stellaranchor"
)

// ============================================================================
// Test doubles for the deposit driver's collaborators
// ============================================================================

type fakeRepaymentFetcher struct {
	recs []RepaymentRecord
	err  error
	hits int
}

func (f *fakeRepaymentFetcher) GetDueRepayments(_ context.Context, _ int) ([]RepaymentRecord, error) {
	f.hits++
	return f.recs, f.err
}

type fakeRepaymentRecorder struct {
	mu sync.Mutex

	updates        int
	fundsReceived  []string
	settled        map[string]string
	expired        []string
	failed         map[string]string
	remindersSent  []string
	referencesSent []string
	nextPolls      map[string]time.Time
	vaultAttempts  map[string]int

	fundsReceivedErr error
	settledErr       error
	expiredErr       error
	reminderErr      error
}

func newFakeRepaymentRecorder() *fakeRepaymentRecorder {
	return &fakeRepaymentRecorder{
		settled:       map[string]string{},
		failed:        map[string]string{},
		nextPolls:     map[string]time.Time{},
		vaultAttempts: map[string]int{},
	}
}

func (r *fakeRepaymentRecorder) MarkReferenceSent(_ context.Context, loanID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.referencesSent = append(r.referencesSent, loanID)
	return nil
}

func (r *fakeRepaymentRecorder) RecordVaultAttempt(_ context.Context, loanID string, attempts int) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.vaultAttempts[loanID] = attempts
	return nil
}

func (r *fakeRepaymentRecorder) RecordDepositUpdate(_ context.Context, _ string, _ *stellaranchor.Transaction) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.updates++
	return nil
}

func (r *fakeRepaymentRecorder) MarkFundsReceived(_ context.Context, loanID string, _ *stellaranchor.Transaction) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.fundsReceivedErr != nil {
		return r.fundsReceivedErr
	}
	r.fundsReceived = append(r.fundsReceived, loanID)
	return nil
}

func (r *fakeRepaymentRecorder) MarkSettled(_ context.Context, loanID, hash string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.settledErr != nil {
		return r.settledErr
	}
	r.settled[loanID] = hash
	return nil
}

func (r *fakeRepaymentRecorder) MarkExpired(_ context.Context, loanID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.expiredErr != nil {
		return r.expiredErr
	}
	r.expired = append(r.expired, loanID)
	return nil
}

func (r *fakeRepaymentRecorder) MarkFailed(_ context.Context, loanID, reason string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.failed[loanID] = reason
	return nil
}

func (r *fakeRepaymentRecorder) MarkReminderSent(_ context.Context, loanID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.reminderErr != nil {
		return r.reminderErr
	}
	r.remindersSent = append(r.remindersSent, loanID)
	return nil
}

func (r *fakeRepaymentRecorder) ScheduleNextPoll(_ context.Context, loanID string, at time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.nextPolls[loanID] = at
	return nil
}

type fakeVaultRepayer struct {
	mu    sync.Mutex
	calls []struct {
		loanID   string
		borrower string
		amount   int64
	}
	hash string
	err  error
}

func (v *fakeVaultRepayer) RepayForBorrower(_ context.Context, loanID, borrower string, amount int64) (string, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.calls = append(v.calls, struct {
		loanID   string
		borrower string
		amount   int64
	}{loanID, borrower, amount})
	if v.err != nil {
		return "", v.err
	}
	return v.hash, nil
}

func (v *fakeVaultRepayer) count() int {
	v.mu.Lock()
	defer v.mu.Unlock()
	return len(v.calls)
}

type fakeRepaymentNotifier struct {
	mu         sync.Mutex
	err        error
	received   []string
	reminder   []string
	expired    []string
	references []string
	moreInfo   []string
}

func (n *fakeRepaymentNotifier) NotifyRepaymentReference(loanID, reference string) error {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.references = append(n.references, loanID+":"+reference)
	return nil
}

func (n *fakeRepaymentNotifier) NotifyRepaymentMoreInfo(loanID string) error {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.err != nil {
		return n.err
	}
	n.moreInfo = append(n.moreInfo, loanID)
	return nil
}

func (n *fakeRepaymentNotifier) NotifyRepaymentReceived(loanID string) error {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.received = append(n.received, loanID)
	return nil
}

func (n *fakeRepaymentNotifier) NotifyRepaymentReminder(loanID string) error {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.reminder = append(n.reminder, loanID)
	return nil
}

func (n *fakeRepaymentNotifier) NotifyRepaymentExpired(loanID string) error {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.expired = append(n.expired, loanID)
	return nil
}

// ============================================================================
// Harness
// ============================================================================

type depositHarness struct {
	driver   *DepositDriver
	srv      *mgFakeServer
	fetcher  *fakeRepaymentFetcher
	recorder *fakeRepaymentRecorder
	vault    *fakeVaultRepayer
	notifier *fakeRepaymentNotifier
	alerts   *fakeAlerts
	now      time.Time
}

func newTestDepositDriver(t *testing.T) *depositHarness {
	t.Helper()
	srv := newMGFakeServer(t)
	clientKP, err := keypair.Random()
	require.NoError(t, err)

	c, err := moneygram.New(moneygram.Config{
		HomeDomain:        srv.homeDom,
		WebAuthEndpoint:   srv.server.URL + "/auth",
		TransferServerURL: srv.server.URL,
		ServerSigningKey:  srv.serverKP.Address(),
		NetworkPassphrase: testNetworkPassphrase,
		USDCIssuer:        "GA5ZSEJYB37JRC5AVCIA5MOP4RHTM335X2KGX3IHOJAPP5RE34K4KZVN",
		TreasurySecret:    clientKP.Seed(),
		HTTPClient:        srv.server.Client(),
		Logger:            slog.Default(),
	})
	require.NoError(t, err)

	h := &depositHarness{
		srv:      srv,
		fetcher:  &fakeRepaymentFetcher{},
		recorder: newFakeRepaymentRecorder(),
		vault:    &fakeVaultRepayer{hash: "vault-repay-hash"},
		notifier: &fakeRepaymentNotifier{},
		alerts:   &fakeAlerts{},
		now:      time.Date(2026, 8, 23, 12, 0, 0, 0, time.UTC),
	}

	d, err := NewDepositDriver(DepositDriverDeps{
		Client:   c,
		Fetcher:  h.fetcher,
		Recorder: h.recorder,
		Vault:    h.vault,
		Notifier: h.notifier,
		Alerts:   h.alerts,
		Config:   DefaultConfig(),
	})
	require.NoError(t, err)
	d.now = func() time.Time { return h.now }
	h.driver = d
	return h
}

// depositRec returns a record mid-window, borrower committed, nothing sent.
func depositRec(h *depositHarness) RepaymentRecord {
	return RepaymentRecord{
		LoanID:            "loan-1",
		SequenceID:        "seq-1",
		MoneyGramTxID:     "mg-dep-1",
		ChildAccountIndex: 7,
		BorrowerAddress:   "GDLGMIMK7MM6YHJIPPRAVZSCFYJWE735GJ3BONBWROGW3LFGXSYL3RLB",
		PayoffStroops:     500000000, // 50 USDC
		RepaymentStatus:   repaymentInitiated,
		ExpiresAt:         h.now.Add(96 * time.Hour),
		PhoneNumber:       "+254700000001",
		UserID:            "user-1",
	}
}

func depositTxJSON(status stellaranchor.Status, extra string) string {
	return fmt.Sprintf(`{"transaction":{
		"id":"mg-dep-1","kind":"deposit","status":%q,
		"amount_in":"50.00","amount_in_asset":"iso4217:KES",
		"amount_out":"50.0000000","amount_out_asset":"USDC"%s
	}}`, status, extra)
}

// ============================================================================
// Happy path
// ============================================================================

func TestDeposit_Completed_SettlesViaRepayFor(t *testing.T) {
	h := newTestDepositDriver(t)
	h.srv.setTransactionJSON(depositTxJSON(stellaranchor.StatusCompleted,
		`,"stellar_transaction_id":"anchor-stellar-hash"`))
	h.fetcher.recs = []RepaymentRecord{depositRec(h)}

	h.driver.poll(context.Background())

	// Borrower told first, before the vault leg.
	assert.Equal(t, []string{"loan-1"}, h.recorder.fundsReceived)
	assert.Equal(t, []string{"loan-1"}, h.notifier.received)

	// Vault leg attributed to the borrower, for the locked payoff.
	require.Equal(t, 1, h.vault.count())
	assert.Equal(t, "loan-1", h.vault.calls[0].loanID)
	assert.Equal(t, "GDLGMIMK7MM6YHJIPPRAVZSCFYJWE735GJ3BONBWROGW3LFGXSYL3RLB", h.vault.calls[0].borrower)
	assert.Equal(t, int64(500000000), h.vault.calls[0].amount)

	assert.Equal(t, "vault-repay-hash", h.recorder.settled["loan-1"])
}

// A settled loan is never refetched, so the realistic replay is the one where
// the vault leg already ran but the row still says funds_received. The borrower
// must not be notified twice.
func TestDeposit_Completed_ReplayDoesNotRenotify(t *testing.T) {
	h := newTestDepositDriver(t)
	h.srv.setTransactionJSON(depositTxJSON(stellaranchor.StatusCompleted, ""))

	rec := depositRec(h)
	rec.RepaymentStatus = repaymentFundsReceived
	h.fetcher.recs = []RepaymentRecord{rec}

	h.driver.poll(context.Background())

	assert.Empty(t, h.recorder.fundsReceived, "funds already received, must not be re-marked")
	assert.Empty(t, h.notifier.received, "borrower must not be told twice")
	assert.Equal(t, 1, h.vault.count(), "vault leg is retried, which is the point of funds_received")
}

func TestDeposit_Completed_VaultFails_StaysFundsReceived(t *testing.T) {
	h := newTestDepositDriver(t)
	h.vault.err = errors.New("soroban simulation failed")
	h.srv.setTransactionJSON(depositTxJSON(stellaranchor.StatusCompleted, ""))
	h.fetcher.recs = []RepaymentRecord{depositRec(h)}

	h.driver.poll(context.Background())

	assert.Equal(t, []string{"loan-1"}, h.recorder.fundsReceived)
	assert.Equal(t, []string{"loan-1"}, h.notifier.received,
		"the borrower is told regardless: the failing leg is ours, not theirs")
	assert.Empty(t, h.recorder.settled)
	assert.Empty(t, h.recorder.failed, "funds are on the treasury; this is not a failed rail")
	assert.Contains(t, h.recorder.nextPolls, "loan-1", "must stay scheduled for reconciliation")
}

func TestDeposit_Completed_SettleRecordFails_AlertsLoudly(t *testing.T) {
	h := newTestDepositDriver(t)
	h.recorder.settledErr = errors.New("db down")
	h.srv.setTransactionJSON(depositTxJSON(stellaranchor.StatusCompleted, ""))
	h.fetcher.recs = []RepaymentRecord{depositRec(h)}

	h.driver.poll(context.Background())

	assert.Equal(t, 1, h.vault.count())
	assert.NotEmpty(t, h.alerts.calls, "chain moved but row did not — ops must know")
	assert.NotContains(t, h.recorder.nextPolls, "loan-1",
		"must not reschedule: another tick could repay a second time")
}

func TestDeposit_Completed_NoBorrowerAddress_WithholdsVaultLeg(t *testing.T) {
	h := newTestDepositDriver(t)
	rec := depositRec(h)
	rec.BorrowerAddress = ""
	h.srv.setTransactionJSON(depositTxJSON(stellaranchor.StatusCompleted, ""))
	h.fetcher.recs = []RepaymentRecord{rec}

	h.driver.poll(context.Background())

	assert.Equal(t, []string{"loan-1"}, h.recorder.fundsReceived)
	assert.Zero(t, h.vault.count(), "plain repay would drop the attribution the rail exists for")
	assert.NotEmpty(t, h.alerts.calls)
}

func TestDeposit_Completed_NoLockedPayoff_WithholdsVaultLeg(t *testing.T) {
	h := newTestDepositDriver(t)
	rec := depositRec(h)
	rec.PayoffStroops = 0
	h.srv.setTransactionJSON(depositTxJSON(stellaranchor.StatusCompleted, ""))
	h.fetcher.recs = []RepaymentRecord{rec}

	h.driver.poll(context.Background())

	assert.Zero(t, h.vault.count())
	assert.NotEmpty(t, h.alerts.calls)
}

// ============================================================================
// Window: expiry and the reminder
// ============================================================================

func TestDeposit_Incomplete_ExpiresAfterWindow(t *testing.T) {
	h := newTestDepositDriver(t)
	rec := depositRec(h)
	rec.ExpiresAt = h.now.Add(-time.Minute)
	rec.ReminderSent = true
	h.srv.setTransactionJSON(depositTxJSON(stellaranchor.StatusIncomplete, ""))
	h.fetcher.recs = []RepaymentRecord{rec}

	h.driver.poll(context.Background())

	assert.Equal(t, []string{"loan-1"}, h.recorder.expired)
	assert.Equal(t, []string{"loan-1"}, h.notifier.expired)
	assert.NotContains(t, h.recorder.nextPolls, "loan-1", "expired repayments stop being polled")
	assert.Zero(t, h.vault.count())
}

// A borrower standing at a counter must not have the quote pulled out from
// under them, so only the not-yet-committed state expires.
func TestDeposit_CommittedPastWindow_DoesNotExpire(t *testing.T) {
	h := newTestDepositDriver(t)
	rec := depositRec(h)
	rec.ExpiresAt = h.now.Add(-time.Minute)
	h.srv.setTransactionJSON(depositTxJSON(stellaranchor.StatusPendingUserTransferStart, ""))
	h.fetcher.recs = []RepaymentRecord{rec}

	h.driver.poll(context.Background())

	assert.Empty(t, h.recorder.expired)
	assert.Contains(t, h.recorder.nextPolls, "loan-1")
}

func TestDeposit_Incomplete_SendsOneReminderBeforeExpiry(t *testing.T) {
	h := newTestDepositDriver(t)
	rec := depositRec(h)
	rec.ExpiresAt = h.now.Add(2 * time.Hour) // inside the 24h reminder window
	h.srv.setTransactionJSON(depositTxJSON(stellaranchor.StatusIncomplete, ""))
	h.fetcher.recs = []RepaymentRecord{rec}

	h.driver.poll(context.Background())
	assert.Equal(t, []string{"loan-1"}, h.recorder.remindersSent)
	assert.Equal(t, []string{"loan-1"}, h.notifier.reminder)

	// Second tick with the marker set: no repeat.
	rec.ReminderSent = true
	h.fetcher.recs = []RepaymentRecord{rec}
	h.driver.poll(context.Background())
	assert.Len(t, h.notifier.reminder, 1)
}

func TestDeposit_Incomplete_EarlyInWindow_NoReminder(t *testing.T) {
	h := newTestDepositDriver(t)
	h.srv.setTransactionJSON(depositTxJSON(stellaranchor.StatusIncomplete, ""))
	h.fetcher.recs = []RepaymentRecord{depositRec(h)} // expires in 96h

	h.driver.poll(context.Background())

	assert.Empty(t, h.notifier.reminder)
	assert.Empty(t, h.recorder.remindersSent)
}

// The marker is written first so a failing SMS is not retried every tick. If
// the marker itself fails, nothing is sent — better a missed reminder than an
// unbounded SMS loop.
func TestDeposit_Incomplete_ReminderMarkerFails_DoesNotSend(t *testing.T) {
	h := newTestDepositDriver(t)
	h.recorder.reminderErr = errors.New("db down")
	rec := depositRec(h)
	rec.ExpiresAt = h.now.Add(2 * time.Hour)
	h.srv.setTransactionJSON(depositTxJSON(stellaranchor.StatusIncomplete, ""))
	h.fetcher.recs = []RepaymentRecord{rec}

	h.driver.poll(context.Background())

	assert.Empty(t, h.notifier.reminder)
}

// ============================================================================
// Cadence
// ============================================================================

func TestDeposit_BackoffIsIdleWhileUnopenedAndActiveOnceCommitted(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status stellaranchor.Status
		want   time.Duration
	}{
		{"unopened link backs off slowly", stellaranchor.StatusIncomplete, defaultDepositIdleBackoff},
		{"committed polls fast", stellaranchor.StatusPendingUserTransferStart, defaultDepositActiveBackoff},
		{"in flight polls fast", stellaranchor.StatusPendingStellar, defaultDepositActiveBackoff},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newTestDepositDriver(t)
			h.srv.setTransactionJSON(depositTxJSON(tc.status, ""))
			h.fetcher.recs = []RepaymentRecord{depositRec(h)}

			h.driver.poll(context.Background())

			require.Contains(t, h.recorder.nextPolls, "loan-1")
			assert.Equal(t, h.now.Add(tc.want), h.recorder.nextPolls["loan-1"])
		})
	}
}

// A MoneyGram outage must not leave next_poll_at in the past, or the row spins
// on every tick for as long as the outage lasts.
func TestDeposit_GetTransactionFails_StillReschedules(t *testing.T) {
	h := newTestDepositDriver(t)
	h.srv.setTransactionJSON("") // 404
	h.fetcher.recs = []RepaymentRecord{depositRec(h)}

	h.driver.poll(context.Background())

	require.Contains(t, h.recorder.nextPolls, "loan-1")
	assert.Equal(t, h.now.Add(defaultDepositIdleBackoff), h.recorder.nextPolls["loan-1"])
	assert.Zero(t, h.vault.count())
}

// ============================================================================
// Terminal failures
// ============================================================================

func TestDeposit_TerminalFailures(t *testing.T) {
	for _, tc := range []struct {
		status     stellaranchor.Status
		wantAlerts bool
	}{
		{stellaranchor.StatusTooSmall, true},
		{stellaranchor.StatusTooLarge, true},
		{stellaranchor.StatusExpired, false},
		{stellaranchor.StatusNoMarket, false},
		{stellaranchor.StatusError, false},
		{stellaranchor.StatusRefunded, false},
	} {
		t.Run(string(tc.status), func(t *testing.T) {
			h := newTestDepositDriver(t)
			h.srv.setTransactionJSON(depositTxJSON(tc.status, ""))
			h.fetcher.recs = []RepaymentRecord{depositRec(h)}

			h.driver.poll(context.Background())

			assert.Contains(t, h.recorder.failed, "loan-1")
			assert.Equal(t, []string{"loan-1"}, h.notifier.expired)
			assert.Zero(t, h.vault.count())
			assert.NotContains(t, h.recorder.nextPolls, "loan-1")
			if tc.wantAlerts {
				assert.NotEmpty(t, h.alerts.calls,
					"our amount gate and MG's limits disagree — a config problem, not a borrower one")
			} else {
				assert.Empty(t, h.alerts.calls)
			}
		})
	}
}

func TestDeposit_OnHold_AlertsWithoutFailing(t *testing.T) {
	h := newTestDepositDriver(t)
	h.srv.setTransactionJSON(depositTxJSON(stellaranchor.StatusOnHold, ""))
	h.fetcher.recs = []RepaymentRecord{depositRec(h)}

	h.driver.poll(context.Background())

	assert.NotEmpty(t, h.alerts.calls)
	assert.Empty(t, h.recorder.failed)
	assert.Contains(t, h.recorder.nextPolls, "loan-1")
}

// ============================================================================
// Construction and lifecycle
// ============================================================================

func TestDeposit_RejectsBadConfig(t *testing.T) {
	_, err := NewDepositDriver(DepositDriverDeps{Config: DefaultConfig()})
	require.Error(t, err)
}

func TestDeposit_NoTxID_Skips(t *testing.T) {
	h := newTestDepositDriver(t)
	rec := depositRec(h)
	rec.MoneyGramTxID = ""
	h.fetcher.recs = []RepaymentRecord{rec}

	h.driver.poll(context.Background())

	assert.Zero(t, h.recorder.updates)
	assert.Zero(t, h.vault.count())
}

func TestDeposit_StartShutsDownOnContextCancel(t *testing.T) {
	h := newTestDepositDriver(t)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		h.driver.Start(ctx)
		close(done)
	}()
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Start did not return after ctx cancel")
	}
}

func TestDeposit_ConfigDefaultsFillUnsetFields(t *testing.T) {
	got := PollerConfig{}.withDepositDefaults()
	assert.Equal(t, defaultDepositPollInterval, got.DepositPollInterval)
	assert.Equal(t, defaultDepositMaxBatch, got.DepositMaxBatch)
	assert.Equal(t, defaultDepositActiveBackoff, got.DepositActiveBackoff)
	assert.Equal(t, defaultDepositIdleBackoff, got.DepositIdleBackoff)

	// A negative reminder window disables the reminder rather than reaching
	// backwards past expiry.
	assert.Zero(t, PollerConfig{DepositReminderBefore: -time.Hour}.withDepositDefaults().DepositReminderBefore)
}

// ─────────────────────────────────────────────────────────────────────
// Vault-leg reconciliation
// ─────────────────────────────────────────────────────────────────────

// vaultFailureRec is a repayment whose funds already reached the treasury and
// whose vault leg has failed `attempts` times.
func vaultFailureRec(h *depositHarness, attempts int) RepaymentRecord {
	rec := depositRec(h)
	rec.RepaymentStatus = repaymentFundsReceived
	rec.VaultAttempts = attempts
	return rec
}

func TestDeposit_VaultFails_IncrementsAttemptCount(t *testing.T) {
	h := newTestDepositDriver(t)
	h.vault.err = errors.New("soroban simulation failed")
	h.srv.setTransactionJSON(depositTxJSON(stellaranchor.StatusCompleted, ""))
	h.fetcher.recs = []RepaymentRecord{vaultFailureRec(h, 3)}

	h.driver.poll(context.Background())

	assert.Equal(t, 4, h.recorder.vaultAttempts["loan-1"],
		"the count must advance from what was already on the row, not restart")
}

func TestDeposit_VaultFails_AlertsExactlyOnceAtTheCeiling(t *testing.T) {
	ceiling := DefaultConfig().withDepositDefaults().DepositVaultMaxAttempts

	// One below the ceiling: this tick reaches it and must alert.
	atCeiling := newTestDepositDriver(t)
	atCeiling.vault.err = errors.New("rpc down")
	atCeiling.srv.setTransactionJSON(depositTxJSON(stellaranchor.StatusCompleted, ""))
	atCeiling.fetcher.recs = []RepaymentRecord{vaultFailureRec(atCeiling, ceiling-1)}
	atCeiling.driver.poll(context.Background())

	require.Len(t, atCeiling.alerts.calls, 1, "crossing the ceiling must page ops")
	assert.Contains(t, atCeiling.alerts.calls[0], "vault leg stuck")

	// Already past the ceiling: still retrying, but silent.
	pastCeiling := newTestDepositDriver(t)
	pastCeiling.vault.err = errors.New("rpc down")
	pastCeiling.srv.setTransactionJSON(depositTxJSON(stellaranchor.StatusCompleted, ""))
	pastCeiling.fetcher.recs = []RepaymentRecord{vaultFailureRec(pastCeiling, ceiling+5)}
	pastCeiling.driver.poll(context.Background())

	assert.Empty(t, pastCeiling.alerts.calls,
		"ops are told once, not on every tick for the rest of the outage")
}

func TestDeposit_VaultFails_SlowsDownPastTheCeiling(t *testing.T) {
	cfg := DefaultConfig().withDepositDefaults()

	before := newTestDepositDriver(t)
	before.vault.err = errors.New("rpc down")
	before.srv.setTransactionJSON(depositTxJSON(stellaranchor.StatusCompleted, ""))
	before.fetcher.recs = []RepaymentRecord{vaultFailureRec(before, 1)}
	before.driver.poll(context.Background())

	after := newTestDepositDriver(t)
	after.vault.err = errors.New("rpc down")
	after.srv.setTransactionJSON(depositTxJSON(stellaranchor.StatusCompleted, ""))
	after.fetcher.recs = []RepaymentRecord{vaultFailureRec(after, cfg.DepositVaultMaxAttempts)}
	after.driver.poll(context.Background())

	early := before.recorder.nextPolls["loan-1"]
	late := after.recorder.nextPolls["loan-1"]
	require.False(t, early.IsZero())
	require.False(t, late.IsZero())
	assert.True(t, late.After(early),
		"past the ceiling the cause is unlikely to clear on its own, so retries must slow")
}

func TestDeposit_VaultFails_NeverGivesUp(t *testing.T) {
	h := newTestDepositDriver(t)
	h.vault.err = errors.New("rpc down")
	h.srv.setTransactionJSON(depositTxJSON(stellaranchor.StatusCompleted, ""))
	h.fetcher.recs = []RepaymentRecord{vaultFailureRec(h, 500)}

	h.driver.poll(context.Background())

	assert.Empty(t, h.recorder.failed,
		"the borrower's USDC is on the treasury; there is no state where abandoning the leg is correct")
	assert.Contains(t, h.recorder.nextPolls, "loan-1", "must remain scheduled however long it takes")
}

func TestDeposit_VaultSucceeds_DoesNotTouchAttemptCount(t *testing.T) {
	h := newTestDepositDriver(t)
	h.srv.setTransactionJSON(depositTxJSON(stellaranchor.StatusCompleted, ""))
	h.fetcher.recs = []RepaymentRecord{vaultFailureRec(h, 2)}

	h.driver.poll(context.Background())

	assert.NotEmpty(t, h.recorder.settled)
	assert.NotContains(t, h.recorder.vaultAttempts, "loan-1",
		"a successful leg records a settlement, not another attempt")
}

// ─────────────────────────────────────────────────────────────────────
// Deposit reference
// ─────────────────────────────────────────────────────────────────────

// Testing found borrowers completing the webview and never receiving a code.
// Nothing in the flow sent one: the committed-but-unpaid branch only
// rescheduled. Without the reference the agent has nothing to take cash
// against, so the repayment cannot complete however ready everything else is.
func TestDeposit_Committed_SendsReferenceOnce(t *testing.T) {
	h := newTestDepositDriver(t)
	h.srv.setTransactionJSON(depositTxJSON(stellaranchor.StatusPendingUserTransferStart, `,"external_transaction_id":"MG-REF-4417"`))
	h.fetcher.recs = []RepaymentRecord{depositRec(h)}

	h.driver.poll(context.Background())

	assert.Equal(t, []string{"loan-1"}, h.recorder.referencesSent,
		"the marker is written before the send, so a failing provider is not retried every tick")
	assert.Equal(t, []string{"loan-1:MG-REF-4417"}, h.notifier.references)
}

func TestDeposit_Committed_DoesNotResendReference(t *testing.T) {
	h := newTestDepositDriver(t)
	h.srv.setTransactionJSON(depositTxJSON(stellaranchor.StatusPendingUserTransferStart, `,"external_transaction_id":"MG-REF-4417"`))

	rec := depositRec(h)
	rec.ReferenceSent = true
	h.fetcher.recs = []RepaymentRecord{rec}

	h.driver.poll(context.Background())

	assert.Empty(t, h.notifier.references,
		"polls every two minutes while the borrower walks to the counter; one SMS is enough")
}

// SEP-24 defines external_transaction_id as the external transaction that
// "started the deposit", so on a cash-in it does not exist until after the
// borrower has paid — the whole period they need instructions, it is empty.
// more_info_url is populated from the first poll, and is the only thing they
// can act on.
func TestDeposit_NoReference_FallsBackToTheTransactionPage(t *testing.T) {
	h := newTestDepositDriver(t)
	h.srv.setTransactionJSON(depositTxJSON(stellaranchor.StatusPendingUserTransferStart,
		`,"more_info_url":"https://extramps.moneygram.com/transaction-status?transaction_id=4a93bfcf&token=eyJhbGciOiJIUzI1NiJ9"`))
	h.fetcher.recs = []RepaymentRecord{depositRec(h)}

	h.driver.poll(context.Background())

	assert.Equal(t, []string{"loan-1"}, h.notifier.moreInfo)
	assert.Empty(t, h.notifier.references)
	assert.Equal(t, []string{"loan-1"}, h.recorder.referencesSent,
		"one marker covers both artifacts — the borrower needs telling once, not twice")
}

// A code a borrower can read out to an agent beats a link they must open on a
// feature phone, so the reference wins whenever both are present.
func TestDeposit_ReferenceWinsOverTheTransactionPage(t *testing.T) {
	h := newTestDepositDriver(t)
	h.srv.setTransactionJSON(depositTxJSON(stellaranchor.StatusPendingUserTransferStart,
		`,"external_transaction_id":"MG-REF-4417","more_info_url":"https://extramps.moneygram.com/transaction-status?transaction_id=4a93bfcf"`))
	h.fetcher.recs = []RepaymentRecord{depositRec(h)}

	h.driver.poll(context.Background())

	assert.Equal(t, []string{"loan-1:MG-REF-4417"}, h.notifier.references)
	assert.Empty(t, h.notifier.moreInfo)
}

// The marker is spent before the send, so a failed send is terminal: no later
// tick retries and the borrower is never told how to pay. That must page
// someone rather than leave a warning in a log.
func TestDeposit_FailedPayInstructionsAlertsOps(t *testing.T) {
	h := newTestDepositDriver(t)
	h.notifier.err = errors.New("loan has no phone number to notify")
	h.srv.setTransactionJSON(depositTxJSON(stellaranchor.StatusPendingUserTransferStart,
		`,"more_info_url":"https://extramps.moneygram.com/transaction-status?transaction_id=4a93bfcf"`))
	h.fetcher.recs = []RepaymentRecord{depositRec(h)}

	h.driver.poll(context.Background())

	require.NotEmpty(t, h.alerts.bodies, "a borrower who cannot pay is not a warning-level event")
	assert.Contains(t, h.alerts.bodies[0], "loan-1")
	assert.Contains(t, h.alerts.bodies[0], "repayment_reference_sent",
		"the alert must say how to recover, since nothing retries on its own")
}

// Neither artifact issued yet. Sending an empty code, or copy trailing off
// where a link should be, is worse than sending nothing.
func TestDeposit_Committed_WithoutReference_SendsNothing(t *testing.T) {
	h := newTestDepositDriver(t)
	h.srv.setTransactionJSON(depositTxJSON(stellaranchor.StatusPendingUserTransferStart, ""))
	h.fetcher.recs = []RepaymentRecord{depositRec(h)}

	h.driver.poll(context.Background())

	assert.Empty(t, h.notifier.references)
	assert.Empty(t, h.notifier.moreInfo)
	assert.Empty(t, h.recorder.referencesSent, "no send, no marker — the next tick must retry")
	assert.Contains(t, h.recorder.nextPolls, "loan-1")
}

// ─────────────────────────────────────────────────────────────────────
// Deposit shortfall
// ─────────────────────────────────────────────────────────────────────

// SEP-24 defines amount_out as net of fee.total, and MoneyGram charges 3.00 USD
// on a 23.40 deposit in the sandbox. If that holds at settlement the treasury
// receives less than the borrower was quoted, and repaying the quoted figure
// drains it a little on every repayment.
func TestDeposit_ShortCredit_AlertsButStillRepaysQuotedPayoff(t *testing.T) {
	h := newTestDepositDriver(t)
	// Quoted 50 USDC, credited 47 — a 3 USDC fee.
	h.srv.setTransactionJSON(depositTxJSON(stellaranchor.StatusCompleted, `,"amount_out":"47.0000000"`))
	h.fetcher.recs = []RepaymentRecord{depositRec(h)}

	h.driver.poll(context.Background())

	require.NotEmpty(t, h.alerts.calls, "a silent treasury drain must page ops")
	assert.Contains(t, h.alerts.calls[0], "short of the quoted payoff")

	// Unchanged until a completed deposit settles the question.
	require.Len(t, h.vault.calls, 1)
	assert.Equal(t, int64(500000000), h.vault.calls[0].amount,
		"the quoted payoff is still what closes the loan")
}

func TestDeposit_FullCredit_DoesNotAlert(t *testing.T) {
	h := newTestDepositDriver(t)
	h.srv.setTransactionJSON(depositTxJSON(stellaranchor.StatusCompleted, `,"amount_out":"50.0000000"`))
	h.fetcher.recs = []RepaymentRecord{depositRec(h)}

	h.driver.poll(context.Background())

	assert.Empty(t, h.alerts.calls)
	assert.NotEmpty(t, h.recorder.settled)
}

// ─────────────────────────────────────────────────────────────────────
// Anchor deadline
// ─────────────────────────────────────────────────────────────────────

// MoneyGram states its own deadline and that is the binding one. The sandbox
// reported roughly 24 hours against a REPAYMENT_WINDOW default of 96, so acting
// on ours would keep a lapsed deposit live for three days and tell the borrower
// they still had time to pay.
func TestDeposit_AnchorDeadlineExpiresBeforeOurWindow(t *testing.T) {
	h := newTestDepositDriver(t)

	rec := depositRec(h)
	rec.ExpiresAt = h.now.Add(72 * time.Hour) // what our window believed
	h.fetcher.recs = []RepaymentRecord{rec}

	// MoneyGram says the deposit lapsed an hour ago.
	past := h.now.Add(-1 * time.Hour).UTC().Format(time.RFC3339)
	h.srv.setTransactionJSON(depositTxJSON(stellaranchor.StatusIncomplete,
		`,"user_action_required_by":"`+past+`"`))

	h.driver.poll(context.Background())

	assert.Equal(t, []string{"loan-1"}, h.recorder.expired,
		"the anchor's deadline governs, not the configured window")
}

// The reminder lead is a fraction of the whole window, measured from
// started_at. Deriving it from the time remaining would make the threshold
// chase itself and only ever be met at expiry.
func TestDeposit_ReminderLeadIsCappedByTheRealWindow(t *testing.T) {
	h := newTestDepositDriver(t)

	// A 24-hour window with 20 hours still to run. The configured lead is 24h,
	// which under the old rule would have fired the reminder at initiation —
	// telling the borrower nothing they had not just been told.
	started := h.now.Add(-4 * time.Hour).UTC().Format(time.RFC3339)
	deadline := h.now.Add(20 * time.Hour).UTC().Format(time.RFC3339)

	rec := depositRec(h)
	h.fetcher.recs = []RepaymentRecord{rec}
	h.srv.setTransactionJSON(depositTxJSON(stellaranchor.StatusIncomplete,
		`,"started_at":"`+started+`","user_action_required_by":"`+deadline+`"`))

	h.driver.poll(context.Background())

	assert.Empty(t, h.notifier.reminder,
		"20 hours out on a 24-hour window is far too early to nudge")
	assert.Empty(t, h.recorder.remindersSent)
}

func TestDeposit_ReminderFiresInsideTheCappedLead(t *testing.T) {
	h := newTestDepositDriver(t)

	// Same 24-hour window, now 1 hour from expiry — inside the 6-hour cap.
	started := h.now.Add(-23 * time.Hour).UTC().Format(time.RFC3339)
	deadline := h.now.Add(1 * time.Hour).UTC().Format(time.RFC3339)

	rec := depositRec(h)
	h.fetcher.recs = []RepaymentRecord{rec}
	h.srv.setTransactionJSON(depositTxJSON(stellaranchor.StatusIncomplete,
		`,"started_at":"`+started+`","user_action_required_by":"`+deadline+`"`))

	h.driver.poll(context.Background())

	assert.Equal(t, []string{"loan-1"}, h.notifier.reminder)
}
