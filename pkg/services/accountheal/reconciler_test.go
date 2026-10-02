package accountheal

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"testing"
	"time"

	"github.com/samber/oops"
	"github.com/stretchr/testify/assert"

	"github.com/Shamba-Records-Limited/microvault/pkg/account"
	"github.com/Shamba-Records-Limited/microvault/pkg/models"
	"github.com/Shamba-Records-Limited/microvault/pkg/repository"
	"github.com/Shamba-Records-Limited/microvault/pkg/stellar"
)

type fakeEnsurer struct{ err error }

func (f fakeEnsurer) EnsureOnChainAccount(context.Context, int, string) error { return f.err }

type check struct {
	attempts int
	at       time.Time
}

type fakeRepo struct {
	checks   []check
	statuses []string
}

func (f *fakeRepo) GetDueChainHeals(context.Context, repository.ChainHealDue, int) ([]*models.Account, error) {
	return nil, nil
}

func (f *fakeRepo) RecordChainCheck(_ context.Context, _ string, attempts int, at time.Time) error {
	f.checks = append(f.checks, check{attempts, at})
	return nil
}

func (f *fakeRepo) UpdateChainStatus(_ context.Context, _ string, status string) error {
	f.statuses = append(f.statuses, status)
	return nil
}

type fakeAlerts struct{ subjects []string }

func (f *fakeAlerts) AlertOps(subject, _ string) error {
	f.subjects = append(f.subjects, subject)
	return nil
}

var testNow = time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC)

func newTestDriver(err error) (*Driver, *fakeRepo, *fakeAlerts) {
	repo := &fakeRepo{}
	alerts := &fakeAlerts{}
	return &Driver{
		ensurer:     fakeEnsurer{err: err},
		repo:        repo,
		alerts:      alerts,
		logger:      slog.New(slog.DiscardHandler),
		maxAttempts: 3,
		now:         func() time.Time { return testNow },
	}, repo, alerts
}

func acct(status string, attempts int) *models.Account {
	return &models.Account{ID: "acct-1", PublicKey: "GABC", AccountIndex: 7, ChainStatus: status, ChainAttempts: attempts}
}

func TestDrive_HealResetsAttempts(t *testing.T) {
	d, repo, alerts := newTestDriver(nil)

	d.Drive(t.Context(), acct(models.ChainStatusFailed, 2))

	assert.Equal(t, []check{{0, testNow}}, repo.checks)
	assert.Empty(t, repo.statuses, "EnsureOnChainAccount confirms the status itself")
	assert.Empty(t, alerts.subjects)
}

func TestDrive_FailureSpendsAnAttemptAndMarksFailed(t *testing.T) {
	for _, status := range []string{models.ChainStatusPending, models.ChainStatusUnknown} {
		t.Run(status, func(t *testing.T) {
			d, repo, _ := newTestDriver(errors.New("create failed"))

			d.Drive(t.Context(), acct(status, 0))

			assert.Equal(t, []check{{1, testNow}}, repo.checks)
			assert.Equal(t, []string{models.ChainStatusFailed}, repo.statuses)
		})
	}
}

func TestDrive_AlreadyFailedIsNotRewritten(t *testing.T) {
	d, repo, _ := newTestDriver(errors.New("create failed"))

	d.Drive(t.Context(), acct(models.ChainStatusFailed, 1))

	assert.Equal(t, []check{{2, testNow}}, repo.checks)
	assert.Empty(t, repo.statuses)
}

func TestDrive_AlertsOnceWhenReachingTheCap(t *testing.T) {
	tests := []struct {
		name      string
		attempts  int
		wantAlert bool
	}{
		{name: "below the cap", attempts: 0, wantAlert: false},
		{name: "reaching the cap", attempts: 2, wantAlert: true},
		{name: "past the cap", attempts: 3, wantAlert: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d, _, alerts := newTestDriver(errors.New("create failed"))

			d.Drive(t.Context(), acct(models.ChainStatusFailed, tt.attempts))

			if tt.wantAlert {
				assert.Equal(t, []string{"Stellar account creation exhausted"}, alerts.subjects)
			} else {
				assert.Empty(t, alerts.subjects)
			}
		})
	}
}

func TestDrive_UnfixableFailureGoesStraightToTheCap(t *testing.T) {
	for _, cause := range []error{
		oops.Wrap(stellar.ErrTransactionRejectedPermanent),
		fmt.Errorf("ensure: %w", account.ErrDerivedAddressMismatch),
	} {
		t.Run(cause.Error(), func(t *testing.T) {
			d, repo, alerts := newTestDriver(cause)

			d.Drive(t.Context(), acct(models.ChainStatusPending, 0))

			assert.Equal(t, []check{{3, testNow}}, repo.checks)
			assert.Equal(t, []string{"Stellar account creation exhausted"}, alerts.subjects)
		})
	}
}

func TestDrive_OutageDefersWithoutSpendingAnAttempt(t *testing.T) {
	d, repo, alerts := newTestDriver(fmt.Errorf("ensure: %w", account.ErrChainCheckUnavailable))

	d.Drive(t.Context(), acct(models.ChainStatusUnknown, 1))

	assert.Equal(t, []check{{1, testNow}}, repo.checks)
	assert.Empty(t, repo.statuses, "an outage must not mark a healthy account failed")
	assert.Empty(t, alerts.subjects)
}

func TestDrive_ConflictIsLeftAlone(t *testing.T) {
	d, repo, alerts := newTestDriver(oops.Wrap(account.ErrDerivationConflict))

	d.Drive(t.Context(), acct(models.ChainStatusFailed, 1))

	assert.Empty(t, repo.checks)
	assert.Empty(t, repo.statuses)
	assert.Empty(t, alerts.subjects)
}
