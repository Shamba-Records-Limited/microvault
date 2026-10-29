package compliance

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Shamba-Records-Limited/microvault/pkg/models"
	"github.com/Shamba-Records-Limited/microvault/pkg/repository"
)

type fakeSettingRepo struct {
	rows []*models.ScreeningProviderSetting
}

func (f *fakeSettingRepo) Propose(_ context.Context, s *models.ScreeningProviderSetting) error {
	for _, r := range f.rows {
		if r.Status == models.ScreeningSettingProposed {
			return repository.ErrScreeningProposalOpen
		}
	}
	s.ID = "set-" + string(rune('a'+len(f.rows)))
	f.rows = append(f.rows, s)
	return nil
}

func (f *fakeSettingRepo) Get(_ context.Context, id string) (*models.ScreeningProviderSetting, error) {
	for _, r := range f.rows {
		if r.ID == id {
			return r, nil
		}
	}
	return nil, repository.ErrScreeningProposalNotOpen
}

func (f *fakeSettingRepo) Decide(_ context.Context, id, status, actor, note string, at time.Time) error {
	r, err := f.Get(context.Background(), id)
	if err != nil || r.Status != models.ScreeningSettingProposed {
		return repository.ErrScreeningProposalNotOpen
	}
	r.Status, r.DecidedBy, r.DecidedAt, r.DecisionNote = status, &actor, &at, note
	return nil
}

func (f *fakeSettingRepo) Current(context.Context) (*models.ScreeningProviderSetting, error) {
	var cur *models.ScreeningProviderSetting
	for _, r := range f.rows {
		if r.Status == models.ScreeningSettingApproved {
			cur = r
		}
	}
	return cur, nil
}

func (f *fakeSettingRepo) List(context.Context, int) ([]*models.ScreeningProviderSetting, error) {
	out := slices.Clone(f.rows)
	slices.Reverse(out)
	return out, nil
}

func validateKnown(names []string) ([]string, error) {
	for _, n := range names {
		if n != "elliptic" && n != "merkle" {
			return nil, errors.New("screening provider " + n + " has no credentials in this deployment")
		}
	}
	return names, nil
}

func TestScreeningSettingsTwoPersonChange(t *testing.T) {
	repo := &fakeSettingRepo{}
	svc := NewScreeningSettings(repo, validateKnown, []string{"elliptic"})
	ctx := context.Background()

	st, err := svc.State(ctx, 10)
	require.NoError(t, err)
	assert.Equal(t, []string{"elliptic"}, st.InForce)
	assert.True(t, st.FromEnv)

	_, err = svc.Propose(ctx, []string{"elliptic"}, "same", "GA")
	require.ErrorIs(t, err, ErrNoProviderChange)
	_, err = svc.Propose(ctx, []string{"elliptic", "merkle"}, " ", "GA")
	require.ErrorIs(t, err, ErrReasonRequired)
	_, err = svc.Propose(ctx, []string{"didit"}, "try", "GA")
	require.Error(t, err)

	p, err := svc.Propose(ctx, []string{"elliptic", "merkle"}, "Stellar USDC confirmed at Merkle", "GA")
	require.NoError(t, err)
	_, err = svc.Propose(ctx, []string{"merkle"}, "another", "GB")
	require.ErrorIs(t, err, repository.ErrScreeningProposalOpen)

	require.ErrorIs(t, svc.Approve(ctx, p.ID, "GA", ""), ErrSelfApproval)
	providers, err := svc.CurrentProviders(ctx)
	require.NoError(t, err)
	assert.Nil(t, providers, "nothing changes until a second operator approves")

	require.NoError(t, svc.Approve(ctx, p.ID, "GB", "checked"))
	providers, err = svc.CurrentProviders(ctx)
	require.NoError(t, err)
	assert.Equal(t, []string{"elliptic", "merkle"}, providers)
	require.ErrorIs(t, svc.Approve(ctx, p.ID, "GC", ""), repository.ErrScreeningProposalNotOpen)

	st, err = svc.State(ctx, 10)
	require.NoError(t, err)
	assert.False(t, st.FromEnv)
	assert.Nil(t, st.Open)
}

func TestScreeningSettingsReject(t *testing.T) {
	repo := &fakeSettingRepo{}
	svc := NewScreeningSettings(repo, validateKnown, []string{"elliptic"})
	ctx := context.Background()
	p, err := svc.Propose(ctx, []string{"merkle"}, "trial", "GA")
	require.NoError(t, err)
	require.NoError(t, svc.Reject(ctx, p.ID, "GA", "withdrawn"), "the proposer may withdraw")
	providers, err := svc.CurrentProviders(ctx)
	require.NoError(t, err)
	assert.Nil(t, providers)
	_, err = svc.Propose(ctx, []string{"merkle"}, "trial again", "GA")
	require.NoError(t, err, "a closed change frees the slot")
}
