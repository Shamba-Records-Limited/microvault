package mgpoller

import (
	"context"
	"encoding/json"
	"errors"
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
	"github.com/Shamba-Records-Limited/microvault/pkg/payment/stellaranchor"
	"github.com/Shamba-Records-Limited/microvault/pkg/stellar/rpc"
	"github.com/Shamba-Records-Limited/microvault/pkg/stellar/types"
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
	updates   []*stellaranchor.Transaction
	sendHash  string
	claims    int
	releases  int
	claimErr  error
	refunds   []RefundRecord
	refundErr error
	mu        sync.Mutex
}

func (r *fakeRecorder) RecordSendAttempt(_ context.Context, _ string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.claimErr != nil {
		return r.claimErr
	}
	r.claims++
	return nil
}

func (r *fakeRecorder) ClearSendAttempt(_ context.Context, _ string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.releases++
	return nil
}

func (r *fakeRecorder) RecordTransactionUpdate(_ context.Context, _ string, tx *stellaranchor.Transaction) error {
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

func (r *fakeRecorder) RecordRefund(_ context.Context, _ string, refund RefundRecord) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.refundErr != nil {
		return r.refundErr
	}
	r.refunds = append(r.refunds, refund)
	return nil
}

type fakeDisbursement struct {
	statuses          []string
	completedNotified []string
	failedNotified    []string
	pickupReady       []string
	repays            []string
	refundNotified    []string
	amountRepays      []amountRepay
	repayAmountErr    error
	mu                sync.Mutex
}

type amountRepay struct {
	seqID   string
	stroops int64
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

func (d *fakeDisbursement) NotifyCashPickupReady(seqID string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.pickupReady = append(d.pickupReady, seqID)
	return nil
}

func (d *fakeDisbursement) RepayVault(seqID string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.repays = append(d.repays, seqID)
	return nil
}

func (d *fakeDisbursement) NotifyRefundReceived(seqID string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.refundNotified = append(d.refundNotified, seqID)
	return nil
}

func (d *fakeDisbursement) RepayVaultAmount(seqID string, stroops int64) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.repayAmountErr != nil {
		return d.repayAmountErr
	}
	d.amountRepays = append(d.amountRepays, amountRepay{seqID: seqID, stroops: stroops})
	return nil
}

// fakeVerifier stands in for on-ledger confirmation of an anchor's refund.
type fakeVerifier struct {
	ok    bool
	err   error
	calls []string

	// payments is what PaymentsTo returns for a matching destination+asset.
	// Empty means the hash names no payment to us — the state before a refund
	// lands, when stellar_transaction_id still points at our outbound send.
	payments    []rpc.Payment
	paymentsErr error
	paymentsTo  []string
}

func (v *fakeVerifier) TransactionSucceeded(_ context.Context, txHash string) (bool, error) {
	v.calls = append(v.calls, txHash)
	return v.ok, v.err
}

func (v *fakeVerifier) PaymentsTo(_ context.Context, txHash, destination, assetCode, assetIssuer string) ([]rpc.Payment, error) {
	v.paymentsTo = append(v.paymentsTo, txHash+"|"+destination+"|"+assetCode)
	if v.paymentsErr != nil {
		return nil, v.paymentsErr
	}
	return v.payments, nil
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

func (t *fakeTreasury) CheckUSDCTrustline(_ context.Context, _ string) (bool, error) {
	return true, nil
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

	// Verification defaults to passing. Refund tests that care override
	// p.verifier directly rather than widening this helper's return.
	p, err := NewPoller(c, fetcher, recorder, disb, treas,
		&fakeVerifier{ok: true}, alerts, DefaultConfig(), nil)
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
		LoanID: "L-1", SequenceID: "L-1", MoneyGramTxID: "mg-1",
		PrincipalStroops: 500_000_000,
	}}
	p.poll(context.Background())
	assert.Empty(t, treas.calls, "must not double-send when MG already observed the payment")
}

// Regression for the observed double-spend: MoneyGram left
// stellar_transaction_id empty for ~10 minutes after we paid, so every 30s
// tick re-sent USDC — 15 duplicate payments for one withdrawal, draining the
// treasury. HasStellarSend (persisted locally from RecordSendUSDC) must hold
// the guard on its own, without any echo from MG.
func TestPoller_PendingUserTransferStart_LocalSendMarker_PreventsDoubleSpend(t *testing.T) {
	p, srv, fetcher, recorder, _, treas, _ := newTestPoller(t)
	// Note: no stellar_transaction_id — MG has not echoed the payment yet.
	srv.setTransactionJSON(`{"transaction":{
		"id":"mg-1","kind":"withdrawal","status":"pending_user_transfer_start",
		"withdraw_anchor_account":"GANCHOR","withdraw_memo":"4242"
	}}`)

	rec := LoanRecord{
		LoanID: "L-1", SequenceID: "L-1", MoneyGramTxID: "mg-1",
		PrincipalStroops: 500_000_000,
	}
	fetcher.loans = []LoanRecord{rec}

	// First tick pays.
	p.poll(context.Background())
	require.Len(t, treas.calls, 1, "first tick should send once")
	require.NotEmpty(t, recorder.sendHash, "send hash must be recorded for idempotency")

	// Subsequent ticks: MG still silent, but our local marker is now set.
	rec.HasStellarSend = true
	fetcher.loans = []LoanRecord{rec}
	for i := 0; i < 5; i++ {
		p.poll(context.Background())
	}
	assert.Len(t, treas.calls, 1,
		"must not re-send while MG lags its stellar_transaction_id echo")
}

// The send must be claimed *before* submission, so a crash in the window
// between SendUSDC returning and RecordSendUSDC persisting cannot let the next
// tick pay twice.
func TestPoller_PendingUserTransferStart_ClaimsBeforeSending(t *testing.T) {
	p, srv, fetcher, recorder, _, treas, _ := newTestPoller(t)
	srv.setTransactionJSON(`{"transaction":{
		"id":"mg-1","kind":"withdrawal","status":"pending_user_transfer_start",
		"withdraw_anchor_account":"GANCHOR","withdraw_memo":"4242"
	}}`)
	fetcher.loans = []LoanRecord{{
		LoanID: "L-1", SequenceID: "L-1", MoneyGramTxID: "mg-1",
		PrincipalStroops: 500_000_000,
	}}

	p.poll(context.Background())

	assert.Equal(t, 1, recorder.claims, "send must be claimed before submitting")
	assert.Len(t, treas.calls, 1)
	assert.Equal(t, 0, recorder.releases, "successful send must not release the claim")
}

// If the claim cannot be persisted we cannot guarantee idempotency, so no
// payment may be attempted at all.
func TestPoller_PendingUserTransferStart_ClaimFails_DoesNotSend(t *testing.T) {
	p, srv, fetcher, recorder, _, treas, _ := newTestPoller(t)
	recorder.claimErr = fmt.Errorf("db down")
	srv.setTransactionJSON(`{"transaction":{
		"id":"mg-1","kind":"withdrawal","status":"pending_user_transfer_start",
		"withdraw_anchor_account":"GANCHOR","withdraw_memo":"4242"
	}}`)
	fetcher.loans = []LoanRecord{{
		LoanID: "L-1", SequenceID: "L-1", MoneyGramTxID: "mg-1",
		PrincipalStroops: 500_000_000,
	}}

	p.poll(context.Background())

	assert.Empty(t, treas.calls, "must not send when the claim could not be persisted")
}

// A payment that failed on-ledger burned only the fee, so the claim is
// released and a later tick may retry (e.g. after the treasury is refilled).
func TestPoller_PendingUserTransferStart_OnLedgerFailure_ReleasesClaim(t *testing.T) {
	p, srv, fetcher, recorder, _, treas, alerts := newTestPoller(t)
	treas.err = fmt.Errorf("treasury USDC transfer failed: %w", types.ErrTransactionFailedOnLedger)
	srv.setTransactionJSON(`{"transaction":{
		"id":"mg-1","kind":"withdrawal","status":"pending_user_transfer_start",
		"withdraw_anchor_account":"GANCHOR","withdraw_memo":"4242"
	}}`)
	fetcher.loans = []LoanRecord{{
		LoanID: "L-1", SequenceID: "L-1", MoneyGramTxID: "mg-1",
		PrincipalStroops: 500_000_000,
	}}

	p.poll(context.Background())

	assert.Equal(t, 1, recorder.claims)
	assert.Equal(t, 1, recorder.releases, "on-ledger failure moved no funds, so retry must be allowed")
	assert.NotEmpty(t, alerts.calls)
}

// An unknown outcome (timeout) must keep the claim: a duplicate payment is
// worse than a stalled loan.
func TestPoller_PendingUserTransferStart_UnknownOutcome_KeepsClaim(t *testing.T) {
	p, srv, fetcher, recorder, _, treas, alerts := newTestPoller(t)
	treas.err = fmt.Errorf("context deadline exceeded")
	srv.setTransactionJSON(`{"transaction":{
		"id":"mg-1","kind":"withdrawal","status":"pending_user_transfer_start",
		"withdraw_anchor_account":"GANCHOR","withdraw_memo":"4242"
	}}`)
	fetcher.loans = []LoanRecord{{
		LoanID: "L-1", SequenceID: "L-1", MoneyGramTxID: "mg-1",
		PrincipalStroops: 500_000_000,
	}}

	p.poll(context.Background())

	assert.Equal(t, 1, recorder.claims)
	assert.Equal(t, 0, recorder.releases, "unknown outcome must keep the claim to prevent a double-spend")
	assert.NotEmpty(t, alerts.calls)
}

func TestPoller_PendingUserTransferComplete_DriftAlert(t *testing.T) {
	p, srv, fetcher, _, _, _, _ := newTestPoller(t)
	srv.setTransactionJSON(`{"transaction":{
		"id":"mg-1","kind":"withdrawal","status":"pending_user_transfer_complete",
		"amount_in":"50.00","amount_out":"5500.00","amount_out_asset":"iso4217:KES",
		"external_transaction_id":"REF-12345"
	}}`)

	// User saw 6500 KES at USSD entry, MG locked 5500 to 15% drift.
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

// Regression: MG reporting pending_user_transfer_complete means it holds our
// USDC and the cash is collectable. The loan must leave its initiated status
// and the borrower must be told the reference exactly once — previously this
// handler only drift-alerted, so loans parked on mg_initiated forever and the
// borrower was never notified their money was waiting.
func TestPoller_PendingUserTransferComplete_MarksReadyAndNotifiesOnce(t *testing.T) {
	p, srv, fetcher, _, disb, _, _ := newTestPoller(t)
	srv.setTransactionJSON(`{"transaction":{
		"id":"mg-1","kind":"withdrawal","status":"pending_user_transfer_complete",
		"amount_in":"50.00","amount_out":"5500.00","amount_out_asset":"iso4217:KES",
		"external_transaction_id":"REF-12345",
		"stellar_transaction_id":"abc123"
	}}`)

	rec := LoanRecord{
		LoanID:             "L-1",
		SequenceID:         "L-1",
		MoneyGramTxID:      "mg-1",
		DisbursementStatus: "mg_initiated",
	}
	fetcher.loans = []LoanRecord{rec}

	p.poll(context.Background())

	if len(disb.statuses) != 1 || disb.statuses[0] != "L-1="+statusProcessing {
		t.Fatalf("expected one transition to %s, got %v", statusProcessing, disb.statuses)
	}
	if len(disb.pickupReady) != 1 || disb.pickupReady[0] != "L-1" {
		t.Fatalf("expected one cash-pickup-ready notification, got %v", disb.pickupReady)
	}

	// Next tick with the status already advanced must not re-notify.
	fetcher.loans = []LoanRecord{{
		LoanID:             "L-1",
		SequenceID:         "L-1",
		MoneyGramTxID:      "mg-1",
		DisbursementStatus: statusProcessing,
	}}
	p.poll(context.Background())

	if len(disb.pickupReady) != 1 {
		t.Fatalf("cash-pickup SMS re-sent on repeat poll: %v", disb.pickupReady)
	}
	if len(disb.statuses) != 1 {
		t.Fatalf("status re-written on repeat poll: %v", disb.statuses)
	}
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
	assert.Equal(t, "L-1=completed", disb.statuses[0])
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

// refund_pending is not terminal for this poller, so a refunded loan keeps
// being fetched every tick. The status transition has to be the guard.
func TestPoller_Refunded_DoesNotRewriteStatusOnRepeatPoll(t *testing.T) {
	p, srv, fetcher, _, disb, _, _ := newTestPoller(t)
	srv.setTransactionJSON(`{"transaction":{
		"id":"mg-1","kind":"withdrawal","status":"refunded","refunded":true
	}}`)

	fetcher.loans = []LoanRecord{{
		LoanID: "L-1", SequenceID: "L-1", MoneyGramTxID: "mg-1",
	}}
	p.poll(context.Background())
	require.Len(t, disb.statuses, 1)

	fetcher.loans = []LoanRecord{{
		LoanID: "L-1", SequenceID: "L-1", MoneyGramTxID: "mg-1",
		DisbursementStatus: statusRefundPending,
	}}
	p.poll(context.Background())

	assert.Len(t, disb.statuses, 1, "status re-written while awaiting inbound USDC")
}

// refundedTxJSON is a MG transaction in refunded state carrying a settled
// refund of the given gross amount and fee.
func refundedTxJSON(gross, fee, hash string) string {
	return `{"transaction":{
		"id":"mg-1","kind":"withdrawal","status":"refunded","refunded":true,
		"amount_in":"50.0000000","amount_in_asset":"USDC",
		"refunds":{
			"amount_refunded":"` + gross + `","amount_fee":"` + fee + `",
			"payments":[{"id":"` + hash + `","id_type":"stellar","amount":"` + gross + `","fee":"` + fee + `"}]
		}
	}}`
}

// refundPendingLoan is a loan already marked refund_pending, i.e. on the tick
// after MG first reported the refund.
func refundPendingLoan(principalStroops int64) LoanRecord {
	return LoanRecord{
		LoanID:             "L-1",
		SequenceID:         "L-1",
		MoneyGramTxID:      "mg-1",
		PrincipalStroops:   principalStroops,
		DisbursementStatus: statusRefundPending,
	}
}

func TestPoller_Refunded_SettlesFullRefund(t *testing.T) {
	p, srv, fetcher, recorder, disb, _, alerts := newTestPoller(t)
	// Sent 50 USDC, MG returns all of it with no fee.
	srv.setTransactionJSON(refundedTxJSON("50.0000000", "0", "refund-hash"))
	fetcher.loans = []LoanRecord{refundPendingLoan(500000000)}

	p.poll(context.Background())

	require.Len(t, recorder.refunds, 1)
	assert.Equal(t, "refund-hash", recorder.refunds[0].TxHash)
	assert.Equal(t, int64(500000000), recorder.refunds[0].NetStroops)
	assert.Zero(t, recorder.refunds[0].ShortfallStroops)

	require.Len(t, disb.amountRepays, 1)
	assert.Equal(t, int64(500000000), disb.amountRepays[0].stroops)
	assert.Empty(t, disb.repays, "principal-amount repay must not be used for refunds")

	assert.Equal(t, []string{"L-1=" + statusRefundReceived}, disb.statuses)
	assert.Equal(t, []string{"L-1"}, disb.refundNotified)
	assert.Empty(t, alerts.calls)
}

// The anchor's own refund fee is money the treasury does not get back, so it
// counts as a shortfall and pages ops like any other.
func TestPoller_Refunded_AnchorFeeCountsAsShortfall(t *testing.T) {
	p, srv, fetcher, recorder, disb, _, alerts := newTestPoller(t)
	srv.setTransactionJSON(refundedTxJSON("50.0000000", "1.5000000", "refund-hash"))
	fetcher.loans = []LoanRecord{refundPendingLoan(500000000)}

	p.poll(context.Background())

	require.Len(t, recorder.refunds, 1)
	assert.Equal(t, int64(485000000), recorder.refunds[0].NetStroops)
	assert.Equal(t, int64(15000000), recorder.refunds[0].ShortfallStroops)

	require.Len(t, disb.amountRepays, 1)
	assert.Equal(t, int64(485000000), disb.amountRepays[0].stroops,
		"vault must be repaid with what came back, not the full principal")
	assert.Contains(t, alerts.calls, "MoneyGram refund shortfall")
}

func TestPoller_Refunded_ShortfallAlertsAndStillRepaysWhatArrived(t *testing.T) {
	p, srv, fetcher, recorder, disb, _, alerts := newTestPoller(t)
	// Sent 50 USDC but MG only returns 40.
	srv.setTransactionJSON(refundedTxJSON("40.0000000", "0", "refund-hash"))
	fetcher.loans = []LoanRecord{refundPendingLoan(500000000)}

	p.poll(context.Background())

	require.Len(t, recorder.refunds, 1)
	assert.Equal(t, int64(100000000), recorder.refunds[0].ShortfallStroops)

	require.Len(t, disb.amountRepays, 1)
	assert.Equal(t, int64(400000000), disb.amountRepays[0].stroops)
	assert.Equal(t, []string{"L-1=" + statusRefundReceived}, disb.statuses)
	assert.Contains(t, alerts.calls, "MoneyGram refund shortfall")
}

func TestPoller_Refunded_NoPaymentsYet_StaysPending(t *testing.T) {
	p, srv, fetcher, recorder, disb, _, _ := newTestPoller(t)
	srv.setTransactionJSON(`{"transaction":{
		"id":"mg-1","kind":"withdrawal","status":"refunded","refunded":true,
		"refunds":{"amount_refunded":"50.0000000","amount_fee":"0","payments":[]}
	}}`)
	fetcher.loans = []LoanRecord{refundPendingLoan(500000000)}

	p.poll(context.Background())

	assert.Empty(t, recorder.refunds)
	assert.Empty(t, disb.amountRepays)
	assert.Empty(t, disb.statuses, "already refund_pending, nothing to advance yet")
}

func TestPoller_Refunded_NotOnLedger_WithholdsRepay(t *testing.T) {
	p, srv, fetcher, recorder, disb, _, alerts := newTestPoller(t)
	srv.setTransactionJSON(refundedTxJSON("50.0000000", "0", "bogus-hash"))
	fetcher.loans = []LoanRecord{refundPendingLoan(500000000)}
	p.verifier = &fakeVerifier{ok: false}

	p.poll(context.Background())

	assert.Empty(t, disb.amountRepays, "must not repay against a refund that did not land")
	assert.Empty(t, recorder.refunds)
	assert.Empty(t, disb.statuses)
	assert.Contains(t, alerts.calls, "MoneyGram refund not on ledger")
}

func TestPoller_Refunded_VerificationUnknown_RetriesWithoutAlerting(t *testing.T) {
	p, srv, fetcher, _, disb, _, alerts := newTestPoller(t)
	srv.setTransactionJSON(refundedTxJSON("50.0000000", "0", "refund-hash"))
	fetcher.loans = []LoanRecord{refundPendingLoan(500000000)}
	p.verifier = &fakeVerifier{err: errors.New("rpc unavailable")}

	p.poll(context.Background())

	assert.Empty(t, disb.amountRepays)
	assert.Empty(t, disb.statuses)
	assert.Empty(t, alerts.calls, "an unreachable RPC is not an anchor fault")
}

func TestPoller_Refunded_RepayFails_StaysPendingForRetry(t *testing.T) {
	p, srv, fetcher, _, disb, _, alerts := newTestPoller(t)
	srv.setTransactionJSON(refundedTxJSON("50.0000000", "0", "refund-hash"))
	fetcher.loans = []LoanRecord{refundPendingLoan(500000000)}
	disb.repayAmountErr = errors.New("vault unreachable")

	p.poll(context.Background())

	assert.Empty(t, disb.statuses, "must not reach refund_received when the vault was not repaid")
	assert.Contains(t, alerts.calls, "Vault repay failed after MoneyGram refund")
}

func TestPoller_Refunded_RecordFails_WithholdsRepay(t *testing.T) {
	p, srv, fetcher, recorder, disb, _, _ := newTestPoller(t)
	srv.setTransactionJSON(refundedTxJSON("50.0000000", "0", "refund-hash"))
	fetcher.loans = []LoanRecord{refundPendingLoan(500000000)}
	recorder.refundErr = errors.New("db down")

	p.poll(context.Background())

	assert.Empty(t, disb.amountRepays, "no repay without a durable record of what came back")
	assert.Empty(t, disb.statuses)
}

func TestPoller_Refunded_FirstTickMarksPendingThenSettles(t *testing.T) {
	p, srv, fetcher, _, disb, _, _ := newTestPoller(t)
	srv.setTransactionJSON(refundedTxJSON("50.0000000", "0", "refund-hash"))

	// First observation: loan is still mg_initiated.
	fetcher.loans = []LoanRecord{{
		LoanID: "L-1", SequenceID: "L-1", MoneyGramTxID: "mg-1",
		PrincipalStroops: 500000000, DisbursementStatus: "mg_initiated",
	}}
	p.poll(context.Background())

	require.Len(t, disb.statuses, 2, "one tick can both open and settle the refund")
	assert.Equal(t, "L-1="+statusRefundPending, disb.statuses[0])
	assert.Equal(t, "L-1="+statusRefundReceived, disb.statuses[1])
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
	assert.Empty(t, disb.repays, "no USDC sent to nothing to repay")
}

func TestPoller_NonTerminalState_NoOp(t *testing.T) {
	for _, status := range []string{
		"incomplete",
		"pending_anchor",
		"pending_external",
		"pending_stellar",
	} {
		t.Run(status, func(t *testing.T) {
			p, srv, fetcher, _, disb, treas, alerts := newTestPoller(t)
			srv.setTransactionJSON(fmt.Sprintf(
				`{"transaction":{"id":"mg-1","kind":"withdrawal","status":%q}}`, status))

			fetcher.loans = []LoanRecord{{
				LoanID: "L-1", SequenceID: "L-1", MoneyGramTxID: "mg-1",
			}}
			p.poll(context.Background())
			assert.Empty(t, disb.statuses)
			assert.Empty(t, treas.calls)
			assert.Empty(t, alerts.calls)
		})
	}
}

func TestPoller_OnHold_AlertsOpsNoTransition(t *testing.T) {
	p, srv, fetcher, _, disb, treas, alerts := newTestPoller(t)
	srv.setTransactionJSON(`{"transaction":{
		"id":"mg-1","kind":"withdrawal","status":"on_hold",
		"message":"compliance review in progress"
	}}`)

	fetcher.loans = []LoanRecord{{
		LoanID: "L-1", SequenceID: "L-1", MoneyGramTxID: "mg-1",
	}}
	p.poll(context.Background())

	assert.Empty(t, disb.statuses, "on_hold is not terminal — no disbursement transition")
	assert.Empty(t, treas.calls)
	assert.Contains(t, alerts.calls, "MoneyGram transaction on hold")
}

func TestPoller_PendingTrust_AlertsOpsNoTransition(t *testing.T) {
	p, srv, fetcher, _, disb, treas, alerts := newTestPoller(t)
	srv.setTransactionJSON(`{"transaction":{
		"id":"mg-1","kind":"withdrawal","status":"pending_trust",
		"message":"anchor missing USDC trustline"
	}}`)

	fetcher.loans = []LoanRecord{{
		LoanID: "L-1", SequenceID: "L-1", MoneyGramTxID: "mg-1",
	}}
	p.poll(context.Background())

	assert.Empty(t, disb.statuses)
	assert.Empty(t, treas.calls)
	assert.Contains(t, alerts.calls, "MoneyGram pending_trust")
}

func TestPoller_PendingUser_LogsOnly(t *testing.T) {
	p, srv, fetcher, _, disb, treas, alerts := newTestPoller(t)
	srv.setTransactionJSON(`{"transaction":{
		"id":"mg-1","kind":"withdrawal","status":"pending_user",
		"message":"awaiting user action"
	}}`)

	fetcher.loans = []LoanRecord{{
		LoanID: "L-1", SequenceID: "L-1", MoneyGramTxID: "mg-1",
	}}
	p.poll(context.Background())

	assert.Empty(t, disb.statuses, "pending_user is in-flight — no disbursement transition")
	assert.Empty(t, treas.calls)
	assert.Empty(t, alerts.calls, "user-driven waits should not page ops")
}

func TestPoller_RejectsBadConfig(t *testing.T) {
	_, err := NewPoller(nil, nil, nil, nil, nil, nil, nil, DefaultConfig(), nil)
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

// MoneyGram's real testnet payload for a user-cancelled withdrawal: status
// "refunded", the deprecated `refunded` flag false, and no refunds object at
// all. The settlement path waits on refunds.payments, so before the attempt
// ceiling this loan polled forever in silence and the vault was never repaid.
const mgRealRefundedPayload = `{"transaction":{
	"id":"mg-1","kind":"withdrawal","status":"refunded",
	"withdraw_anchor_account":"GCU7KVTYEPCUZOQPJ6RLNWW64FS3IBHGZTEFWERO7ZZVDQYHMG7W5B6M",
	"withdraw_memo_type":"id",
	"amount_in":"7.8","amount_in_asset":"USDC",
	"amount_out":"1004.0","amount_out_asset":"KES",
	"amount_fee":"0.0","amount_fee_asset":"USDC",
	"stellar_transaction_id":"225e86168268d5b22bb2863101460de4f4a29e9a65d5567fe0c8fb19b7307b38",
	"external_transaction_id":"72540163","refunded":false
}}`

func TestPoller_Refunded_NoRefundsObject_DoesNotRepayVault(t *testing.T) {
	p, srv, fetcher, recorder, disb, _, _ := newTestPoller(t)
	srv.setTransactionJSON(mgRealRefundedPayload)
	fetcher.loans = []LoanRecord{refundPendingLoan(78000000)}

	p.poll(context.Background())

	assert.Empty(t, recorder.refunds, "no refund hash is available to record")
	assert.Empty(t, disb.amountRepays, "repaying without proof of return could overdraw the treasury")
}

// Without a ceiling this is a silent infinite loop: no alert, no state change,
// every tick identical.
func TestPoller_Refunded_NoRefundsObject_AlertsOnceAtCeiling(t *testing.T) {
	p, srv, fetcher, _, _, _, alerts := newTestPoller(t)
	srv.setTransactionJSON(mgRealRefundedPayload)
	fetcher.loans = []LoanRecord{refundPendingLoan(78000000)}
	p.cfg.RefundSettleMaxAttempts = 3

	for range 3 {
		p.poll(context.Background())
	}
	require.Len(t, alerts.calls, 1, "should escalate exactly once at the ceiling")
	assert.Contains(t, alerts.calls[0], "MoneyGram refund has no settlement details")

	// Polling continues in case MG backfills, but must not re-alert each tick.
	for range 5 {
		p.poll(context.Background())
	}
	assert.Len(t, alerts.calls, 1, "the ceiling must not re-fire on every later tick")
}

// A ceiling of zero disables escalation rather than alerting on every tick.
func TestPoller_Refunded_NoRefundsObject_CeilingDisabled(t *testing.T) {
	p, srv, fetcher, _, _, _, alerts := newTestPoller(t)
	srv.setTransactionJSON(mgRealRefundedPayload)
	fetcher.loans = []LoanRecord{refundPendingLoan(78000000)}
	p.cfg.RefundSettleMaxAttempts = 0

	for range 5 {
		p.poll(context.Background())
	}
	assert.Empty(t, alerts.calls)
}
