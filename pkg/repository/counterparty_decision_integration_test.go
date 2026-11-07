//go:build integration

package repository

import (
	"context"
	"errors"
	"testing"

	"github.com/Shamba-Records-Limited/microvault/pkg/models"
)

func TestCounterpartyRepository_DecisionsOnlyFromPending(t *testing.T) {
	repo, err := NewCounterpartyRepository(testDB)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	addr := newCounterpartyAddress(t, repo)

	if err := repo.ApproveKYB(ctx, addr.CounterpartyID, "admin"); err != nil {
		t.Fatal(err)
	}
	if err := repo.RejectKYB(ctx, addr.CounterpartyID, "admin"); !errors.Is(err, ErrCounterpartyKYBDecided) {
		t.Fatalf("rejecting an approved counterparty = %v, want ErrCounterpartyKYBDecided", err)
	}
	if err := repo.RejectKYB(ctx, "00000000-0000-0000-0000-000000000000", "admin"); !errors.Is(err, ErrCounterpartyNotFound) {
		t.Fatalf("unknown counterparty = %v, want ErrCounterpartyNotFound", err)
	}

	if err := repo.ApproveAddress(ctx, addr.ID, "admin", "cleared"); err != nil {
		t.Fatal(err)
	}
	if err := repo.RejectAddress(ctx, addr.ID, "admin", "second thoughts"); !errors.Is(err, ErrCounterpartyAddressDecided) {
		t.Fatalf("rejecting an approved address = %v, want ErrCounterpartyAddressDecided", err)
	}

	testDB.Exec(`UPDATE counterparty_addresses SET status = ?, onchain_state = ? WHERE id = ?`,
		models.AddressStatusReview, models.OnchainStateFrozen, addr.ID)
	if err := repo.ApproveAddress(ctx, addr.ID, "admin", "override"); !errors.Is(err, ErrCounterpartyAddressDecided) {
		t.Fatalf("approving an address frozen on-chain = %v, want ErrCounterpartyAddressDecided", err)
	}
	got, _ := repo.GetAddressByID(ctx, addr.ID)
	if got.OnchainState != models.OnchainStateFrozen {
		t.Fatalf("a refused approval must leave the freeze, got %s", got.OnchainState)
	}
}
