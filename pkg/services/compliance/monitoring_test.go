package compliance

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/stellar/go-stellar-sdk/keypair"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	pkgcompliance "github.com/Shamba-Records-Limited/microvault/pkg/compliance"
	"github.com/Shamba-Records-Limited/microvault/pkg/models"
	"github.com/Shamba-Records-Limited/microvault/pkg/repository"
)

type fakeSignalRepo struct {
	known    map[string]repository.MonitoredAddress
	queued   []*models.MonitoringSignal
	keys     map[string]bool
	due      []*models.MonitoringSignal
	results  map[string]string
	failures map[string]bool
	alerted  []string
}

func newFakeSignalRepo() *fakeSignalRepo {
	return &fakeSignalRepo{known: map[string]repository.MonitoredAddress{}, keys: map[string]bool{}, results: map[string]string{}, failures: map[string]bool{}}
}

func (f *fakeSignalRepo) MatchAddresses(_ context.Context, addrs []string) ([]repository.MonitoredAddress, error) {
	var out []repository.MonitoredAddress
	for _, a := range addrs {
		if m, ok := f.known[a]; ok {
			out = append(out, m)
		}
	}
	return out, nil
}

func (f *fakeSignalRepo) CounterpartyAddresses(_ context.Context, id string) ([]repository.MonitoredAddress, error) {
	var out []repository.MonitoredAddress
	for _, m := range f.known {
		if m.CounterpartyID == id {
			out = append(out, m)
		}
	}
	return out, nil
}

func (f *fakeSignalRepo) Enqueue(_ context.Context, signals []*models.MonitoringSignal) (int64, error) {
	var n int64
	for _, s := range signals {
		addr := ""
		if s.CounterpartyAddressID != nil {
			addr = *s.CounterpartyAddressID
		}
		k := s.Provider + s.EventKey + addr
		if f.keys[k] {
			continue
		}
		f.keys[k] = true
		f.queued = append(f.queued, s)
		n++
	}
	return n, nil
}

func (f *fakeSignalRepo) ListDue(context.Context, time.Time, int) ([]*models.MonitoringSignal, error) {
	return f.due, nil
}

func (f *fakeSignalRepo) RecordResult(_ context.Context, id, verdict, _ string, _ time.Time) error {
	f.results[id] = verdict
	return nil
}

func (f *fakeSignalRepo) RecordAttemptFailed(_ context.Context, id, _ string, _ time.Time, final bool) error {
	f.failures[id] = final
	return nil
}

func (f *fakeSignalRepo) MarkAlerted(_ context.Context, id string, _ time.Time) error {
	f.alerted = append(f.alerted, id)
	return nil
}

type fakeRescreener struct {
	res *pkgcompliance.Screening
	err error
	got []string
}

func (f *fakeRescreener) Rescreen(_ context.Context, id string) (*pkgcompliance.Screening, error) {
	f.got = append(f.got, id)
	return f.res, f.err
}

func newMonitoring(repo *fakeSignalRepo, r *fakeRescreener, a *recordingAlerts) *Monitoring {
	m := NewMonitoring(MonitoringDeps{Repo: repo, Rescreener: r, Alerts: a, Logger: slog.New(slog.DiscardHandler)})
	m.now = func() time.Time { return time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC) }
	return m
}

func TestHandleMerkleWebhookQueuesLenderAddressesOnce(t *testing.T) {
	repo := newFakeSignalRepo()
	lender, other := keypair.MustRandom().Address(), keypair.MustRandom().Address()
	repo.known[lender] = repository.MonitoredAddress{AddressID: "addr-1", CounterpartyID: "cp-1", Address: lender}
	a := &recordingAlerts{}
	m := newMonitoring(repo, &fakeRescreener{}, a)

	body := []byte(`{"event_type":"risk","data":{"identifier":"` + lender + `","counterparty":"` + other + `"}}`)
	require.NoError(t, m.HandleMerkleWebhook(context.Background(), body))
	require.NoError(t, m.HandleMerkleWebhook(context.Background(), body), "a retried delivery is queued once")
	require.Len(t, repo.queued, 1)
	assert.Equal(t, "addr-1", *repo.queued[0].CounterpartyAddressID)
	assert.Equal(t, models.MonitoringSignalQueued, repo.queued[0].Status)
	assert.Empty(t, a.subjects)
}

func TestHandleMerkleWebhookUnmatchedAndReports(t *testing.T) {
	repo := newFakeSignalRepo()
	a := &recordingAlerts{}
	m := newMonitoring(repo, &fakeRescreener{}, a)

	require.NoError(t, m.HandleMerkleWebhook(context.Background(), []byte(`{"event_type":"report:address.generated"}`)))
	assert.Empty(t, repo.queued, "report downloads are ignored")

	body := []byte(`{"event_type":"risk","data":{"identifier":"0xabc"}}`)
	require.NoError(t, m.HandleMerkleWebhook(context.Background(), body))
	require.NoError(t, m.HandleMerkleWebhook(context.Background(), body))
	require.Len(t, repo.queued, 1)
	assert.Equal(t, models.MonitoringSignalUnmatched, repo.queued[0].Status)
	assert.Equal(t, []string{SubjectMonitoringUnmatched}, a.subjects, "alerted once per delivery")

	assert.Error(t, m.HandleMerkleWebhook(context.Background(), []byte(`nope`)))
}

func signal(attempts int) *models.MonitoringSignal {
	addr := "addr-1"
	return &models.MonitoringSignal{ID: "sig-1", Provider: "merkle", Event: "risk", CounterpartyAddressID: &addr, Attempts: attempts}
}

func TestMonitoringTickAlertsOnEveryOutcome(t *testing.T) {
	tests := []struct {
		verdict pkgcompliance.Verdict
		subject string
	}{
		{pkgcompliance.VerdictRejected, SubjectMonitoringRejected},
		{pkgcompliance.VerdictReview, SubjectMonitoringFlagged},
		{pkgcompliance.VerdictApproved, SubjectMonitoringFlagged},
	}
	for _, tt := range tests {
		t.Run(string(tt.verdict), func(t *testing.T) {
			repo := newFakeSignalRepo()
			repo.due = []*models.MonitoringSignal{signal(0)}
			r := &fakeRescreener{res: &pkgcompliance.Screening{Verdict: tt.verdict, Provider: "merkle"}}
			a := &recordingAlerts{}
			newMonitoring(repo, r, a).Tick(context.Background())
			assert.Equal(t, []string{"addr-1"}, r.got)
			assert.Equal(t, string(tt.verdict), repo.results["sig-1"])
			assert.Equal(t, []string{tt.subject}, a.subjects)
			assert.Equal(t, []string{"sig-1"}, repo.alerted)
		})
	}
}

func TestMonitoringTickRetriesThenGivesUp(t *testing.T) {
	repo := newFakeSignalRepo()
	repo.due = []*models.MonitoringSignal{signal(0)}
	a := &recordingAlerts{}
	newMonitoring(repo, &fakeRescreener{res: &pkgcompliance.Screening{Verdict: pkgcompliance.VerdictPending}}, a).Tick(context.Background())
	assert.False(t, repo.failures["sig-1"], "pending is retried")
	assert.Empty(t, a.subjects)

	repo.due = []*models.MonitoringSignal{signal(len(screeningBackoff) - 1)}
	newMonitoring(repo, &fakeRescreener{err: errors.New("provider down")}, a).Tick(context.Background())
	assert.True(t, repo.failures["sig-1"])
	assert.Equal(t, []string{SubjectMonitoringStuck}, a.subjects)
	assert.Empty(t, repo.results)
}
