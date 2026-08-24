package auth

import (
	"errors"
	"testing"
	"time"

	"github.com/stellar/go-stellar-sdk/keypair"
	"github.com/stellar/go-stellar-sdk/txnbuild"

	"github.com/Shamba-Records-Limited/microvault/pkg/config"
)

const testPassphrase = "Test SDF Network ; September 2015"

// memStore is an in-memory ChallengeStore.
type memStore struct {
	m map[string]*Challenge
}

func newMemStore() *memStore { return &memStore{m: map[string]*Challenge{}} }

func (s *memStore) Store(id string, c *Challenge) error { s.m[id] = c; return nil }
func (s *memStore) Get(id string) (*Challenge, error) {
	c, ok := s.m[id]
	if !ok {
		return nil, errors.New("not found")
	}
	return c, nil
}
func (s *memStore) Delete(id string) error { delete(s.m, id); return nil }

func newChallengeSvc(t *testing.T, store ChallengeStore) (ChallengeService, *keypair.Full) {
	t.Helper()
	admin, err := keypair.Random()
	if err != nil {
		t.Fatal(err)
	}
	server, err := keypair.Random()
	if err != nil {
		t.Fatal(err)
	}
	svc, err := NewChallengeService(
		&config.AuthConfig{ChallengeExpiration: 5 * time.Minute},
		&config.StellarConfig{
			AdminPublicKey:    admin.Address(),
			AdminSecretKey:    admin.Seed(),
			ServerSecretKey:   server.Seed(),
			NetworkPassphrase: testPassphrase,
		},
		store,
	)
	if err != nil {
		t.Fatalf("NewChallengeService: %v", err)
	}
	return svc, admin
}

// signChallenge countersigns a challenge the way a wallet would.
func signChallenge(t *testing.T, txB64 string, kp *keypair.Full) string {
	t.Helper()
	gtx, err := txnbuild.TransactionFromXDR(txB64)
	if err != nil {
		t.Fatal(err)
	}
	tx, ok := gtx.Transaction()
	if !ok {
		t.Fatal("not a simple transaction")
	}
	tx, err = tx.Sign(testPassphrase, kp)
	if err != nil {
		t.Fatal(err)
	}
	out, err := tx.Base64()
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestNewChallengeService_InvalidSecret(t *testing.T) {
	_, err := NewChallengeService(
		&config.AuthConfig{},
		&config.StellarConfig{ServerSecretKey: "not-a-secret"},
		newMemStore(),
	)
	if err == nil {
		t.Error("expected error for invalid server secret")
	}
}

func TestNewChallengeService_RejectsSharedKey(t *testing.T) {
	kp, _ := keypair.Random()
	_, err := NewChallengeService(
		&config.AuthConfig{},
		&config.StellarConfig{
			AdminPublicKey:    kp.Address(),
			ServerSecretKey:   kp.Seed(),
			NetworkPassphrase: testPassphrase,
		},
		newMemStore(),
	)
	if err == nil {
		t.Error("expected error when the signing key is the admin key")
	}
}

func TestGenerateChallenge(t *testing.T) {
	store := newMemStore()
	svc, _ := newChallengeSvc(t, store)

	ch, err := svc.GenerateChallenge()
	if err != nil {
		t.Fatalf("GenerateChallenge: %v", err)
	}
	if ch.ID == "" || ch.Transaction == "" {
		t.Error("challenge missing ID or transaction")
	}
	if store.m[ch.ID] == nil {
		t.Error("challenge not persisted to store")
	}
	// IDs are unique per call.
	ch2, _ := svc.GenerateChallenge()
	if ch.ID == ch2.ID {
		t.Error("challenge IDs should be unique")
	}
}

func TestVerify_HappyPath(t *testing.T) {
	store := newMemStore()
	svc, admin := newChallengeSvc(t, store)

	ch, _ := svc.GenerateChallenge()
	if err := svc.VerifySignedChallenge(ch.ID, signChallenge(t, ch.Transaction, admin)); err != nil {
		t.Fatalf("VerifySignedChallenge: %v", err)
	}
	// Single-use: the challenge is deleted after a successful verify.
	if store.m[ch.ID] != nil {
		t.Error("challenge should be deleted after verification (replay guard)")
	}
}

func TestVerify_RejectsUnsignedEcho(t *testing.T) {
	svc, _ := newChallengeSvc(t, newMemStore())

	ch, _ := svc.GenerateChallenge()
	if err := svc.VerifySignedChallenge(ch.ID, ch.Transaction); !errors.Is(err, ErrInvalidSignature) {
		t.Errorf("err = %v, want ErrInvalidSignature", err)
	}
}

func TestVerify_RejectsForeignSigner(t *testing.T) {
	svc, _ := newChallengeSvc(t, newMemStore())
	attacker, _ := keypair.Random()

	ch, _ := svc.GenerateChallenge()
	if err := svc.VerifySignedChallenge(ch.ID, signChallenge(t, ch.Transaction, attacker)); !errors.Is(err, ErrInvalidSignature) {
		t.Errorf("err = %v, want ErrInvalidSignature", err)
	}
}

func TestVerify_NotFound(t *testing.T) {
	svc, _ := newChallengeSvc(t, newMemStore())
	if err := svc.VerifySignedChallenge("missing", "irrelevant"); !errors.Is(err, ErrChallengeNotFound) {
		t.Errorf("err = %v, want ErrChallengeNotFound", err)
	}
}

func TestVerify_Expired(t *testing.T) {
	store := newMemStore()
	svc, _ := newChallengeSvc(t, store)
	store.m["exp"] = &Challenge{ID: "exp", Transaction: "x", ExpiresAt: time.Now().Add(-time.Minute)}
	if err := svc.VerifySignedChallenge("exp", "x"); !errors.Is(err, ErrChallengeExpired) {
		t.Errorf("err = %v, want ErrChallengeExpired", err)
	}
}

func TestVerify_InvalidTransaction(t *testing.T) {
	svc, _ := newChallengeSvc(t, newMemStore())
	ch, _ := svc.GenerateChallenge()
	if err := svc.VerifySignedChallenge(ch.ID, "not-valid-xdr"); !errors.Is(err, ErrInvalidTransaction) {
		t.Errorf("err = %v, want ErrInvalidTransaction", err)
	}
}

func TestVerify_TransactionMismatch(t *testing.T) {
	svc, _ := newChallengeSvc(t, newMemStore())
	ch1, _ := svc.GenerateChallenge()
	ch2, _ := svc.GenerateChallenge()
	// A valid tx from a different challenge has a different hash.
	if err := svc.VerifySignedChallenge(ch1.ID, ch2.Transaction); !errors.Is(err, ErrTransactionMismatch) {
		t.Errorf("err = %v, want ErrTransactionMismatch", err)
	}
}
