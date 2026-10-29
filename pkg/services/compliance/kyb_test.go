package compliance

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Shamba-Records-Limited/microvault/pkg/compliance/didit"
	"github.com/Shamba-Records-Limited/microvault/pkg/models"
	"github.com/Shamba-Records-Limited/microvault/pkg/repository"
)

type fakeVerifier struct {
	calls int
	got   [3]string
}

func (f *fakeVerifier) CreateKYBSession(_ context.Context, workflowID, vendorData, callback string) (*didit.Session, error) {
	f.calls++
	f.got = [3]string{workflowID, vendorData, callback}
	return &didit.Session{ID: "bs_" + vendorData, URL: "https://verify.didit.me/session/bs_" + vendorData}, nil
}

type fakeKYBRepo struct {
	cps    *fakeRepo
	events []*models.CounterpartyKYBEvent
}

func (f *fakeKYBRepo) SetSession(_ context.Context, id, provider, sessionID, url, status string, at time.Time) error {
	cp, ok := f.cps.counterparties[id]
	if !ok {
		return repository.ErrCounterpartyNotFound
	}
	cp.KYBProvider, cp.KYBSessionID, cp.KYBSessionURL, cp.KYBSessionStatus, cp.KYBSessionAt = provider, &sessionID, url, status, &at
	return nil
}

func (f *fakeKYBRepo) RecordEvent(_ context.Context, e *models.CounterpartyKYBEvent) error {
	cp, ok := f.cps.counterparties[e.CounterpartyID]
	if !ok {
		return repository.ErrCounterpartyNotFound
	}
	if cp.KYBSessionID == nil || *cp.KYBSessionID != e.SessionID {
		return repository.ErrKYBSessionMismatch
	}
	f.events = append(f.events, e)
	if e.Status != "" {
		cp.KYBSessionStatus = e.Status
	}
	return nil
}

func (f *fakeKYBRepo) ListEvents(context.Context, string, int) ([]*models.CounterpartyKYBEvent, error) {
	return f.events, nil
}

type kybAlerts struct{ subjects []string }

func (a *kybAlerts) AlertOps(_ context.Context, subject, _ string) error {
	a.subjects = append(a.subjects, subject)
	return nil
}

func newKYBFixture() (*KYB, *fakeRepo, *fakeKYBRepo, *fakeVerifier, *kybAlerts) {
	cps := newFakeRepo()
	cps.counterparties["cp-1"] = &models.Counterparty{ID: "cp-1", LegalName: "Lender Ltd", KYBStatus: models.CounterpartyKYBPending}
	repo := &fakeKYBRepo{cps: cps}
	v := &fakeVerifier{}
	a := &kybAlerts{}
	k := NewKYB(KYBDeps{Counterparties: cps, Repo: repo, Verifier: v, WorkflowID: "wf-kyb", Callback: "https://portal/done", Alerts: a, Logger: slog.New(slog.DiscardHandler)})
	return k, cps, repo, v, a
}

func event(status, session string) *didit.Event {
	ev := &didit.Event{Event: "status.updated"}
	ev.Data.SessionKind = "business"
	ev.Data.VendorData = "cp-1"
	ev.Data.SessionID = session
	ev.Data.Status = status
	return ev
}

func TestKYBStartCreatesThenReusesAnOpenSession(t *testing.T) {
	k, cps, _, v, _ := newKYBFixture()
	url, err := k.Start(context.Background(), "cp-1")
	require.NoError(t, err)
	assert.Equal(t, "https://verify.didit.me/session/bs_cp-1", url)
	assert.Equal(t, [3]string{"wf-kyb", "cp-1", "https://portal/done"}, v.got)
	assert.Equal(t, didit.StatusNotStarted, cps.counterparties["cp-1"].KYBSessionStatus)

	_, err = k.Start(context.Background(), "cp-1")
	require.NoError(t, err)
	assert.Equal(t, 1, v.calls, "an open session is reused")

	cps.counterparties["cp-1"].KYBSessionStatus = didit.StatusDeclined
	_, err = k.Start(context.Background(), "cp-1")
	require.NoError(t, err)
	assert.Equal(t, 2, v.calls, "a closed session is replaced")
}

func TestKYBStartRefusals(t *testing.T) {
	k, cps, _, _, _ := newKYBFixture()
	cps.counterparties["cp-1"].KYBStatus = models.CounterpartyKYBApproved
	_, err := k.Start(context.Background(), "cp-1")
	assert.ErrorIs(t, err, ErrKYBAlreadyApproved)

	_, err = NewKYB(KYBDeps{}).Start(context.Background(), "cp-1")
	assert.ErrorIs(t, err, ErrKYBNotConfigured)
}

func TestKYBHandleEvent(t *testing.T) {
	k, cps, repo, _, a := newKYBFixture()
	_, err := k.Start(context.Background(), "cp-1")
	require.NoError(t, err)

	require.NoError(t, k.HandleEvent(context.Background(), event(didit.StatusInReview, "bs_cp-1"), []byte(`{"a":1}`)))
	require.NoError(t, k.HandleEvent(context.Background(), event(didit.StatusApproved, "bs_cp-1"), []byte(`{"a":2}`)))
	assert.Equal(t, didit.StatusApproved, cps.counterparties["cp-1"].KYBSessionStatus)
	assert.Len(t, repo.events, 2)
	assert.Equal(t, []string{SubjectKYBNeedsReview, SubjectKYBVerified}, a.subjects)
	assert.Equal(t, models.CounterpartyKYBPending, cps.counterparties["cp-1"].KYBStatus, "Didit never approves the lender by itself")

	require.NoError(t, k.HandleEvent(context.Background(), event(didit.StatusDeclined, "bs_old"), []byte(`{}`)), "a superseded session is dropped, not retried")
	assert.Len(t, repo.events, 2)

	user := event(didit.StatusApproved, "bs_cp-1")
	user.Data.SessionKind = "user"
	require.NoError(t, k.HandleEvent(context.Background(), user, []byte(`{}`)))
	assert.Len(t, repo.events, 2, "personal KYC sessions are not lender KYB")
}

func TestApproveKYBRequiresDiditApproval(t *testing.T) {
	repo := newFakeRepo()
	repo.counterparties["cp-1"] = &models.Counterparty{ID: "cp-1", KYBStatus: models.CounterpartyKYBPending, KYBSessionStatus: didit.StatusInReview}
	svc := NewService(Deps{Repo: repo, RequireKYBVerified: true})
	assert.ErrorIs(t, svc.ApproveKYB(context.Background(), "cp-1", "GACTOR"), ErrKYBNotVerified)

	repo.counterparties["cp-1"].KYBSessionStatus = didit.StatusApproved
	require.NoError(t, svc.ApproveKYB(context.Background(), "cp-1", "GACTOR"))
	assert.Equal(t, models.CounterpartyKYBApproved, repo.counterparties["cp-1"].KYBStatus)
}

type fakeRescreens struct {
	got []string
	n   int64
}

func (f *fakeRescreens) QueueCounterparty(_ context.Context, provider, event, id string, _ []byte) (int64, error) {
	f.got = append(f.got, provider+"/"+event+"/"+id)
	return f.n, nil
}

func TestKYBDataUpdatedQueuesRescreens(t *testing.T) {
	k, cps, repo, _, a := newKYBFixture()
	rs := &fakeRescreens{n: 2}
	k.rescreens = rs
	_, err := k.Start(context.Background(), "cp-1")
	require.NoError(t, err)
	require.NoError(t, k.HandleEvent(context.Background(), event(didit.StatusApproved, "bs_cp-1"), []byte(`{}`)))
	a.subjects = nil

	ev := event("", "bs_cp-1")
	ev.Event = "data.updated"
	require.NoError(t, k.HandleEvent(context.Background(), ev, []byte(`{"aml":1}`)))
	assert.Equal(t, []string{"didit/data.updated/cp-1"}, rs.got)
	assert.Equal(t, []string{SubjectKYBDataChanged}, a.subjects)
	assert.Len(t, repo.events, 2, "the update joins the event trail")
	assert.Equal(t, didit.StatusApproved, cps.counterparties["cp-1"].KYBSessionStatus, "an update without a status keeps the session status")
}
