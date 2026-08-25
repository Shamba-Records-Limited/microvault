package adapters

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"

	"github.com/samber/oops"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tyler-smith/go-bip32"

	"github.com/Shamba-Records-Limited/microvault/pkg/account"
	"github.com/Shamba-Records-Limited/microvault/pkg/models"
	"github.com/Shamba-Records-Limited/microvault/pkg/stellar"
)

// derivationTestSeed is arbitrary entropy, not a Stellar secret. deriveChildKeypair
// only reads walletConfig.MasterKey, so the adapter can be built directly and the
// derivation pinned without a key-shaped constant in the repo.
const derivationTestSeed = "microvault-derivation-test-seed"

func newDerivationTestAdapter(t *testing.T) *UserServiceAdapter {
	t.Helper()
	master, err := bip32.NewMasterKey([]byte(derivationTestSeed))
	require.NoError(t, err)
	return &UserServiceAdapter{
		walletConfig: WalletConfig{MasterKey: master},
		logger:       slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
}

// Child keypairs were derived at m/44'/148'/(account_index + 4)' between 051d0b3
// and migration 000009, so accounts.account_index did not identify the key it
// stored. These golden addresses pin the derivation to the stored index exactly.
// A failure here means an offset was reintroduced — which silently yields a
// valid keypair for the wrong account, so it must never regress quietly.
func TestDeriveChildKeypair_UsesStoredIndexVerbatim(t *testing.T) {
	a := newDerivationTestAdapter(t)

	golden := map[int]string{
		1: "GAMZZBSGUJML7J7TP3N6S7SRPECW4TIQLIMVVGOJK3VY6TXN4RQRERER",
		5: "GBYK3N42J6ZJZ7GJWB6B3BRMVBFRD7OCBXWTDY6AVE2XIPJERE665Q4H",
	}

	for index, want := range golden {
		kp, err := a.deriveChildKeypair(index)
		require.NoError(t, err)
		assert.Equal(t, want, kp.Address(), "derivation changed for index %d", index)
	}
}

// The historical bug in one assertion: index n must not derive what n+4 derives.
func TestDeriveChildKeypair_NoHiddenOffset(t *testing.T) {
	a := newDerivationTestAdapter(t)

	first, err := a.deriveChildKeypair(1)
	require.NoError(t, err)
	offset, err := a.deriveChildKeypair(1 + 4)
	require.NoError(t, err)

	assert.NotEqual(t, offset.Address(), first.Address(),
		"index 1 derived what index 5 should — the +4 offset is back")
}

// fakeAccountService embeds the interface so only what these tests touch needs
// implementing; anything else panics loudly rather than returning a zero value.
type fakeAccountService struct {
	account.Service
	acct        *account.AccountResponse
	lookupErr   error
	chainWrites []string
	updateErr   error
}

func (f *fakeAccountService) GetByPublicKey(_ context.Context, _ string) (*account.AccountResponse, error) {
	if f.lookupErr != nil {
		return nil, f.lookupErr
	}
	return f.acct, nil
}

func (f *fakeAccountService) UpdateChainStatus(_ context.Context, _ string, chainStatus string) error {
	if f.updateErr != nil {
		return f.updateErr
	}
	f.chainWrites = append(f.chainWrites, chainStatus)
	return nil
}

// fakeStellarService embeds the interface so only the two methods
// EnsureOnChainAccount touches need implementing; anything else panics loudly
// rather than silently returning a zero value.
type fakeStellarService struct {
	stellar.Service
	exists  bool
	created []stellar.CreateAccountRequest
}

func (f *fakeStellarService) AccountExists(_ context.Context, _ string) (bool, error) {
	return f.exists, nil
}

func (f *fakeStellarService) CreateSponsoredAccount(_ context.Context, req stellar.CreateAccountRequest) error {
	f.created = append(f.created, req)
	return nil
}

// The offset lived at the call sites, not inside deriveChildKeypair, so the
// golden vectors above would not have caught it. This covers the self-heal path
// that re-creates a child account after registration failed to land it on-chain:
// it must re-derive from the stored index alone.
func TestEnsureOnChainAccount_DerivesFromStoredIndexWithoutOffset(t *testing.T) {
	a := newDerivationTestAdapter(t)
	fake := &fakeStellarService{exists: false}
	a.stellarService = fake
	a.accountService = &fakeAccountService{
		acct: &account.AccountResponse{ID: "acct-1", ChainStatus: models.ChainStatusPending},
	}

	const storedIndex = 3
	kp, err := a.deriveChildKeypair(storedIndex)
	require.NoError(t, err)

	require.NoError(t, a.EnsureOnChainAccount(context.Background(), storedIndex, kp.Address()))

	require.Len(t, fake.created, 1, "missing account should have been created")
	assert.Equal(t, kp.Address(), fake.created[0].ChildKeypair.Address())
}

// The address check is what makes a wrong index loud instead of silent.
func TestEnsureOnChainAccount_RejectsIndexAddressMismatch(t *testing.T) {
	a := newDerivationTestAdapter(t)
	fake := &fakeStellarService{exists: false}
	a.stellarService = fake

	kp, err := a.deriveChildKeypair(3)
	require.NoError(t, err)

	err = a.EnsureOnChainAccount(context.Background(), 3+4, kp.Address())
	require.Error(t, err)
	// Both addresses are attributes now: which pair disagreed is the fact
	// worth pinning, and it is what an operator needs to diagnose a bad seed.
	var oopsErr oops.OopsError
	require.ErrorAs(t, err, &oopsErr)
	assert.NotEmpty(t, oopsErr.Context()["derived_address"])
	assert.NotEmpty(t, oopsErr.Context()["stored_address"])
	assert.Empty(t, fake.created, "must not create an account for a mismatched index")
}

// Registration commits the account row before the chain create is attempted, so
// chain_status is the only durable marker that a heal is still owed. Ensuring an
// account must settle it.
func TestEnsureOnChainAccount_ConfirmsChainStatusAfterCreating(t *testing.T) {
	a := newDerivationTestAdapter(t)
	a.stellarService = &fakeStellarService{exists: false}
	accts := &fakeAccountService{
		acct: &account.AccountResponse{ID: "acct-1", ChainStatus: models.ChainStatusFailed},
	}
	a.accountService = accts

	kp, err := a.deriveChildKeypair(3)
	require.NoError(t, err)
	require.NoError(t, a.EnsureOnChainAccount(context.Background(), 3, kp.Address()))

	assert.Equal(t, []string{models.ChainStatusConfirmed}, accts.chainWrites)
}

// The common heal: a goroutine dropped by a restart left the row pending even
// though the account did land on-chain.
func TestEnsureOnChainAccount_ConfirmsPendingRowThatIsAlreadyOnChain(t *testing.T) {
	a := newDerivationTestAdapter(t)
	stell := &fakeStellarService{exists: true}
	a.stellarService = stell
	accts := &fakeAccountService{
		acct: &account.AccountResponse{ID: "acct-1", ChainStatus: models.ChainStatusPending},
	}
	a.accountService = accts

	require.NoError(t, a.EnsureOnChainAccount(context.Background(), 3, "GABC"))

	assert.Equal(t, []string{models.ChainStatusConfirmed}, accts.chainWrites)
	assert.Empty(t, stell.created, "account already exists; nothing to create")
}

func TestEnsureOnChainAccount_AlreadyConfirmedSkipsRedundantWrite(t *testing.T) {
	a := newDerivationTestAdapter(t)
	a.stellarService = &fakeStellarService{exists: true}
	accts := &fakeAccountService{
		acct: &account.AccountResponse{ID: "acct-1", ChainStatus: models.ChainStatusConfirmed},
	}
	a.accountService = accts

	require.NoError(t, a.EnsureOnChainAccount(context.Background(), 3, "GABC"))

	assert.Empty(t, accts.chainWrites, "confirmed rows must not be rewritten every disbursement")
}

// Bookkeeping is best effort — the account genuinely exists at that point, so a
// failed status write must not fail the caller and block lending.
func TestEnsureOnChainAccount_BookkeepingFailureDoesNotFailCaller(t *testing.T) {
	a := newDerivationTestAdapter(t)
	a.stellarService = &fakeStellarService{exists: true}
	a.accountService = &fakeAccountService{lookupErr: errors.New("db down")}

	assert.NoError(t, a.EnsureOnChainAccount(context.Background(), 3, "GABC"))
}

func TestDeriveChildKeypair_IsDeterministic(t *testing.T) {
	a := newDerivationTestAdapter(t)

	first, err := a.deriveChildKeypair(7)
	require.NoError(t, err)
	second, err := newDerivationTestAdapter(t).deriveChildKeypair(7)
	require.NoError(t, err)

	assert.Equal(t, first.Address(), second.Address(),
		"same seed and index must reproduce the same account across processes")
}
