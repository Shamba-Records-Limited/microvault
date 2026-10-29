package compliance

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	pkgcompliance "github.com/Shamba-Records-Limited/microvault/pkg/compliance"
	"github.com/Shamba-Records-Limited/microvault/pkg/models"
	"github.com/Shamba-Records-Limited/microvault/pkg/repository"
)

type fakeDepositRepo struct {
	due      []*models.DepositScreening
	cp       *repository.DepositCounterparty
	results  map[string]*models.DepositScreening
	failures map[string]bool // id -> final
	next     map[string]time.Time
	alerted  []string
}

func newFakeDepositRepo(due ...*models.DepositScreening) *fakeDepositRepo {
	return &fakeDepositRepo{due: due, results: map[string]*models.DepositScreening{}, failures: map[string]bool{}, next: map[string]time.Time{}}
}

func (f *fakeDepositRepo) Enqueue(context.Context, *models.DepositScreening) error { return nil }
func (f *fakeDepositRepo) ListDue(context.Context, time.Time, int) ([]*models.DepositScreening, error) {
	return f.due, nil
}

func (f *fakeDepositRepo) RecordResult(_ context.Context, id string, r *models.DepositScreening) error {
	f.results[id] = r
	return nil
}

func (f *fakeDepositRepo) RecordAttemptFailed(_ context.Context, id, _ string, next time.Time, final bool) error {
	f.failures[id] = final
	f.next[id] = next
	return nil
}

func (f *fakeDepositRepo) MarkAlerted(_ context.Context, id string, _ time.Time) error {
	f.alerted = append(f.alerted, id)
	return nil
}

func (f *fakeDepositRepo) Counterparty(context.Context, string) (*repository.DepositCounterparty, error) {
	return f.cp, nil
}

type fakeTxScreener struct {
	res *pkgcompliance.Screening
	err error
	got pkgcompliance.TxScreenRequest
}

func (f *fakeTxScreener) ScreenTransaction(_ context.Context, req pkgcompliance.TxScreenRequest) (*pkgcompliance.Screening, error) {
	f.got = req
	return f.res, f.err
}

type recordingAlerts struct{ subjects []string }

func (r *recordingAlerts) AlertOps(_ context.Context, subject, _ string) error {
	r.subjects = append(r.subjects, subject)
	return nil
}

func newDepositScreener(repo *fakeDepositRepo, screener *fakeTxScreener, a *recordingAlerts) *DepositScreener {
	s := NewDepositScreener(DepositScreenerDeps{Repo: repo, Screener: screener, Alerts: a, Logger: slog.New(slog.DiscardHandler)})
	s.now = func() time.Time { return time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC) }
	return s
}

func deposit() *models.DepositScreening {
	return &models.DepositScreening{ID: "d1", TxHash: "abc", Depositor: "GLENDER", Receiver: "CVAULT"}
}

func TestDepositScreenerRecordsACleanDeposit(t *testing.T) {
	repo := newFakeDepositRepo(deposit())
	repo.cp = &repository.DepositCounterparty{AddressID: "addr-1", CustomerReference: "kyb-1"}
	screener := &fakeTxScreener{res: &pkgcompliance.Screening{Provider: "merkle", Verdict: pkgcompliance.VerdictApproved}}
	a := &recordingAlerts{}
	newDepositScreener(repo, screener, a).Tick(context.Background())

	require.Contains(t, repo.results, "d1")
	assert.Equal(t, "approved", repo.results["d1"].Verdict)
	assert.Equal(t, "addr-1", *repo.results["d1"].CounterpartyAddressID)
	assert.Equal(t, "kyb-1", screener.got.CustomerReference)
	assert.Equal(t, "CVAULT", screener.got.Recipient)
	assert.Empty(t, a.subjects)
}

func TestDepositScreenerAlertsOnABadVerdict(t *testing.T) {
	for verdict, subject := range map[pkgcompliance.Verdict]string{
		pkgcompliance.VerdictRejected: SubjectDepositSanctioned,
		pkgcompliance.VerdictReview:   SubjectDepositNeedsReview,
	} {
		repo := newFakeDepositRepo(deposit())
		a := &recordingAlerts{}
		newDepositScreener(repo, &fakeTxScreener{res: &pkgcompliance.Screening{Verdict: verdict}}, a).Tick(context.Background())
		assert.Equal(t, []string{subject}, a.subjects)
		assert.Equal(t, []string{"d1"}, repo.alerted)
	}
}

func TestDepositScreenerRetriesPendingAndErrors(t *testing.T) {
	for _, screener := range []*fakeTxScreener{
		{err: errors.New("provider down")},
		{res: &pkgcompliance.Screening{Verdict: pkgcompliance.VerdictPending}},
	} {
		repo := newFakeDepositRepo(deposit())
		a := &recordingAlerts{}
		s := newDepositScreener(repo, screener, a)
		s.Tick(context.Background())
		assert.Empty(t, repo.results, "nothing is recorded as screened")
		assert.False(t, repo.failures["d1"], "the first failure is not final")
		assert.Equal(t, s.now().Add(time.Minute), repo.next["d1"])
		assert.Empty(t, a.subjects)
	}
}

func TestDepositScreenerGivesUpAfterTheLastAttempt(t *testing.T) {
	d := deposit()
	d.Attempts = len(depositBackoff) - 1
	repo := newFakeDepositRepo(d)
	a := &recordingAlerts{}
	newDepositScreener(repo, &fakeTxScreener{err: errors.New("still down")}, a).Tick(context.Background())
	assert.True(t, repo.failures["d1"])
	assert.Equal(t, []string{SubjectDepositScreeningStuck}, a.subjects)
}
