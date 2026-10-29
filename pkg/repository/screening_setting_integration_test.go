//go:build integration

package repository

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Shamba-Records-Limited/microvault/pkg/models"
)

func TestScreeningSettingRepository(t *testing.T) {
	ctx := context.Background()
	repo, err := NewScreeningSettingRepository(testDB)
	require.NoError(t, err)
	require.NoError(t, testDB.Exec("DELETE FROM screening_provider_settings").Error)

	cur, err := repo.Current(ctx)
	require.NoError(t, err)
	assert.Nil(t, cur)

	p := &models.ScreeningProviderSetting{Providers: "elliptic,merkle", Reason: "trial", Status: models.ScreeningSettingProposed, ProposedBy: "GA"}
	require.NoError(t, repo.Propose(ctx, p))
	err = repo.Propose(ctx, &models.ScreeningProviderSetting{Providers: "merkle", Reason: "x", Status: models.ScreeningSettingProposed, ProposedBy: "GB"})
	require.ErrorIs(t, err, ErrScreeningProposalOpen, "one waiting change at a time")

	require.Error(t, repo.Decide(ctx, p.ID, models.ScreeningSettingApproved, "GA", "", time.Now()), "the database refuses self-approval too")
	require.NoError(t, repo.Decide(ctx, p.ID, models.ScreeningSettingApproved, "GB", "ok", time.Now()))
	require.ErrorIs(t, repo.Decide(ctx, p.ID, models.ScreeningSettingRejected, "GC", "", time.Now()), ErrScreeningProposalNotOpen)

	cur, err = repo.Current(ctx)
	require.NoError(t, err)
	require.NotNil(t, cur)
	assert.Equal(t, []string{"elliptic", "merkle"}, cur.ProviderList())

	q := &models.ScreeningProviderSetting{Providers: "merkle", Reason: "withdrawn later", Status: models.ScreeningSettingProposed, ProposedBy: "GA"}
	require.NoError(t, repo.Propose(ctx, q))
	require.NoError(t, repo.Decide(ctx, q.ID, models.ScreeningSettingRejected, "GA", "withdrawn", time.Now()), "the proposer may withdraw")
	cur, err = repo.Current(ctx)
	require.NoError(t, err)
	assert.Equal(t, p.ID, cur.ID)

	list, err := repo.List(ctx, 10)
	require.NoError(t, err)
	require.Len(t, list, 2)
	assert.Equal(t, q.ID, list[0].ID)
	_, err = repo.Get(ctx, "00000000-0000-0000-0000-000000000000")
	assert.ErrorIs(t, err, ErrScreeningProposalNotOpen)
}
