package chippermon

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/govalues/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Shamba-Records-Limited/microvault/pkg/payment/chipper"
)

type recordingAlerts struct{ subjects []string }

func (r *recordingAlerts) AlertOps(_ context.Context, subject, _ string) error {
	r.subjects = append(r.subjects, subject)
	return nil
}

type fakeBalance struct {
	value float64
	err   error
}

func (f *fakeBalance) AvailableBalance(context.Context) (float64, error) { return f.value, f.err }

func TestFloatMonitor(t *testing.T) {
	bal := &fakeBalance{value: 120}
	al := &recordingAlerts{}
	now := time.Date(2026, 11, 27, 12, 0, 0, 0, time.UTC)
	m := NewFloatMonitor(bal, al, nil, FloatMonitorConfig{Threshold: decimal.MustParse("100")})
	m.now = func() time.Time { return now }
	ctx := context.Background()

	m.Check(ctx)
	assert.Empty(t, al.subjects, "above the threshold")

	bal.value = 80
	m.Check(ctx)
	m.Check(ctx)
	assert.Equal(t, []string{SubjectFloatLow}, al.subjects, "one alert per crossing")

	now = now.Add(6 * time.Hour)
	m.Check(ctx)
	assert.Equal(t, []string{SubjectFloatLow, SubjectFloatLow}, al.subjects, "repeats while low")

	bal.err = errors.New("chipper down")
	m.Check(ctx)
	assert.Len(t, al.subjects, 2, "an unreadable balance is not an alert")

	bal.err, bal.value = nil, 150
	m.Check(ctx)
	m.Check(ctx)
	assert.Equal(t, []string{SubjectFloatLow, SubjectFloatLow, SubjectFloatRecovered}, al.subjects)
}

type fakeLedger struct {
	entries map[string][]chipper.LedgerEntry
	fail    map[string]bool
}

func (f *fakeLedger) ListLedger(_ context.Context, filter chipper.LedgerFilter) (*chipper.Page[chipper.LedgerEntry], error) {
	if f.fail[filter.ReferenceID] {
		return nil, errors.New("503")
	}
	return &chipper.Page[chipper.LedgerEntry]{Data: f.entries[filter.ReferenceID]}, nil
}

type fakeFeeStore struct {
	payouts []SettledPayout
	since   time.Time
}

func (f *fakeFeeStore) SettledChipperPayouts(_ context.Context, since time.Time, _ int) ([]SettledPayout, error) {
	f.since = since
	return f.payouts, nil
}

func entry(typ, amount string) chipper.LedgerEntry {
	return chipper.LedgerEntry{Type: typ, Amount: decimal.MustParse(amount)}
}

func TestFeeReconciler(t *testing.T) {
	ledger := &fakeLedger{
		entries: map[string][]chipper.LedgerEntry{
			"pay_ok":       {entry(chipper.LedgerPayoutDebit, "-7.678911"), entry(chipper.LedgerFeeDebit, "-0.115184")},
			"pay_off":      {entry(chipper.LedgerFeeDebit, "-0.50")},
			"pay_refunded": {entry(chipper.LedgerFeeDebit, "-0.115184"), entry(chipper.LedgerFeeRefund, "0.115184")},
		},
		fail: map[string]bool{"pay_unread": true},
	}
	store := &fakeFeeStore{payouts: []SettledPayout{
		{LoanID: "l1", LoanReference: "MV-1", PayoutID: "pay_ok", ServiceFeeCents: 12},
		{LoanID: "l2", LoanReference: "MV-2", PayoutID: "pay_off", ServiceFeeCents: 12},
		{LoanID: "l3", LoanReference: "MV-3", PayoutID: "pay_refunded", ServiceFeeCents: 12},
		{LoanID: "l4", LoanReference: "MV-4", PayoutID: "pay_unread", ServiceFeeCents: 12},
	}}
	al := &recordingAlerts{}
	now := time.Date(2026, 11, 27, 12, 0, 0, 0, time.UTC)
	r := NewFeeReconciler(ledger, store, al, nil, FeeReconcilerConfig{})
	r.now = func() time.Time { return now }

	mismatches, err := r.Reconcile(context.Background())
	require.NoError(t, err)
	require.Len(t, mismatches, 2)
	assert.Equal(t, "MV-2", mismatches[0].LoanReference, "a fee off by more than a cent")
	assert.Equal(t, "MV-3", mismatches[1].LoanReference, "a refunded fee nets to zero")
	assert.Equal(t, []string{SubjectFeeMismatch}, al.subjects, "one alert per run")
	assert.Equal(t, now.Add(-25*time.Hour), store.since, "the window overlaps the interval")
}
