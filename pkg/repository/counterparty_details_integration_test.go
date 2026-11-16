//go:build integration

package repository

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Shamba-Records-Limited/microvault/pkg/models"
)

func TestCounterpartyUpdateDetails(t *testing.T) {
	ctx := context.Background()
	cps, err := NewCounterpartyRepository(testDB)
	require.NoError(t, err)
	addr := newCounterpartyAddress(t, cps)
	require.NoError(t, cps.ApproveKYB(ctx, addr.CounterpartyID, "GACTOR"))

	d := CounterpartyDetails{
		LegalName: "Renamed Capital Ltd", RegistrationNumber: "PVT-9", Jurisdiction: "KE",
		AddressLine1: "Riverside Drive 14", City: "Nairobi", Region: "KE-30", PostalCode: "00100",
	}
	require.NoError(t, cps.UpdateDetails(ctx, addr.CounterpartyID, d))

	cp, err := cps.GetByID(ctx, addr.CounterpartyID)
	require.NoError(t, err)
	assert.Equal(t, "Renamed Capital Ltd", cp.LegalName)
	assert.Equal(t, "KE-30", cp.Region)
	assert.Equal(t, "00100", cp.PostalCode)
	assert.Equal(t, models.CounterpartyKYBApproved, cp.KYBStatus, "an edit leaves the approval alone")
	assert.Len(t, cp.Addresses, 1, "an edit leaves the addresses alone")

	require.ErrorIs(t, cps.UpdateDetails(ctx, uuid.NewString(), d), ErrCounterpartyNotFound)
}
