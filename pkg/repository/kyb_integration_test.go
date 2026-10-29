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

func TestKYBRepository(t *testing.T) {
	ctx := context.Background()
	cps, err := NewCounterpartyRepository(testDB)
	require.NoError(t, err)
	repo, err := NewKYBRepository(testDB)
	require.NoError(t, err)
	addr := newCounterpartyAddress(t, cps)
	cpID := addr.CounterpartyID
	session := "bs_" + uuid.NewString()[:8]

	require.ErrorIs(t, repo.RecordEvent(ctx, &models.CounterpartyKYBEvent{CounterpartyID: cpID, Provider: "didit", SessionID: session, Event: "status.updated", Status: "IN_PROGRESS", Payload: datatypes.JSON(`{}`)}),
		ErrKYBSessionMismatch, "no session yet")

	require.NoError(t, repo.SetSession(ctx, cpID, "didit", session, "https://verify.didit.me/x", "NOT_STARTED", time.Now()))
	require.ErrorIs(t, repo.SetSession(ctx, uuid.NewString(), "didit", "bs_other", "u", "NOT_STARTED", time.Now()), ErrCounterpartyNotFound)

	for _, st := range []string{"IN_PROGRESS", "APPROVED"} {
		require.NoError(t, repo.RecordEvent(ctx, &models.CounterpartyKYBEvent{
			CounterpartyID: cpID, Provider: "didit", SessionID: session, Event: "status.updated", Status: st, Payload: datatypes.JSON(`{"status":"` + st + `"}`),
		}))
	}
	cp, err := cps.GetByID(ctx, cpID)
	require.NoError(t, err)
	assert.Equal(t, "APPROVED", cp.KYBSessionStatus)
	assert.Equal(t, session, *cp.KYBSessionID)
	assert.Equal(t, "didit", cp.KYBProvider)

	events, err := repo.ListEvents(ctx, cpID, 10)
	require.NoError(t, err)
	require.Len(t, events, 2)
	assert.Equal(t, "APPROVED", events[0].Status, "newest first")
}
