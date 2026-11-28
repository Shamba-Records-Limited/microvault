package webhook

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Shamba-Records-Limited/microvault/pkg/payment/yellowcard"
)

type fakeYellowCardStore struct {
	inFlight []YellowCardInFlight
	cutoffs  []time.Time
	err      error
}

func (f *fakeYellowCardStore) InFlightYellowCardPayouts(_ context.Context, cutoff time.Time, _ int) ([]YellowCardInFlight, error) {
	f.cutoffs = append(f.cutoffs, cutoff)
	return f.inFlight, f.err
}

func ycReconciler(disb *fakeDisb, payments *fakePayments, settled bool, store *fakeYellowCardStore) (*YellowCardReconciler, *fakeAlerts) {
	alerts := &fakeAlerts{}
	svc := NewService(disb, alerts, nil, payments).WithSettlement(fakeSettlement{settled: settled}).WithYellowCardStore(store)
	r := NewYellowCardReconciler(svc, YellowCardReconcilerConfig{})
	r.now = func() time.Time { return time.Date(2026, 11, 28, 12, 0, 0, 0, time.UTC) }
	return r, alerts
}

func inFlightPayout() *fakeYellowCardStore {
	return &fakeYellowCardStore{inFlight: []YellowCardInFlight{{SequenceID: "seq-1", LoanID: "loan-1", PaymentID: "pay-1"}}}
}

func TestYellowCardReconciler_AppliesFinalOutcomes(t *testing.T) {
	cases := []struct {
		name      string
		status    string
		direct    bool
		want      string
		completed bool
		failed    bool
		alerts    int
	}{
		{"complete settles and notifies", yellowcard.StatusComplete, false, yellowcard.DisbursementComplete, true, false, 0},
		{"fiat failure fails and notifies", yellowcard.StatusFailed, false, yellowcard.DisbursementFailed, false, true, 1},
		{"direct failure awaits the refund", yellowcard.StatusFailed, true, yellowcard.DisbursementRefundPending, false, false, 1},
		{"expired is a failure", yellowcard.StatusExpired, false, yellowcard.DisbursementFailed, false, true, 1},
		{"cancelled is a failure", yellowcard.StatusCancelled, false, yellowcard.DisbursementFailed, false, true, 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			disb := &fakeDisb{direct: tc.direct}
			r, alerts := ycReconciler(disb, &fakePayments{details: &yellowcard.PaymentDetails{ID: "pay-1", Status: tc.status}}, false, inFlightPayout())

			r.Tick(context.Background())

			require.NotEmpty(t, disb.statuses)
			assert.Equal(t, tc.want, disb.last())
			assert.Equal(t, tc.completed, disb.notifiedComplete)
			assert.Equal(t, tc.failed, disb.notifiedFailed)
			assert.Equal(t, tc.alerts, alerts.count)
		})
	}
}

// A payout still in progress is left for its webhook: re-applying it would
// repeat side effects such as the pending-liquidity alert on every pass.
func TestYellowCardReconciler_LeavesPayoutsInProgress(t *testing.T) {
	for _, status := range []string{yellowcard.StatusProcessing, yellowcard.StatusPendingLiquidity, yellowcard.StatusPendingSettlement, yellowcard.StatusRefunded} {
		t.Run(status, func(t *testing.T) {
			disb := &fakeDisb{}
			r, alerts := ycReconciler(disb, &fakePayments{details: &yellowcard.PaymentDetails{Status: status}}, false, inFlightPayout())
			r.Tick(context.Background())
			assert.Empty(t, disb.statuses)
			assert.Zero(t, alerts.count)
		})
	}
}

// The webhook may have landed between listing and applying; the borrower is
// not told twice.
func TestYellowCardReconciler_SkipsASettledDisbursement(t *testing.T) {
	disb := &fakeDisb{}
	r, _ := ycReconciler(disb, &fakePayments{details: &yellowcard.PaymentDetails{Status: yellowcard.StatusComplete}}, true, inFlightPayout())
	r.Tick(context.Background())
	assert.Empty(t, disb.statuses)
	assert.False(t, disb.notifiedComplete)
}

func TestYellowCardReconciler_ToleratesFailures(t *testing.T) {
	t.Run("lookup fails", func(t *testing.T) {
		disb := &fakeDisb{}
		r, _ := ycReconciler(disb, &fakePayments{err: errors.New("yc down")}, false, inFlightPayout())
		r.Tick(context.Background())
		assert.Empty(t, disb.statuses)
	})
	t.Run("listing fails", func(t *testing.T) {
		disb := &fakeDisb{}
		store := inFlightPayout()
		store.err = errors.New("db down")
		r, _ := ycReconciler(disb, &fakePayments{details: &yellowcard.PaymentDetails{Status: yellowcard.StatusComplete}}, false, store)
		r.Tick(context.Background())
		assert.Empty(t, disb.statuses)
	})
}

func TestYellowCardReconciler_ListsPayoutsPastTheAge(t *testing.T) {
	store := &fakeYellowCardStore{}
	r, _ := ycReconciler(&fakeDisb{}, &fakePayments{}, false, store)
	r.Tick(context.Background())
	require.Len(t, store.cutoffs, 1)
	assert.Equal(t, r.now().Add(-15*time.Minute), store.cutoffs[0])
}

// A repeated completion webhook for a settled disbursement is ignored, so
// the borrower is notified once.
func TestProcessYellowCardEvent_RepeatedCompletionIsIgnored(t *testing.T) {
	disb := &fakeDisb{}
	svc := NewService(disb, &fakeAlerts{}, nil, nil).WithSettlement(fakeSettlement{settled: true})
	require.NoError(t, svc.ProcessYellowCardEvent(context.Background(), yellowcard.WebhookEvent{
		Event: yellowcard.EventSendComplete, SequenceID: "seq-1", PaymentID: "pay-1", Status: yellowcard.StatusComplete,
	}))
	assert.Empty(t, disb.statuses)
	assert.False(t, disb.notifiedComplete)
}
