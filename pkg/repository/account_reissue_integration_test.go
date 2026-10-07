//go:build integration

package repository

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/Shamba-Records-Limited/microvault/pkg/models"
)

func TestAccountRepository_ReissueConflict(t *testing.T) {
	repo, err := NewAccountRepository(testDB)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	u := newUser(t, "254711000301")
	acc := &models.Account{UserID: u.ID, PublicKey: "GCONFLICT" + u.ID, AccountIndex: 100, Status: "active"}
	if err := repo.Create(ctx, acc); err != nil {
		t.Fatalf("create account: %v", err)
	}
	if err := repo.UpdateChainStatus(ctx, acc.ID, models.ChainStatusConflict); err != nil {
		t.Fatal(err)
	}
	next, err := repo.EnsureAccountIndexIntegrity(ctx, 500)
	if err != nil {
		t.Fatal(err)
	}

	derive := func(index int) (string, error) { return fmt.Sprintf("GREISSUED%d", index), nil }
	moved, err := repo.ReissueConflict(ctx, acc.ID, derive)
	if err != nil {
		t.Fatalf("ReissueConflict: %v", err)
	}
	if int64(moved.AccountIndex) != next {
		t.Errorf("index = %d, want the next free index %d", moved.AccountIndex, next)
	}
	if moved.PublicKey != fmt.Sprintf("GREISSUED%d", next) {
		t.Errorf("address = %s, want the one derived for the new index", moved.PublicKey)
	}
	if moved.ChainStatus != models.ChainStatusPending || moved.ChainAttempts != 0 || moved.ChainCheckedAt != nil {
		t.Errorf("row not reset to pending: %+v", moved)
	}

	if _, err := repo.ReissueConflict(ctx, acc.ID, derive); !errors.Is(err, ErrAccountNotConflict) {
		t.Errorf("re-issuing a pending row = %v, want ErrAccountNotConflict", err)
	}

	if err := repo.UpdateChainStatus(ctx, acc.ID, models.ChainStatusConflict); err != nil {
		t.Fatal(err)
	}
	failing := func(int) (string, error) { return "", errors.New("derive failed") }
	if _, err := repo.ReissueConflict(ctx, acc.ID, failing); err == nil {
		t.Fatal("a derive failure must fail the re-issue")
	}
	again, _ := repo.GetByID(ctx, acc.ID)
	if again.AccountIndex != moved.AccountIndex || again.ChainStatus != models.ChainStatusConflict {
		t.Errorf("a failed re-issue must leave the row untouched: %+v", again)
	}
	after, err := repo.EnsureAccountIndexIntegrity(ctx, 0)
	if err != nil {
		t.Fatal(err)
	}
	if after <= next+1 {
		t.Errorf("the failed attempt's index must stay spent: next is %d, want > %d", after, next+1)
	}
}
