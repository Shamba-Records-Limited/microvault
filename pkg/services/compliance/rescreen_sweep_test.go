package compliance

import (
	"context"
	"encoding/json"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	pkgcompliance "github.com/Shamba-Records-Limited/microvault/pkg/compliance"
	"github.com/Shamba-Records-Limited/microvault/pkg/models"
)

func newTestSweep(repo *fakeRepo, screener *fakeScreener) *RescreenSweep {
	svc := NewService(Deps{Repo: repo, Screener: screener})
	return NewRescreenSweep(RescreenSweepDeps{Repo: repo, Service: svc, Logger: slog.New(slog.DiscardHandler)})
}

func TestRescreenSweep_Tick(t *testing.T) {
	t.Run("a lapsed approved address is marked expired and rescreened", func(t *testing.T) {
		repo := newFakeRepo()
		newApprovedCounterparty(repo, "cp-1", models.CounterpartyKYBApproved)
		past := time.Now().Add(-time.Hour)
		addr := &models.CounterpartyAddress{
			ID: "addr-1", CounterpartyID: "cp-1", Address: testAddress(),
			Status: models.AddressStatusApproved, OnchainState: models.OnchainStateApproved, ExpiresAt: &past,
		}
		repo.addresses[addr.ID] = addr
		screener := &fakeScreener{result: &pkgcompliance.Screening{Verdict: pkgcompliance.VerdictApproved, Raw: json.RawMessage(`{}`)}}

		newTestSweep(repo, screener).tick(context.Background())

		require.Len(t, screener.calls, 1, "the lapsed address must have been rescreened")
		assert.Equal(t, models.AddressStatusApproved, addr.Status, "a clean rescreen restores approved status")
		assert.Equal(t, models.OnchainStateApproved, addr.OnchainState, "already on-chain — must not be re-queued for a redundant write")
	})

	t.Run("an address not yet expired is left alone", func(t *testing.T) {
		repo := newFakeRepo()
		future := time.Now().Add(time.Hour)
		addr := &models.CounterpartyAddress{
			ID: "addr-1", Address: testAddress(),
			Status: models.AddressStatusApproved, ExpiresAt: &future,
		}
		repo.addresses[addr.ID] = addr
		screener := &fakeScreener{}

		newTestSweep(repo, screener).tick(context.Background())

		assert.Empty(t, screener.calls)
		assert.Equal(t, models.AddressStatusApproved, addr.Status)
	})

	t.Run("a failed rescreen leaves the address visibly expired, not silently stale", func(t *testing.T) {
		repo := newFakeRepo()
		newApprovedCounterparty(repo, "cp-1", models.CounterpartyKYBApproved)
		past := time.Now().Add(-time.Hour)
		addr := &models.CounterpartyAddress{
			ID: "addr-1", CounterpartyID: "cp-1", Address: testAddress(),
			Status: models.AddressStatusApproved, ExpiresAt: &past,
		}
		repo.addresses[addr.ID] = addr
		screener := &fakeScreener{err: assertErr("elliptic is down")}

		newTestSweep(repo, screener).tick(context.Background())

		assert.Equal(t, models.AddressStatusExpired, addr.Status, "must not silently keep showing a stale approved badge")
	})
}
