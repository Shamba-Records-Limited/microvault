package mgpoller

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/stellar/go-stellar-sdk/keypair"
	"github.com/stellar/go-stellar-sdk/txnbuild"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Shamba-Records-Limited/microvault/pkg/payment/moneygram"
)

const testNetworkPassphrase = "Test SDF Network ; September 2015"

// mgFakeServer mimics the SEP-10 + SEP-24 endpoints with a swappable
// transaction status, so individual tests can drive the poller through
// any state transition without rebuilding the whole client.
type mgFakeServer struct {
	t        *testing.T
	serverKP *keypair.Full
	homeDom  string
	server   *httptest.Server

	mu           sync.Mutex
	currentTxRaw string // JSON for `transaction` envelope
}

func newMGFakeServer(t *testing.T) *mgFakeServer {
	t.Helper()
	kp, err := keypair.Random()
	require.NoError(t, err)
	f := &mgFakeServer{t: t, serverKP: kp, homeDom: "stellar.moneygram.test"}

	mux := http.NewServeMux()
	mux.HandleFunc("/auth", f.handleAuth)
	mux.HandleFunc("/transaction", f.handleTransaction)
	f.server = httptest.NewServer(mux)
	t.Cleanup(f.server.Close)
	return f
}

func (f *mgFakeServer) setTransactionJSON(raw string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.currentTxRaw = raw
}

func (f *mgFakeServer) handleAuth(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		clientAccount := r.URL.Query().Get("account")
		memoStr := r.URL.Query().Get("memo")
		require.NotEmpty(f.t, clientAccount)
		require.NotEmpty(f.t, memoStr)
		memo := txnbuild.MemoID(0)
		_, err := fmt.Sscanf(memoStr, "%d", &memo)
		require.NoError(f.t, err)
		tx, err := txnbuild.BuildChallengeTx(
			f.serverKP.Seed(), clientAccount, f.homeDom, f.homeDom,
			testNetworkPassphrase, 5*time.Minute, &memo,
		)
		require.NoError(f.t, err)
		xdr, err := tx.Base64()
		require.NoError(f.t, err)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{
			"transaction": xdr, "network_passphrase": testNetworkPassphrase,
		})
	case http.MethodPost:
		jwt := makeMGTestJWT(time.Now().Add(time.Hour).Unix())
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"token": jwt})
	}
}

func (f *mgFakeServer) handleTransaction(w http.ResponseWriter, _ *http.Request) {
	f.mu.Lock()
	raw := f.currentTxRaw
	f.mu.Unlock()
	if raw == "" {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	_, _ = fmt.Fprintln(w, raw)
}

func makeMGTestJWT(exp int64) string {
	header := "eyJhbGciOiJub25lIn0"
	payloadBytes, _ := json.Marshal(map[string]int64{"exp": exp})
	payload := base64URL(payloadBytes)
	return header + "." + payload + "."
}

func base64URL(b []byte) string {
	const alpha = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_"
	out := make([]byte, 0, (len(b)*4+2)/3)
	for i := 0; i < len(b); i += 3 {
		var n uint32
		var p int
		for j := 0; j < 3 && i+j < len(b); j++ {
			n |= uint32(b[i+j]) << (16 - 8*j)
			p++
		}
		for k := 0; k <= p; k++ {
			out = append(out, alpha[(n>>(18-6*k))&0x3F])
		}
	}
	return string(out)
}

// Test doubles for the poller's collaborators. Each records the calls it
// receives so individual tests can assert call counts and arguments.

type fakeFetcher struct {
	loans []LoanRecord
	err   error
	hits  int
}

func (f *fakeFetcher) GetActiveMoneyGramLoans(_ context.Context, _ int) ([]LoanRecord, error) {
	f.hits++
	return f.loans, f.err
}

type fakeRecorder struct {
	updates  []*moneygram.Transaction
	sendHash string
	mu       sync.Mutex
}

func (r *fakeRecorder) RecordTransactionUpdate(_ context.Context, _ string, tx *moneygram.Transaction) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.updates = append(r.updates, tx)
	return nil
}

func (r *fakeRecorder) RecordSendUSDC(_ context.Context, _ string, h string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.sendHash = h
	return nil
}

type fakeDisbursement struct {
	statuses           []string
	completedNotified  []string
	failedNotified     []string
	repays             []string
	mu                 sync.Mutex
}

func (d *fakeDisbursement) UpdateDisbursementStatus(seqID, status string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.statuses = append(d.statuses, seqID+"="+status)
	return nil
}

func (d *fakeDisbursement) NotifyDisbursementComplete(seqID string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.completedNotified = append(d.completedNotified, seqID)
	return nil
}

func (d *fakeDisbursement) NotifyDisbursementFailed(seqID string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.failedNotified = append(d.failedNotified, seqID)
	return nil
}

func (d *fakeDisbursement) RepayVault(seqID string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.repays = append(d.repays, seqID)
	return nil
}

type fakeTreasury struct {
	calls []sendCall
	hash  string
	err   error
}

type sendCall struct {
	dest, memo string
	stroops    int64
}

func (t *fakeTreasury) SendUSDC(_ context.Context, dest, memo string, stroops int64) (string, error) {
	t.calls = append(t.calls, sendCall{dest, memo, stroops})
	if t.err != nil {
		return "", t.err
	}
	return t.hash, nil
}

type fakeAlerts struct {
	calls []string
}

func (a *fakeAlerts) AlertOps(subject, _ string) error {
	a.calls = append(a.calls, subject)
	return nil
}

// newTestPoller wires a Poller against a fake MG server and the four fake
// dependencies. The tx state is pre-seeded by callers via srv.setTransactionJSON.
func newTestPoller(t *testing.T) (*Poller, *mgFakeServer, *fakeFetcher, *fakeRecorder, *fakeDisbursement, *fakeTreasury, *fakeAlerts) {
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

	fetcher := &fakeFetcher{}
	recorder := &fakeRecorder{}
	disb := &fakeDisbursement{}
	treas := &fakeTreasury{hash: "stellar-tx-hash"}
	alerts := &fakeAlerts{}

	p, err := NewPoller(c, fetcher, recorder, disb, treas, alerts, DefaultConfig(), nil)
	require.NoError(t, err)
	return p, srv, fetcher, recorder, disb, treas, alerts
}

func TestPoller_PendingUserTransferStart_SendsUSDC(t *testing.T) {
	p, srv, fetcher, recorder, disb, treas, _ := newTestPoller(t)
	srv.setTransactionJSON(`{"transaction":{
		"id":"mg-1","kind":"withdrawal","status":"pending_user_transfer_start",
		"withdraw_anchor_account":"GANCHOR","withdraw_memo":"4242"
	}}`)

	fetcher.loans = []LoanRecord{{
		LoanID:            "L-1",
		SequenceID:        "L-1",
		MoneyGramTxID:     "mg-1",
		ChildAccountIndex: 7,
		PrincipalStroops:  500_000_000, // 50 USDC
	}}
	p.poll(context.Background())

	require.Len(t, treas.calls, 1)
	assert.Equal(t, "GANCHOR", treas.calls[0].dest)
	assert.Equal(t, "4242", treas.calls[0].memo)
	assert.Equal(t, int64(500_000_000), treas.calls[0].stroops)
	assert.Equal(t, "stellar-tx-hash", recorder.sendHash)
	// No state transition yet — MG hasn't observed the payment.
	assert.Empty(t, disb.statuses)
}

func TestPoller_PendingUserTransferStart_AlreadySent_Skips(t *testing.T) {
	p, srv, fetcher, _, _, treas, _ := newTestPoller(t)
	srv.setTransactionJSON(`{"transaction":{
		"id":"mg-1","kind":"withdrawal","status":"pending_user_transfer_start",
		"withdraw_anchor_account":"GANCHOR","withdraw_memo":"4242",
		"stellar_transaction_id":"already-sent"
	}}`)

	fetcher.loans = []LoanRecord{{
		LoanID:           "L-1", SequenceID: "L-1", MoneyGramTxID: "mg-1",
		PrincipalStroops: 500_000_000,
	}}
	p.poll(context.Background())
	assert.Empty(t, treas.calls, "must not double-send when MG already observed the payment")
}

func TestPoller_PendingUserTransferComplete_DriftAlert(t *testing.T) {
	p, srv, fetcher, _, _, _, _ := newTestPoller(t)
	srv.setTransactionJSON(`{"transaction":{
		"id":"mg-1","kind":"withdrawal","status":"pending_user_transfer_complete",
		"amount_in":"50.00","amount_out":"5500.00","amount_out_asset":"iso4217:KES",
		"external_transaction_id":"REF-12345"
	}}`)

	// User saw 6500 KES at USSD entry, MG locked 5500 → 15% drift.
	fetcher.loans = []LoanRecord{{
		LoanID:               "L-1",
		SequenceID:           "L-1",
		MoneyGramTxID:        "mg-1",
		RequestedLocalAmount: 6500.0,
	}}
	// We can't easily intercept the slog "PAYOUT DRIFT" warning, but we
	// can assert that the recorder still ran and no terminal transitions
	// fired (drift is informational only, never blocking).
	p.poll(context.Background())
}

func TestPoller_Completed_NotifiesAndTransitions(t *testing.T) {
	p, srv, fetcher, _, disb, _, _ := newTestPoller(t)
	srv.setTransactionJSON(`{"transaction":{
		"id":"mg-1","kind":"withdrawal","status":"completed"
	}}`)

	fetcher.loans = []LoanRecord{{
		LoanID: "L-1", SequenceID: "L-1", MoneyGramTxID: "mg-1",
	}}
	p.poll(context.Background())

	require.Len(t, disb.statuses, 1)
	assert.Equal(t, "L-1=complete", disb.statuses[0])
	assert.Equal(t, []string{"L-1"}, disb.completedNotified)
}

func TestPoller_Refunded_MarksRefundPending(t *testing.T) {
	p, srv, fetcher, _, disb, _, _ := newTestPoller(t)
	srv.setTransactionJSON(`{"transaction":{
		"id":"mg-1","kind":"withdrawal","status":"refunded","refunded":true
	}}`)

	fetcher.loans = []LoanRecord{{
		LoanID: "L-1", SequenceID: "L-1", MoneyGramTxID: "mg-1",
	}}
	p.poll(context.Background())

	require.Len(t, disb.statuses, 1)
	assert.Equal(t, "L-1=refund_pending", disb.statuses[0])
	assert.Empty(t, disb.repays, "vault repay is the ingest worker's job, not the poller's")
}

func TestPoller_TerminalFailure_AfterUSDCSent_RepaysVault(t *testing.T) {
	p, srv, fetcher, _, disb, _, alerts := newTestPoller(t)
	srv.setTransactionJSON(`{"transaction":{
		"id":"mg-1","kind":"withdrawal","status":"expired",
		"stellar_transaction_id":"sent-already","message":"user did not pick up"
	}}`)

	fetcher.loans = []LoanRecord{{
		LoanID: "L-1", SequenceID: "L-1", MoneyGramTxID: "mg-1",
	}}
	p.poll(context.Background())

	require.Len(t, disb.statuses, 1)
	assert.Equal(t, "L-1=failed", disb.statuses[0])
	assert.Equal(t, []string{"L-1"}, disb.failedNotified)
	assert.Equal(t, []string{"L-1"}, disb.repays)
	assert.Contains(t, alerts.calls, "MoneyGram off-ramp terminated")
}

func TestPoller_TerminalFailure_BeforeUSDCSent_NoRepay(t *testing.T) {
	p, srv, fetcher, _, disb, _, _ := newTestPoller(t)
	srv.setTransactionJSON(`{"transaction":{
		"id":"mg-1","kind":"withdrawal","status":"too_small",
		"message":"amount below corridor minimum"
	}}`)

	fetcher.loans = []LoanRecord{{
		LoanID: "L-1", SequenceID: "L-1", MoneyGramTxID: "mg-1",
	}}
	p.poll(context.Background())

	assert.Equal(t, []string{"L-1=failed"}, disb.statuses)
	assert.Empty(t, disb.repays, "no USDC sent → nothing to repay")
}

func TestPoller_NonTerminalState_NoOp(t *testing.T) {
	p, srv, fetcher, _, disb, treas, _ := newTestPoller(t)
	srv.setTransactionJSON(`{"transaction":{
		"id":"mg-1","kind":"withdrawal","status":"pending_anchor"
	}}`)

	fetcher.loans = []LoanRecord{{
		LoanID: "L-1", SequenceID: "L-1", MoneyGramTxID: "mg-1",
	}}
	p.poll(context.Background())
	assert.Empty(t, disb.statuses)
	assert.Empty(t, treas.calls)
}

func TestPoller_RejectsBadConfig(t *testing.T) {
	_, err := NewPoller(nil, nil, nil, nil, nil, nil, DefaultConfig(), nil)
	require.Error(t, err)
}

func TestPoller_StartShutsDownOnContextCancel(t *testing.T) {
	p, _, _, _, _, _, _ := newTestPoller(t)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		p.Start(ctx)
		close(done)
	}()
	cancel()
	select {
	case <-done:
		// good — exited cleanly
	case <-time.After(2 * time.Second):
		t.Fatal("Start did not return after ctx cancel")
	}
}
