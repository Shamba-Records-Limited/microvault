//go:build integration

package repository

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stellar/go-stellar-sdk/keypair"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/datatypes"

	"github.com/Shamba-Records-Limited/microvault/pkg/models"
)

func TestMonitoringSignalRepository(t *testing.T) {
	ctx := context.Background()
	cps, err := NewCounterpartyRepository(testDB)
	require.NoError(t, err)
	repo, err := NewMonitoringSignalRepository(testDB)
	require.NoError(t, err)

	screened := newCounterpartyAddress(t, cps)
	require.NoError(t, testDB.Model(&models.CounterpartyAddress{}).Where("id = ?", screened.ID).Update("status", models.AddressStatusApproved).Error)
	pending := &models.CounterpartyAddress{ID: uuid.NewString(), CounterpartyID: screened.CounterpartyID, Address: keypair.MustRandom().Address()}
	require.NoError(t, cps.AddAddress(ctx, pending))

	matched, err := repo.MatchAddresses(ctx, []string{screened.Address, pending.Address, keypair.MustRandom().Address()})
	require.NoError(t, err)
	require.Len(t, matched, 1, "addresses still waiting for KYB are not rescreened")
	assert.Equal(t, MonitoredAddress{AddressID: screened.ID, CounterpartyID: screened.CounterpartyID, Address: screened.Address}, matched[0])

	byCP, err := repo.CounterpartyAddresses(ctx, screened.CounterpartyID)
	require.NoError(t, err)
	assert.Equal(t, matched, byCP)

	key := uuid.NewString() + uuid.NewString()[:28]
	sig := func() *models.MonitoringSignal {
		return &models.MonitoringSignal{
			Provider: "merkle", Event: "risk", EventKey: key, CounterpartyID: &screened.CounterpartyID,
			CounterpartyAddressID: &screened.ID, Status: models.MonitoringSignalQueued, Payload: datatypes.JSON(`{}`),
		}
	}
	n, err := repo.Enqueue(ctx, []*models.MonitoringSignal{sig()})
	require.NoError(t, err)
	assert.EqualValues(t, 1, n)
	n, err = repo.Enqueue(ctx, []*models.MonitoringSignal{sig()})
	require.NoError(t, err)
	assert.EqualValues(t, 0, n, "a retried delivery is ignored")

	unmatched := func() *models.MonitoringSignal {
		return &models.MonitoringSignal{Provider: "merkle", EventKey: key, Status: models.MonitoringSignalUnmatched, Payload: datatypes.JSON(`{}`)}
	}
	n, err = repo.Enqueue(ctx, []*models.MonitoringSignal{unmatched()})
	require.NoError(t, err)
	assert.EqualValues(t, 1, n)
	n, err = repo.Enqueue(ctx, []*models.MonitoringSignal{unmatched()})
	require.NoError(t, err)
	assert.EqualValues(t, 0, n, "unmatched repeats are ignored too")

	mine := func() []*models.MonitoringSignal {
		due, err := repo.ListDue(ctx, time.Now().Add(time.Second), 500)
		require.NoError(t, err)
		var out []*models.MonitoringSignal
		for _, d := range due {
			if d.EventKey == key {
				out = append(out, d)
			}
		}
		return out
	}
	require.Len(t, mine(), 1, "only the queued signal is due")
	id := mine()[0].ID

	require.NoError(t, repo.RecordAttemptFailed(ctx, id, "provider down", time.Now().Add(time.Hour), false))
	assert.Empty(t, mine(), "a retried signal waits for its next attempt")

	require.NoError(t, repo.RecordResult(ctx, id, "rejected", "merkle", time.Now()))
	require.NoError(t, repo.MarkAlerted(ctx, id, time.Now()))
	var got models.MonitoringSignal
	require.NoError(t, testDB.First(&got, "id = ?", id).Error)
	assert.Equal(t, models.MonitoringSignalRescreened, got.Status)
	assert.Equal(t, 2, got.Attempts)
	assert.Equal(t, "rejected", got.Verdict)
	assert.Empty(t, got.LastError)
	assert.NotNil(t, got.AlertedAt)
	assert.NotNil(t, got.RescreenedAt)
}
