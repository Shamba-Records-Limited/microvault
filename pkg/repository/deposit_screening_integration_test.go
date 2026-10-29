//go:build integration

package repository

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/datatypes"

	"github.com/Shamba-Records-Limited/microvault/pkg/models"
)

func TestDepositScreeningRepository(t *testing.T) {
	ctx := context.Background()
	cps, err := NewCounterpartyRepository(testDB)
	require.NoError(t, err)
	repo, err := NewDepositScreeningRepository(testDB)
	require.NoError(t, err)
	addr := newCounterpartyAddress(t, cps)

	event := "evt-" + uuid.NewString()[:8]
	d := &models.DepositScreening{EventID: event, TxHash: "abc", Ledger: 150, Depositor: addr.Address, Receiver: "CVAULT"}
	require.NoError(t, repo.Enqueue(ctx, d))
	require.NoError(t, repo.Enqueue(ctx, &models.DepositScreening{EventID: event, TxHash: "abc", Ledger: 150, Depositor: addr.Address}),
		"a repeat of the same event is ignored")

	due, err := repo.ListDue(ctx, time.Now().Add(time.Second), 100)
	require.NoError(t, err)
	var mine []*models.DepositScreening
	for _, x := range due {
		if x.EventID == event {
			mine = append(mine, x)
		}
	}
	require.Len(t, mine, 1, "queued once")
	id := mine[0].ID

	cp, err := repo.Counterparty(ctx, addr.Address)
	require.NoError(t, err)
	require.NotNil(t, cp)
	assert.Equal(t, addr.ID, cp.AddressID)
	assert.NotEmpty(t, cp.CustomerReference)
	none, err := repo.Counterparty(ctx, "GNOTALENDER")
	require.NoError(t, err)
	assert.Nil(t, none)

	require.NoError(t, repo.RecordAttemptFailed(ctx, id, "provider down", time.Now().Add(time.Hour), false))
	due, err = repo.ListDue(ctx, time.Now().Add(time.Second), 100)
	require.NoError(t, err)
	for _, x := range due {
		assert.NotEqual(t, id, x.ID, "a retried deposit waits for its next attempt")
	}

	score := 4.0
	require.NoError(t, repo.RecordResult(ctx, id, &models.DepositScreening{
		Provider: "elliptic+merkle", Verdict: "review", RiskScore: &score,
		RawPayload: datatypes.JSON(`{"merkle":{}}`), CounterpartyAddressID: &addr.ID,
	}))
	require.NoError(t, repo.MarkAlerted(ctx, id, time.Now()))

	var got models.DepositScreening
	require.NoError(t, testDB.Where("id = ?", id).First(&got).Error)
	assert.Equal(t, models.DepositScreeningScreened, got.Status)
	assert.Equal(t, 2, got.Attempts)
	assert.Equal(t, "review", got.Verdict)
	assert.Equal(t, "elliptic+merkle", got.Provider)
	assert.NotNil(t, got.ScreenedAt)
	assert.NotNil(t, got.AlertedAt)
	assert.Empty(t, got.LastError)

	require.NoError(t, repo.RecordAttemptFailed(ctx, id, "late", time.Now(), true))
	require.NoError(t, testDB.Where("id = ?", id).First(&got).Error)
	assert.Equal(t, models.DepositScreeningScreened, got.Status, "a screened deposit is never reopened")
}
