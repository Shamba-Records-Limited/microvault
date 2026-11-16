//go:build integration

package repository

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Shamba-Records-Limited/microvault/pkg/models"
)

func TestCounterpartyAddressUnfreezeAndReinstate(t *testing.T) {
	ctx := context.Background()
	cps, err := NewCounterpartyRepository(testDB)
	require.NoError(t, err)
	addr := newCounterpartyAddress(t, cps)
	require.NoError(t, cps.ApproveAddress(ctx, addr.ID, "GCOMP", "cleared", time.Now().Add(90*24*time.Hour)))

	require.NoError(t, cps.FreezeAddress(ctx, addr.ID, "GCOMP", "court order"))
	require.NoError(t, cps.SetOnchainState(ctx, addr.ID, models.OnchainStateFrozen))

	require.NoError(t, cps.UnfreezeAddress(ctx, addr.ID))
	got, err := cps.GetAddressByID(ctx, addr.ID)
	require.NoError(t, err)
	assert.Nil(t, got.FrozenAt)
	assert.NotNil(t, got.RevokedAt, "lifting a freeze leaves the address revoked")
	assert.Equal(t, models.OnchainStateRevoked, got.OnchainState)
	assert.ErrorIs(t, cps.UnfreezeAddress(ctx, addr.ID), ErrCounterpartyAddressNotFound, "nothing to lift twice")

	require.NoError(t, cps.ReinstateAddress(ctx, addr.ID))
	got, err = cps.GetAddressByID(ctx, addr.ID)
	require.NoError(t, err)
	assert.Nil(t, got.RevokedAt)
	assert.Nil(t, got.OverrideReason)
	assert.Equal(t, models.OnchainStatePending, got.OnchainState)
	assert.ErrorIs(t, cps.ReinstateAddress(ctx, uuid.NewString()), ErrCounterpartyAddressNotFound)
}
