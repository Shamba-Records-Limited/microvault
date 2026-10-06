//go:build integration

package repository

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stellar/go-stellar-sdk/keypair"

	"github.com/Shamba-Records-Limited/microvault/pkg/models"
)

func newCounterpartyAddress(t *testing.T, repo CounterpartyRepository) *models.CounterpartyAddress {
	t.Helper()
	ctx := context.Background()
	cp := &models.Counterparty{ID: uuid.NewString(), LegalName: "Test Ltd", EllipticCustomerReference: uuid.NewString()}
	if err := repo.Create(ctx, cp); err != nil {
		t.Fatal(err)
	}
	addr := &models.CounterpartyAddress{ID: uuid.NewString(), CounterpartyID: cp.ID, Address: keypair.MustRandom().Address()}
	if err := repo.AddAddress(ctx, addr); err != nil {
		t.Fatal(err)
	}
	return addr
}

func TestCounterpartyRepository_FreezeAndExitDeadline(t *testing.T) {
	repo, err := NewCounterpartyRepository(testDB)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	addr := newCounterpartyAddress(t, repo)

	if err := repo.RevokeAddress(ctx, addr.ID, "admin", "lapsed"); err != nil {
		t.Fatal(err)
	}
	revoking, _ := repo.ListAddressesNeedingOnchainRevoke(ctx, 50)
	if !containsAddr(revoking, addr.ID) {
		t.Fatal("revoked address should be in the revoke queue")
	}

	if err := repo.FreezeAddress(ctx, addr.ID, "admin", "authorities"); err != nil {
		t.Fatal(err)
	}
	got, _ := repo.GetAddressByID(ctx, addr.ID)
	if got.FrozenAt == nil || got.RevokedAt == nil {
		t.Fatalf("freeze should set frozen_at and keep revoked_at: %+v", got)
	}
	revoking, _ = repo.ListAddressesNeedingOnchainRevoke(ctx, 50)
	freezing, _ := repo.ListAddressesNeedingOnchainFreeze(ctx, 50)
	if containsAddr(revoking, addr.ID) || !containsAddr(freezing, addr.ID) {
		t.Fatal("a freeze intent belongs to the freeze queue only")
	}

	deadline := time.Now().Add(3 * 24 * time.Hour).UTC().Truncate(time.Second)
	if err := repo.SetExitDeadline(ctx, addr.Address, &deadline); err != nil {
		t.Fatal(err)
	}
	closing, _ := repo.ListExitWindowsClosing(ctx, time.Now().Add(7*24*time.Hour), 1, 50)
	if !containsAddr(closing, addr.ID) {
		t.Fatal("deadline inside seven days should be listed")
	}
	if err := repo.SetExitWarningLevel(ctx, addr.ID, 1); err != nil {
		t.Fatal(err)
	}
	closing, _ = repo.ListExitWindowsClosing(ctx, time.Now().Add(7*24*time.Hour), 1, 50)
	if containsAddr(closing, addr.ID) {
		t.Fatal("an address already warned at this level should not be listed again")
	}

	if err := repo.SetExitDeadline(ctx, addr.Address, &deadline); err != nil {
		t.Fatal(err)
	}
	got, _ = repo.GetAddressByID(ctx, addr.ID)
	if got.ExitWarningLevel != 1 {
		t.Fatal("re-setting the same deadline must not reset the warning level")
	}
	later := deadline.Add(24 * time.Hour)
	if err := repo.SetExitDeadline(ctx, addr.Address, &later); err != nil {
		t.Fatal(err)
	}
	got, _ = repo.GetAddressByID(ctx, addr.ID)
	if got.ExitWarningLevel != 0 || !got.ExitDeadline.Equal(later) {
		t.Fatalf("a moved deadline resets the warning level: %+v", got)
	}

	if err := repo.SetExitDeadline(ctx, "GNOTACOUNTERPARTY", nil); err != nil {
		t.Fatal("an unknown address is not an error")
	}

	if err := repo.ApproveAddress(ctx, addr.ID, "admin", "cleared"); err != nil {
		t.Fatal(err)
	}
	got, _ = repo.GetAddressByID(ctx, addr.ID)
	if got.RevokedAt != nil || got.FrozenAt != nil {
		t.Fatal("re-approval clears revoke and freeze intents")
	}
	revoking, _ = repo.ListAddressesNeedingOnchainRevoke(ctx, 50)
	freezing, _ = repo.ListAddressesNeedingOnchainFreeze(ctx, 50)
	if containsAddr(revoking, addr.ID) || containsAddr(freezing, addr.ID) {
		t.Fatal("a re-approved address must not be revoked or frozen again")
	}
}

func containsAddr(addrs []*models.CounterpartyAddress, id string) bool {
	for _, a := range addrs {
		if a.ID == id {
			return true
		}
	}
	return false
}
