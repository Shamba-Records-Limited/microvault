package compliance

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Shamba-Records-Limited/microvault/pkg/models"
)

type onchainWriterTestErr string

func (e onchainWriterTestErr) Error() string { return string(e) }
func assertErr(msg string) error             { return onchainWriterTestErr(msg) }

func testNow() time.Time { return time.Now() }

type fakeSigner struct {
	allowErr    error
	disallowErr error
	allowed     []string
	disallowed  []string
}

func (f *fakeSigner) AllowDepositor(_ context.Context, address string) error {
	f.allowed = append(f.allowed, address)
	return f.allowErr
}

func (f *fakeSigner) DisallowDepositor(_ context.Context, address string) error {
	f.disallowed = append(f.disallowed, address)
	return f.disallowErr
}

func newTestWriter(repo *fakeRepo, signer *fakeSigner) *OnchainWriter {
	return NewOnchainWriter(OnchainWriterDeps{Repo: repo, Signer: signer, Logger: slog.New(slog.DiscardHandler)})
}

func TestOnchainWriter_ProcessAllows(t *testing.T) {
	t.Run("submits allow_depositor and confirms onchain_state on success", func(t *testing.T) {
		repo := newFakeRepo()
		addr := &models.CounterpartyAddress{
			ID: "addr-1", Address: testAddress(),
			Status: models.AddressStatusApproved, OnchainState: models.OnchainStatePending,
		}
		repo.addresses[addr.ID] = addr
		signer := &fakeSigner{}

		newTestWriter(repo, signer).processAllows(context.Background())

		require.Len(t, signer.allowed, 1)
		assert.Equal(t, addr.Address, signer.allowed[0])
		assert.Equal(t, models.OnchainStateApproved, addr.OnchainState)
	})

	t.Run("a failed submission leaves the row pending for a retry", func(t *testing.T) {
		repo := newFakeRepo()
		addr := &models.CounterpartyAddress{
			ID: "addr-1", Address: testAddress(),
			Status: models.AddressStatusApproved, OnchainState: models.OnchainStatePending,
		}
		repo.addresses[addr.ID] = addr
		signer := &fakeSigner{allowErr: assertErr("rpc down")}

		newTestWriter(repo, signer).processAllows(context.Background())

		assert.Equal(t, models.OnchainStatePending, addr.OnchainState, "must not be marked approved when the chain call failed")
	})

	t.Run("addresses not in pending onchain_state are left alone", func(t *testing.T) {
		repo := newFakeRepo()
		already := &models.CounterpartyAddress{ID: "addr-1", Address: testAddress(), Status: models.AddressStatusApproved, OnchainState: models.OnchainStateApproved}
		notApproved := &models.CounterpartyAddress{ID: "addr-2", Address: testAddress(), Status: models.AddressStatusReview, OnchainState: models.OnchainStateAbsent}
		repo.addresses[already.ID] = already
		repo.addresses[notApproved.ID] = notApproved
		signer := &fakeSigner{}

		newTestWriter(repo, signer).processAllows(context.Background())

		assert.Empty(t, signer.allowed)
	})
}

func TestOnchainWriter_ProcessRevokes(t *testing.T) {
	t.Run("submits disallow_depositor and confirms onchain_state on success", func(t *testing.T) {
		repo := newFakeRepo()
		revokedAt := testNow()
		addr := &models.CounterpartyAddress{
			ID: "addr-1", Address: testAddress(),
			Status: models.AddressStatusApproved, OnchainState: models.OnchainStateApproved,
			RevokedAt: &revokedAt,
		}
		repo.addresses[addr.ID] = addr
		signer := &fakeSigner{}

		newTestWriter(repo, signer).processRevokes(context.Background())

		require.Len(t, signer.disallowed, 1)
		assert.Equal(t, models.OnchainStateRevoked, addr.OnchainState)
	})

	t.Run("addresses with no revocation intent are left alone", func(t *testing.T) {
		repo := newFakeRepo()
		addr := &models.CounterpartyAddress{ID: "addr-1", Address: testAddress(), OnchainState: models.OnchainStateApproved}
		repo.addresses[addr.ID] = addr
		signer := &fakeSigner{}

		newTestWriter(repo, signer).processRevokes(context.Background())

		assert.Empty(t, signer.disallowed)
	})

	t.Run("a failed submission leaves onchain_state unconfirmed for a retry", func(t *testing.T) {
		repo := newFakeRepo()
		revokedAt := testNow()
		addr := &models.CounterpartyAddress{
			ID: "addr-1", Address: testAddress(),
			OnchainState: models.OnchainStateApproved, RevokedAt: &revokedAt,
		}
		repo.addresses[addr.ID] = addr
		signer := &fakeSigner{disallowErr: assertErr("rpc down")}

		newTestWriter(repo, signer).processRevokes(context.Background())

		assert.Equal(t, models.OnchainStateApproved, addr.OnchainState, "must not be marked revoked when the chain call failed")
	})
}
