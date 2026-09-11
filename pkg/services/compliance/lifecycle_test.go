package compliance

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stellar/go-stellar-sdk/keypair"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	pkgcompliance "github.com/Shamba-Records-Limited/microvault/pkg/compliance"
	"github.com/Shamba-Records-Limited/microvault/pkg/models"
	"github.com/Shamba-Records-Limited/microvault/pkg/repository"
)

// fakeScreener is a scripted pkgcompliance.Screener — one result or error,
// returned for every call, with the requests it received recorded.
type fakeScreener struct {
	result *pkgcompliance.Screening
	err    error
	calls  []pkgcompliance.ScreenRequest
}

func (f *fakeScreener) ScreenAddress(_ context.Context, req pkgcompliance.ScreenRequest) (*pkgcompliance.Screening, error) {
	f.calls = append(f.calls, req)
	if f.err != nil {
		return nil, f.err
	}
	return f.result, nil
}

// fakeRepo is an in-memory repository.CounterpartyRepository — enough of
// the real interface's behavior (KYB gating, address lookup, screening
// records) to test the service without a database.
type fakeRepo struct {
	counterparties map[string]*models.Counterparty
	addresses      map[string]*models.CounterpartyAddress
	screenings     []*models.AddressScreening
}

func newFakeRepo() *fakeRepo {
	return &fakeRepo{
		counterparties: map[string]*models.Counterparty{},
		addresses:      map[string]*models.CounterpartyAddress{},
	}
}

var _ repository.CounterpartyRepository = (*fakeRepo)(nil)

func (f *fakeRepo) Create(_ context.Context, cp *models.Counterparty) error {
	if cp.ID == "" {
		cp.ID = "cp-" + cp.LegalName
	}
	f.counterparties[cp.ID] = cp
	return nil
}

func (f *fakeRepo) GetByID(_ context.Context, id string) (*models.Counterparty, error) {
	cp, ok := f.counterparties[id]
	if !ok {
		return nil, repository.ErrCounterpartyNotFound
	}
	return cp, nil
}

func (f *fakeRepo) List(context.Context, string, int, int) ([]*models.Counterparty, error) {
	return nil, nil
}
func (f *fakeRepo) Count(context.Context, string) (int64, error) { return 0, nil }

func (f *fakeRepo) ApproveKYB(_ context.Context, id, actor string) error {
	cp, ok := f.counterparties[id]
	if !ok {
		return repository.ErrCounterpartyNotFound
	}
	cp.KYBStatus = models.CounterpartyKYBApproved
	cp.KYBApprovedBy = &actor
	return nil
}

func (f *fakeRepo) RejectKYB(_ context.Context, id, _ string) error {
	cp, ok := f.counterparties[id]
	if !ok {
		return repository.ErrCounterpartyNotFound
	}
	cp.KYBStatus = models.CounterpartyKYBRejected
	return nil
}

func (f *fakeRepo) AddAddress(_ context.Context, addr *models.CounterpartyAddress) error {
	if addr.ID == "" {
		addr.ID = "addr-" + addr.Address
	}
	f.addresses[addr.ID] = addr
	return nil
}

func (f *fakeRepo) GetAddressByID(_ context.Context, id string) (*models.CounterpartyAddress, error) {
	addr, ok := f.addresses[id]
	if !ok {
		return nil, repository.ErrCounterpartyAddressNotFound
	}
	out := *addr
	if cp, ok := f.counterparties[addr.CounterpartyID]; ok {
		out.Counterparty = cp
	}
	return &out, nil
}

func (f *fakeRepo) ListAddressesByCounterparty(_ context.Context, counterpartyID string) ([]*models.CounterpartyAddress, error) {
	var out []*models.CounterpartyAddress
	for _, a := range f.addresses {
		if a.CounterpartyID == counterpartyID {
			out = append(out, a)
		}
	}
	return out, nil
}

func (f *fakeRepo) ListAddressesByStatus(_ context.Context, statuses []string, limit, offset int) ([]*models.CounterpartyAddress, error) {
	want := make(map[string]bool, len(statuses))
	for _, s := range statuses {
		want[s] = true
	}
	var out []*models.CounterpartyAddress
	for _, a := range f.addresses {
		if want[string(a.Status)] {
			out = append(out, a)
		}
	}
	return out, nil
}

func (f *fakeRepo) CountAddressesByStatus(_ context.Context, statuses []string) (int64, error) {
	addrs, _ := f.ListAddressesByStatus(context.Background(), statuses, 0, 0)
	return int64(len(addrs)), nil
}

func (f *fakeRepo) ApproveAddress(_ context.Context, id, actor, reason string) error {
	addr, ok := f.addresses[id]
	if !ok {
		return repository.ErrCounterpartyAddressNotFound
	}
	addr.Status = models.AddressStatusApproved
	addr.OnchainState = models.OnchainStatePending
	addr.ApprovedBy = &actor
	addr.OverrideReason = &reason
	return nil
}

func (f *fakeRepo) RejectAddress(_ context.Context, id, _, reason string) error {
	addr, ok := f.addresses[id]
	if !ok {
		return repository.ErrCounterpartyAddressNotFound
	}
	addr.Status = models.AddressStatusRejected
	addr.OverrideReason = &reason
	return nil
}

func (f *fakeRepo) RevokeAddress(_ context.Context, id, actor, reason string) error {
	addr, ok := f.addresses[id]
	if !ok {
		return repository.ErrCounterpartyAddressNotFound
	}
	addr.OnchainState = models.OnchainStateRevoked
	addr.RevokedBy = &actor
	addr.OverrideReason = &reason
	return nil
}

func (f *fakeRepo) ListScreeningsByAddress(_ context.Context, addressID string) ([]*models.AddressScreening, error) {
	var out []*models.AddressScreening
	for _, s := range f.screenings {
		if s.AddressID == addressID {
			out = append(out, s)
		}
	}
	return out, nil
}

func (f *fakeRepo) RecordScreening(_ context.Context, screening *models.AddressScreening, newStatus *models.CounterpartyAddressStatus, newOnchainState *models.CounterpartyAddressOnchainState, expiresAt *time.Time) error {
	if screening.ID == "" {
		screening.ID = "screening-" + screening.AddressID + "-" + time.Now().String()
	}
	f.screenings = append(f.screenings, screening)

	addr, ok := f.addresses[screening.AddressID]
	if !ok {
		return repository.ErrCounterpartyAddressNotFound
	}
	if newStatus != nil {
		addr.Status = *newStatus
	}
	if newOnchainState != nil {
		addr.OnchainState = *newOnchainState
	}
	addr.LastScreeningID = &screening.ID
	addr.ExpiresAt = expiresAt
	return nil
}

func (f *fakeRepo) ListAddressesNeedingOnchainAllow(_ context.Context, limit int) ([]*models.CounterpartyAddress, error) {
	var out []*models.CounterpartyAddress
	for _, a := range f.addresses {
		if a.Status == models.AddressStatusApproved && a.OnchainState == models.OnchainStatePending {
			out = append(out, a)
		}
	}
	return out, nil
}

func (f *fakeRepo) ListAddressesNeedingOnchainRevoke(_ context.Context, limit int) ([]*models.CounterpartyAddress, error) {
	var out []*models.CounterpartyAddress
	for _, a := range f.addresses {
		if a.RevokedAt != nil && a.OnchainState != models.OnchainStateRevoked {
			out = append(out, a)
		}
	}
	return out, nil
}

func (f *fakeRepo) SetOnchainState(_ context.Context, id string, state models.CounterpartyAddressOnchainState) error {
	addr, ok := f.addresses[id]
	if !ok {
		return repository.ErrCounterpartyAddressNotFound
	}
	addr.OnchainState = state
	return nil
}

func (f *fakeRepo) MarkExpiredAddresses(_ context.Context) (int64, error) {
	var touched int64
	now := time.Now()
	for _, a := range f.addresses {
		if a.Status == models.AddressStatusApproved && a.ExpiresAt != nil && a.ExpiresAt.Before(now) {
			a.Status = models.AddressStatusExpired
			touched++
		}
	}
	return touched, nil
}

func testAddress() string { return keypair.MustRandom().Address() }

func newApprovedCounterparty(repo *fakeRepo, id string, kyb models.CounterpartyKYBStatus) *models.Counterparty {
	cp := &models.Counterparty{ID: id, LegalName: "Test Co", EllipticCustomerReference: "kyb-" + id, KYBStatus: kyb}
	repo.counterparties[id] = cp
	return cp
}

func f64(v float64) *float64 { return &v }

func TestService_SubmitAddress(t *testing.T) {
	t.Run("rejects a malformed address before touching the repo or screener", func(t *testing.T) {
		repo := newFakeRepo()
		screener := &fakeScreener{}
		svc := NewService(Deps{Repo: repo, Screener: screener})

		_, err := svc.SubmitAddress(context.Background(), "cp-1", "not-a-stellar-address")
		assert.ErrorIs(t, err, ErrInvalidAddress)
		assert.Empty(t, screener.calls)
	})

	t.Run("saves the address without screening when KYB is not yet approved", func(t *testing.T) {
		repo := newFakeRepo()
		newApprovedCounterparty(repo, "cp-1", models.CounterpartyKYBPending)
		screener := &fakeScreener{}
		svc := NewService(Deps{Repo: repo, Screener: screener})

		addr, err := svc.SubmitAddress(context.Background(), "cp-1", testAddress())
		require.NoError(t, err)
		assert.Equal(t, models.AddressStatusPending, addr.Status)
		assert.Empty(t, screener.calls, "screening must not fire before KYB approval")
	})

	t.Run("screens immediately when KYB is already approved", func(t *testing.T) {
		repo := newFakeRepo()
		newApprovedCounterparty(repo, "cp-1", models.CounterpartyKYBApproved)
		screener := &fakeScreener{result: &pkgcompliance.Screening{Verdict: pkgcompliance.VerdictApproved, Raw: json.RawMessage(`{}`)}}
		svc := NewService(Deps{Repo: repo, Screener: screener})

		addr, err := svc.SubmitAddress(context.Background(), "cp-1", testAddress())
		require.NoError(t, err)
		require.Len(t, screener.calls, 1)
		assert.Equal(t, "kyb-cp-1", screener.calls[0].CustomerReference)

		stored := repo.addresses[addr.ID]
		assert.Equal(t, models.AddressStatusApproved, stored.Status)
	})
}

func TestService_ScreenAndRecord(t *testing.T) {
	cases := []struct {
		name              string
		verdict           pkgcompliance.Verdict
		wantStatus        models.CounterpartyAddressStatus
		wantExpiresSet    bool
		wantOnchainQueued bool
	}{
		{"approved sets approved status with an expiry and queues the on-chain write", pkgcompliance.VerdictApproved, models.AddressStatusApproved, true, true},
		{"rejected sets rejected status with no expiry", pkgcompliance.VerdictRejected, models.AddressStatusRejected, false, false},
		{"review sets review status with no expiry", pkgcompliance.VerdictReview, models.AddressStatusReview, false, false},
		{"unscreenable is a provisional approval with an expiry and queues the on-chain write", pkgcompliance.VerdictUnscreenable, models.AddressStatusApproved, true, true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo := newFakeRepo()
			newApprovedCounterparty(repo, "cp-1", models.CounterpartyKYBApproved)
			addr := &models.CounterpartyAddress{
				ID: "addr-1", CounterpartyID: "cp-1", Address: testAddress(),
				Status: models.AddressStatusPending, OnchainState: models.OnchainStateAbsent,
			}
			repo.addresses[addr.ID] = addr

			screener := &fakeScreener{result: &pkgcompliance.Screening{Verdict: tc.verdict, Raw: json.RawMessage(`{}`)}}
			svc := NewService(Deps{Repo: repo, Screener: screener})

			require.NoError(t, svc.ScreenAndRecord(context.Background(), addr.ID))

			assert.Equal(t, tc.wantStatus, addr.Status)
			if tc.wantOnchainQueued {
				assert.Equal(t, models.OnchainStatePending, addr.OnchainState)
			} else {
				assert.Equal(t, models.OnchainStateAbsent, addr.OnchainState, "no verdict outside approved/unscreenable should touch onchain_state")
			}
			if tc.wantExpiresSet {
				assert.NotNil(t, addr.ExpiresAt)
			} else {
				assert.Nil(t, addr.ExpiresAt)
			}
			require.Len(t, repo.screenings, 1)
			assert.Equal(t, string(tc.verdict), repo.screenings[0].Verdict)
		})
	}

	t.Run("a pending verdict never downgrades an address that was already approved", func(t *testing.T) {
		repo := newFakeRepo()
		newApprovedCounterparty(repo, "cp-1", models.CounterpartyKYBApproved)
		expires := time.Now().Add(24 * time.Hour)
		addr := &models.CounterpartyAddress{
			ID: "addr-1", CounterpartyID: "cp-1", Address: testAddress(),
			Status: models.AddressStatusApproved, ExpiresAt: &expires,
		}
		repo.addresses[addr.ID] = addr

		screener := &fakeScreener{result: &pkgcompliance.Screening{Verdict: pkgcompliance.VerdictPending, Raw: json.RawMessage(`{}`)}}
		svc := NewService(Deps{Repo: repo, Screener: screener})

		require.NoError(t, svc.ScreenAndRecord(context.Background(), addr.ID))

		assert.Equal(t, models.AddressStatusApproved, addr.Status, "status must be untouched by a still-pending rescreen")
		require.Len(t, repo.screenings, 1, "the pending screening is still recorded, per the append-only rule")
	})

	t.Run("re-approving an already on-chain-confirmed address does not re-queue an on-chain write", func(t *testing.T) {
		repo := newFakeRepo()
		newApprovedCounterparty(repo, "cp-1", models.CounterpartyKYBApproved)
		addr := &models.CounterpartyAddress{
			ID: "addr-1", CounterpartyID: "cp-1", Address: testAddress(),
			Status: models.AddressStatusApproved, OnchainState: models.OnchainStateApproved,
		}
		repo.addresses[addr.ID] = addr

		screener := &fakeScreener{result: &pkgcompliance.Screening{Verdict: pkgcompliance.VerdictApproved, Raw: json.RawMessage(`{}`)}}
		svc := NewService(Deps{Repo: repo, Screener: screener})

		require.NoError(t, svc.ScreenAndRecord(context.Background(), addr.ID))

		assert.Equal(t, models.OnchainStateApproved, addr.OnchainState, "a clean rescreen of an already-confirmed address must not re-queue a redundant write")
	})

	t.Run("re-approving a revoked address does not silently re-queue an on-chain write", func(t *testing.T) {
		repo := newFakeRepo()
		newApprovedCounterparty(repo, "cp-1", models.CounterpartyKYBApproved)
		addr := &models.CounterpartyAddress{
			ID: "addr-1", CounterpartyID: "cp-1", Address: testAddress(),
			Status: models.AddressStatusApproved, OnchainState: models.OnchainStateRevoked,
		}
		repo.addresses[addr.ID] = addr

		screener := &fakeScreener{result: &pkgcompliance.Screening{Verdict: pkgcompliance.VerdictApproved, Raw: json.RawMessage(`{}`)}}
		svc := NewService(Deps{Repo: repo, Screener: screener})

		require.NoError(t, svc.ScreenAndRecord(context.Background(), addr.ID))

		assert.Equal(t, models.OnchainStateRevoked, addr.OnchainState, "re-enabling a revoked address on-chain is a deliberate admin action, not an automated rescreen side effect")
	})

	t.Run("a null risk score is preserved, never coerced to zero", func(t *testing.T) {
		repo := newFakeRepo()
		newApprovedCounterparty(repo, "cp-1", models.CounterpartyKYBApproved)
		addr := &models.CounterpartyAddress{ID: "addr-1", CounterpartyID: "cp-1", Address: testAddress()}
		repo.addresses[addr.ID] = addr

		screener := &fakeScreener{result: &pkgcompliance.Screening{Verdict: pkgcompliance.VerdictReview, RiskScore: nil, Raw: json.RawMessage(`{}`)}}
		svc := NewService(Deps{Repo: repo, Screener: screener})

		require.NoError(t, svc.ScreenAndRecord(context.Background(), addr.ID))
		assert.Nil(t, repo.screenings[0].RiskScore)
	})

	t.Run("a real risk score round-trips unchanged", func(t *testing.T) {
		repo := newFakeRepo()
		newApprovedCounterparty(repo, "cp-1", models.CounterpartyKYBApproved)
		addr := &models.CounterpartyAddress{ID: "addr-1", CounterpartyID: "cp-1", Address: testAddress()}
		repo.addresses[addr.ID] = addr

		screener := &fakeScreener{result: &pkgcompliance.Screening{Verdict: pkgcompliance.VerdictReview, RiskScore: f64(42.5), Raw: json.RawMessage(`{}`)}}
		svc := NewService(Deps{Repo: repo, Screener: screener})

		require.NoError(t, svc.ScreenAndRecord(context.Background(), addr.ID))
		require.NotNil(t, repo.screenings[0].RiskScore)
		assert.Equal(t, 42.5, *repo.screenings[0].RiskScore)
	})
}

func TestService_ApproveKYB(t *testing.T) {
	repo := newFakeRepo()
	newApprovedCounterparty(repo, "cp-1", models.CounterpartyKYBPending)
	a1 := &models.CounterpartyAddress{ID: "addr-1", CounterpartyID: "cp-1", Address: testAddress()}
	a2 := &models.CounterpartyAddress{ID: "addr-2", CounterpartyID: "cp-1", Address: testAddress()}
	repo.addresses[a1.ID] = a1
	repo.addresses[a2.ID] = a2

	screener := &fakeScreener{result: &pkgcompliance.Screening{Verdict: pkgcompliance.VerdictApproved, Raw: json.RawMessage(`{}`)}}
	svc := NewService(Deps{Repo: repo, Screener: screener})

	require.NoError(t, svc.ApproveKYB(context.Background(), "cp-1", "GADMIN..."))

	assert.Equal(t, models.CounterpartyKYBApproved, repo.counterparties["cp-1"].KYBStatus)
	assert.Len(t, screener.calls, 2, "every existing address on the counterparty is screened")
	assert.Equal(t, models.AddressStatusApproved, a1.Status)
	assert.Equal(t, models.AddressStatusApproved, a2.Status)
}
