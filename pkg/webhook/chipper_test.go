package webhook

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/govalues/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Shamba-Records-Limited/microvault/pkg/payment/chipper"
	"github.com/Shamba-Records-Limited/microvault/pkg/payment/fonbnk"
	"github.com/Shamba-Records-Limited/microvault/pkg/payment/offramp"
	"github.com/Shamba-Records-Limited/microvault/pkg/payment/yellowcard"
)

type fakeChipperPayouts struct {
	released   []chipper.Release
	releaseErr error
	reissued   []string
	reissueErr error
	payouts    map[string]*chipper.Payout
}

func (f *fakeChipperPayouts) ReleasePayout(_ context.Context, rel chipper.Release) (*offramp.Result, error) {
	if f.releaseErr != nil {
		return nil, f.releaseErr
	}
	f.released = append(f.released, rel)
	return &offramp.Result{RequestID: "pay_released"}, nil
}

func (f *fakeChipperPayouts) ReissuePayout(_ context.Context, _ *chipper.Payout, reference string) (*chipper.Payout, error) {
	if f.reissueErr != nil {
		return nil, f.reissueErr
	}
	f.reissued = append(f.reissued, reference)
	return &chipper.Payout{ID: "pay_retry", ExternalReference: reference}, nil
}

func (f *fakeChipperPayouts) Payout(_ context.Context, id string) (*chipper.Payout, error) {
	if p, ok := f.payouts[id]; ok {
		return p, nil
	}
	return nil, errors.New("not found")
}

type fakeChipperStore struct {
	settled  bool
	direct   bool
	pending  []chipper.Release
	inFlight []ChipperInFlight
	recorded map[string]string
	cutoff   time.Time
}

func (f *fakeChipperStore) DisbursementSettled(context.Context, string) (bool, error) {
	return f.settled, nil
}

func (f *fakeChipperStore) PendingChipperReleases(context.Context, int) ([]chipper.Release, error) {
	return f.pending, nil
}

func (f *fakeChipperStore) InFlightChipperPayouts(_ context.Context, cutoff time.Time, _ int) ([]ChipperInFlight, error) {
	f.cutoff = cutoff
	return f.inFlight, nil
}

func (f *fakeChipperStore) RecordChipperPayout(_ context.Context, seq, payoutID string) error {
	if f.recorded == nil {
		f.recorded = map[string]string{}
	}
	f.recorded[seq] = payoutID
	return nil
}

func (f *fakeChipperStore) IsDirectSettlement(context.Context, string) (bool, error) {
	return f.direct, nil
}

func chipperPayout(status, reference string) *chipper.Payout {
	rate := decimal.MustParse("130.226805")
	return &chipper.Payout{
		ID:                "pay_kes_1",
		Status:            status,
		To:                chipper.PayoutParty{Amount: decimal.MustParse("1000.00"), Currency: "KES"},
		From:              chipper.PayoutParty{Amount: decimal.MustParse("7.678911"), Currency: "USDC"},
		Fee:               &chipper.Money{Amount: decimal.MustParse("0.115184"), Currency: "USDC"},
		Rate:              &rate,
		ExternalReference: reference,
	}
}

func payoutEvent(t *testing.T, event string, p *chipper.Payout) chipper.Event {
	t.Helper()
	data, err := json.Marshal(p)
	require.NoError(t, err)
	return chipper.Event{Event: event, Data: data}
}

func chipperService(disb *fakeDisb, alerts *fakeAlerts, payouts *fakeChipperPayouts, store *fakeChipperStore) *Service {
	return NewService(disb, alerts, nil, nil).WithChipper(payouts, store)
}

func TestChipperPayoutCompleted(t *testing.T) {
	disb, store := &fakeDisb{}, &fakeChipperStore{direct: true}
	svc := chipperService(disb, &fakeAlerts{}, &fakeChipperPayouts{}, store)

	require.NoError(t, svc.ProcessChipperEvent(context.Background(), payoutEvent(t, chipper.EventPayoutCompleted, chipperPayout(chipper.PayoutCompleted, "loan-1"))))

	assert.Equal(t, yellowcard.DisbursementComplete, disb.last())
	assert.True(t, disb.notifiedComplete)
	require.NotNil(t, disb.completion)
	assert.InDelta(t, 1000.0, disb.completion.ConvertedAmountLocal, 1e-9)
	assert.InDelta(t, 0.115184, disb.completion.ServiceFeeAmountUSD, 1e-9)
	assert.InDelta(t, 15.0, disb.completion.ServiceFeeAmountLocal, 0.01)
	assert.Equal(t, "pay_kes_1", store.recorded["loan-1"])
}

func TestChipperPayoutAlreadySettled(t *testing.T) {
	disb := &fakeDisb{}
	svc := chipperService(disb, &fakeAlerts{}, &fakeChipperPayouts{}, &fakeChipperStore{settled: true})

	require.NoError(t, svc.ProcessChipperEvent(context.Background(), payoutEvent(t, chipper.EventPayoutCompleted, chipperPayout(chipper.PayoutCompleted, "loan-1"))))
	assert.Empty(t, disb.statuses)
	assert.False(t, disb.notifiedComplete, "a repeated webhook must not notify again")
}

func TestChipperPayoutFailed(t *testing.T) {
	tests := []struct {
		name        string
		reference   string
		direct      bool
		reissueErr  error
		wantReissue []string
		wantStatus  string
	}{
		{"direct first failure repeats from the balance", "loan-1", true, nil, []string{"loan-1_fiat"}, ""},
		{"repeat fails to be created", "loan-1", true, errors.New("rejected"), nil, yellowcard.DisbursementFailed},
		{"repeated payout failed", "loan-1_fiat", true, nil, nil, yellowcard.DisbursementFailed},
		{"balance-funded payout failed", "loan-1", false, nil, nil, yellowcard.DisbursementFailed},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			disb, alerts := &fakeDisb{}, &fakeAlerts{}
			payouts := &fakeChipperPayouts{reissueErr: tt.reissueErr}
			store := &fakeChipperStore{direct: tt.direct}
			svc := chipperService(disb, alerts, payouts, store)

			require.NoError(t, svc.ProcessChipperEvent(context.Background(), payoutEvent(t, chipper.EventPayoutFailed, chipperPayout(chipper.PayoutFailed, tt.reference))))

			assert.Equal(t, tt.wantReissue, payouts.reissued)
			assert.Equal(t, tt.wantStatus, disb.last())
			assert.Equal(t, tt.wantStatus != "", disb.notifiedFailed)
			assert.Equal(t, 1, alerts.count)
			if tt.wantReissue != nil {
				assert.Equal(t, "pay_retry", store.recorded[tt.reference])
			}
		})
	}
}

type fakeChipperOrders struct{ refs []chipper.ResourceRef }

func (f *fakeChipperOrders) ApplyChipperOrder(_ context.Context, ref chipper.ResourceRef) error {
	f.refs = append(f.refs, ref)
	return nil
}

func TestChipperOrderEventsReachRepayments(t *testing.T) {
	orders := &fakeChipperOrders{}
	disb := &fakeDisb{}
	svc := chipperService(disb, &fakeAlerts{}, &fakeChipperPayouts{}, &fakeChipperStore{}).WithChipperOrders(orders)

	for _, ev := range []string{chipper.EventOrderFundsReceived, chipper.EventOrderCompleted, chipper.EventOrderFailed, chipper.EventOrderExpired} {
		data := []byte(`{"id":"ord_1","status":"` + ev + `","externalReference":"rp-LN1-1"}`)
		require.NoError(t, svc.ProcessChipperEvent(context.Background(), chipper.Event{Event: ev, Data: data}))
	}
	require.Len(t, orders.refs, 4)
	assert.Equal(t, "ord_1", orders.refs[0].ID)
	assert.Equal(t, "rp-LN1-1", orders.refs[0].ExternalReference)
	assert.Empty(t, disb.statuses)
}

func TestChipperOrderEventsIgnoredWithoutRepayments(t *testing.T) {
	svc := chipperService(&fakeDisb{}, &fakeAlerts{}, &fakeChipperPayouts{}, &fakeChipperStore{})
	ev := chipper.Event{Event: chipper.EventOrderCompleted, Data: []byte(`{"id":"ord_1","status":"completed"}`)}
	require.NoError(t, svc.ProcessChipperEvent(context.Background(), ev))
}

type fakeChipperDisputes struct {
	events   []string
	disputes []*chipper.Dispute
}

func (f *fakeChipperDisputes) ApplyChipperDispute(_ context.Context, event string, d *chipper.Dispute) error {
	f.events = append(f.events, event)
	f.disputes = append(f.disputes, d)
	return nil
}

const disputeJSON = `{"id":"dsp_1","collectionId":"col_1","amount":"2578.10","currency":"KES","category":"chargeback","status":"open","createdAt":"2026-11-28T10:00:00Z","updatedAt":"2026-11-28T10:00:00Z"}`

func TestChipperDisputeEventsReachRepayments(t *testing.T) {
	disputes, alerts := &fakeChipperDisputes{}, &fakeAlerts{}
	svc := chipperService(&fakeDisb{}, alerts, &fakeChipperPayouts{}, &fakeChipperStore{}).WithChipperDisputes(disputes)

	for _, ev := range []string{chipper.EventDisputeCreated, chipper.EventDisputeUpdated, chipper.EventDisputeClosed} {
		require.NoError(t, svc.ProcessChipperEvent(context.Background(), chipper.Event{Event: ev, Data: []byte(disputeJSON)}))
	}
	assert.Equal(t, []string{chipper.EventDisputeCreated, chipper.EventDisputeUpdated, chipper.EventDisputeClosed}, disputes.events)
	assert.Equal(t, "col_1", disputes.disputes[0].CollectionID)
	assert.Zero(t, alerts.count, "the rail raises its own alerts")
}

// A dispute holds money on our balance, so it reaches ops even with no
// repayment rail to claim it.
func TestChipperDisputeAlertsWithoutRepayments(t *testing.T) {
	alerts := &fakeAlerts{}
	svc := chipperService(&fakeDisb{}, alerts, &fakeChipperPayouts{}, &fakeChipperStore{})
	require.NoError(t, svc.ProcessChipperEvent(context.Background(), chipper.Event{Event: chipper.EventDisputeCreated, Data: []byte(disputeJSON)}))
	assert.Equal(t, 1, alerts.count)

	err := svc.ProcessChipperEvent(context.Background(), chipper.Event{Event: chipper.EventDisputeCreated, Data: []byte(`[]`)})
	require.Error(t, err)
	assert.Equal(t, 2, alerts.count, "an unreadable dispute still pages")
}

// An order's own USDC payout to the treasury also fires payout.completed; it
// must not be applied as a loan disbursement.
func TestChipperCryptoPayoutLegIgnored(t *testing.T) {
	disb, store := &fakeDisb{}, &fakeChipperStore{}
	svc := chipperService(disb, &fakeAlerts{}, &fakeChipperPayouts{}, store)

	code := chipper.MethodUSDCStellar
	p := chipperPayout(chipper.PayoutCompleted, "rp-LN1-1")
	p.To.Code = &code
	require.NoError(t, svc.ProcessChipperEvent(context.Background(), payoutEvent(t, chipper.EventPayoutCompleted, p)))
	assert.Empty(t, disb.statuses)
	assert.Empty(t, store.recorded)
}

func depositEvent(t *testing.T, typ, status, hash string) chipper.Event {
	t.Helper()
	data, err := json.Marshal(map[string]any{
		"id": "col_1", "type": typ, "status": status,
		"details": map[string]string{"hash": hash, "chain": "Stellar", "tag": "2802699348"},
	})
	require.NoError(t, err)
	return chipper.Event{Event: chipper.EventCollectionCompleted, Data: data}
}

func TestChipperDepositReleasesPendingPayout(t *testing.T) {
	rel := chipper.Release{LoanID: "loan-1", Reference: "loan-1", DepositTxHash: "abc123"}
	tests := []struct {
		name         string
		event        func(t *testing.T) chipper.Event
		wantReleased int
	}{
		{"matching deposit", func(t *testing.T) chipper.Event { return depositEvent(t, "crypto", "completed", "ABC123") }, 1},
		{"other deposit", func(t *testing.T) chipper.Event { return depositEvent(t, "crypto", "completed", "other") }, 0},
		{"not completed", func(t *testing.T) chipper.Event { return depositEvent(t, "crypto", "processing", "abc123") }, 0},
		{"not crypto", func(t *testing.T) chipper.Event { return depositEvent(t, "mobile_money", "completed", "abc123") }, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			payouts := &fakeChipperPayouts{}
			store := &fakeChipperStore{pending: []chipper.Release{rel}}
			svc := chipperService(&fakeDisb{}, &fakeAlerts{}, payouts, store)

			require.NoError(t, svc.ProcessChipperEvent(context.Background(), tt.event(t)))
			assert.Len(t, payouts.released, tt.wantReleased)
			if tt.wantReleased > 0 {
				assert.Equal(t, "pay_released", store.recorded["loan-1"])
			}
		})
	}
}

func TestChipperNotConfigured(t *testing.T) {
	err := NewService(&fakeDisb{}, &fakeAlerts{}, nil, nil).ProcessChipperEvent(context.Background(), chipper.Event{Event: chipper.EventPayoutCompleted})
	require.Error(t, err)
}

func TestChipperReconcilerTick(t *testing.T) {
	disb := &fakeDisb{}
	payouts := &fakeChipperPayouts{payouts: map[string]*chipper.Payout{
		"pay_done":    chipperPayout(chipper.PayoutCompleted, "loan-2"),
		"pay_running": chipperPayout(chipper.PayoutProcessing, "loan-3"),
	}}
	store := &fakeChipperStore{
		direct:  true,
		pending: []chipper.Release{{LoanID: "loan-1", Reference: "loan-1", DepositTxHash: "abc"}},
		inFlight: []ChipperInFlight{
			{SequenceID: "loan-2", LoanID: "loan-2", PayoutID: "pay_done"},
			{SequenceID: "loan-3", LoanID: "loan-3", PayoutID: "pay_running"},
			{SequenceID: "loan-4", LoanID: "loan-4", PayoutID: "pay_missing"},
		},
	}
	svc := chipperService(disb, &fakeAlerts{}, payouts, store)
	now := time.Date(2026, 11, 27, 12, 0, 0, 0, time.UTC)
	r := NewChipperReconciler(svc, ChipperReconcilerConfig{})
	r.now = func() time.Time { return now }

	r.Tick(context.Background())

	assert.Len(t, payouts.released, 1, "the pending deposit is released")
	assert.Equal(t, []string{yellowcard.DisbursementComplete}, disb.statuses, "only the terminal in-flight payout is applied")
	assert.Equal(t, now.Add(-10*time.Minute), store.cutoff)
}

func fonbnkEvent(status, orderType string) fonbnk.WebhookEvent {
	var ev fonbnk.WebhookEvent
	ev.Data.Order = fonbnk.WebhookOrder{ID: "ord_1", MerchantOrderParams: "loan-1", Type: orderType, Status: status}
	ev.Data.Order.Payout.Cashout.AmountAfterFees = 985
	return ev
}

type fakeSettlement struct{ settled bool }

func (f fakeSettlement) DisbursementSettled(context.Context, string) (bool, error) {
	return f.settled, nil
}

func TestProcessFonbnkEvent(t *testing.T) {
	tests := []struct {
		name       string
		status     string
		orderType  string
		settled    bool
		wantStatus string
		wantDone   bool
		wantFailed bool
		wantAlert  bool
	}{
		{"payout successful", fonbnk.StatusPayoutSuccessful, fonbnk.OrderTypeOffRamp, false, yellowcard.DisbursementComplete, true, false, false},
		{"payout successful repeated", fonbnk.StatusPayoutSuccessful, fonbnk.OrderTypeOffRamp, true, "", false, false, false},
		{"payout failed awaits refund", fonbnk.StatusPayoutFailed, fonbnk.OrderTypeOffRamp, false, yellowcard.DisbursementRefundPending, false, false, false},
		{"refund successful", fonbnk.StatusRefundSuccessful, fonbnk.OrderTypeOffRamp, false, yellowcard.DisbursementFailed, false, true, true},
		{"refund failed", fonbnk.StatusRefundFailed, fonbnk.OrderTypeOffRamp, false, "", false, false, true},
		{"deposit expired is not final", fonbnk.StatusDepositExpired, fonbnk.OrderTypeOffRamp, false, "", false, false, false},
		{"on-ramp ignored", fonbnk.StatusPayoutSuccessful, fonbnk.OrderTypeOnRamp, false, "", false, false, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			disb, alerts := &fakeDisb{}, &fakeAlerts{}
			svc := NewService(disb, alerts, nil, nil).WithSettlement(fakeSettlement{settled: tt.settled})

			require.NoError(t, svc.ProcessFonbnkEvent(context.Background(), fonbnkEvent(tt.status, tt.orderType)))
			assert.Equal(t, tt.wantStatus, disb.last())
			assert.Equal(t, tt.wantDone, disb.notifiedComplete)
			assert.Equal(t, tt.wantFailed, disb.notifiedFailed)
			assert.Equal(t, tt.wantAlert, alerts.count > 0)
			if tt.wantDone {
				require.NotNil(t, disb.completion)
				assert.InDelta(t, 985.0, disb.completion.ConvertedAmountLocal, 1e-9)
				assert.Zero(t, disb.completion.ServiceFeeAmountUSD, "fonbnk takes its fee out of the payout")
			}
		})
	}
}
