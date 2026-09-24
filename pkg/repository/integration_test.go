//go:build integration

// Package repository integration tests run against a real Postgres. They are
// gated behind the `integration` build tag so the default `go test ./...` /
// `make test` stays hermetic. Run them with a migrated test database via
// `make test-integration` (see docker-compose.test.yml), or locally against any
// Postgres by exporting DB_HOST/DB_PORT/DB_USER/DB_PASSWORD/DB_NAME.
package repository

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"

	"github.com/Shamba-Records-Limited/microvault/pkg/config"
	"github.com/Shamba-Records-Limited/microvault/pkg/models"
	"github.com/Shamba-Records-Limited/microvault/platform/database"
	"gorm.io/gorm"
)

var testDB *gorm.DB

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func TestMain(m *testing.M) {
	cfg := &config.PostgresConfig{
		Host:     env("DB_HOST", "localhost"),
		Port:     env("DB_PORT", "5435"),
		User:     env("DB_USER", "microvault_test"),
		Password: env("DB_PASSWORD", "microvault_test"),
		DBName:   env("DB_NAME", "microvault_test"),
		SSLMode:  env("DB_SSL_MODE", "disable"),
		TimeZone: "UTC",
	}

	// Migrate; on any failure, skip the suite rather than fail (no DB available).
	if err := database.RunMigrations(cfg); err != nil {
		fmt.Println("integration: skipping — migrations failed:", err)
		os.Exit(0)
	}
	db, err := database.GetConnection("test", cfg)
	if err != nil {
		fmt.Println("integration: skipping — connect failed:", err)
		os.Exit(0)
	}
	testDB = db

	// Start each run from a clean slate.
	testDB.Exec("TRUNCATE users, accounts, transactions RESTART IDENTITY CASCADE")

	os.Exit(m.Run())
}

func newUser(t *testing.T, mobile string) *models.User {
	t.Helper()
	repo, err := NewUserRepository(testDB)
	if err != nil {
		t.Fatal(err)
	}
	u := &models.User{MobileNumber: mobile, CountryCode: "KE", KYCStatus: "pending", Status: "active", PreferredLanguage: "en"}
	if err := repo.Create(context.Background(), u); err != nil {
		t.Fatalf("create user: %v", err)
	}
	return u
}

func TestUserRepository_CRUD(t *testing.T) {
	repo, err := NewUserRepository(testDB)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	u := newUser(t, "254711000001")
	if u.ID == "" {
		t.Fatal("BeforeCreate did not set an ID")
	}

	got, err := repo.GetByID(ctx, u.ID)
	if err != nil || got.MobileNumber != "254711000001" {
		t.Errorf("GetByID = %+v (err %v)", got, err)
	}

	byMobile, err := repo.GetByMobileNumber(ctx, "254711000001")
	if err != nil || byMobile.ID != u.ID {
		t.Errorf("GetByMobileNumber = %+v (err %v)", byMobile, err)
	}

	if _, err := repo.GetByID(ctx, "00000000-0000-0000-0000-000000000000"); !errors.Is(err, ErrUserNotFound) {
		t.Errorf("GetByID(missing) err = %v, want ErrUserNotFound", err)
	}
}

func TestAccountRepository_CRUD(t *testing.T) {
	u := newUser(t, "254711000002")
	repo, err := NewAccountRepository(testDB)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	pk := "G" + "BBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBB"
	acc := &models.Account{UserID: u.ID, PublicKey: pk, AccountIndex: 0, Status: "active"}
	if err := repo.Create(ctx, acc); err != nil {
		t.Fatalf("create account: %v", err)
	}

	byKey, err := repo.GetByPublicKey(ctx, pk)
	if err != nil || byKey.ID != acc.ID {
		t.Errorf("GetByPublicKey = %+v (err %v)", byKey, err)
	}

	// account_index_seq is a global monotonic sequence: consecutive calls hand
	// out strictly increasing, never-reused indices (see migration 000009).
	i1, err := repo.GetNextAccountIndex(ctx, u.ID)
	if err != nil {
		t.Fatalf("GetNextAccountIndex: %v", err)
	}
	i2, _ := repo.GetNextAccountIndex(ctx, u.ID)
	if i2 <= i1 {
		t.Errorf("GetNextAccountIndex not monotonic: %d then %d", i1, i2)
	}
}

func TestTransactionRepository_CRUD(t *testing.T) {
	u := newUser(t, "254711000003")
	repo, err := NewTransactionRepository(testDB)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	hash := "deadbeefhash01"
	ext := "ext-req-1"
	tx := &models.Transaction{
		UserID: &u.ID, TxType: "off_ramp", Amount: 1000, Asset: "USDC",
		Status: models.TxStatusPending, StellarTxHash: &hash, ExternalID: &ext,
	}
	if err := repo.Create(ctx, tx); err != nil {
		t.Fatalf("create tx: %v", err)
	}

	byHash, err := repo.GetByStellarHash(ctx, hash)
	if err != nil || byHash.ID != tx.ID {
		t.Errorf("GetByStellarHash = %+v (err %v)", byHash, err)
	}

	list, err := repo.ListByExternalID(ctx, ext)
	if err != nil || len(list) != 1 {
		t.Errorf("ListByExternalID = %v (err %v)", list, err)
	}

	// UpdateFields persists a partial change and is read back.
	if err := repo.UpdateFields(ctx, tx.ID, map[string]any{"status": models.TxStatusSubmitted}); err != nil {
		t.Fatalf("UpdateFields: %v", err)
	}
	reloaded, _ := repo.GetByID(ctx, tx.ID)
	if reloaded.Status != models.TxStatusSubmitted {
		t.Errorf("status after update = %q, want submitted", reloaded.Status)
	}

	submitted, err := repo.GetByStatus(ctx, models.TxStatusSubmitted, 10, 0)
	if err != nil || len(submitted) != 1 {
		t.Errorf("GetByStatus = %v (err %v)", submitted, err)
	}
}

// The staging incident that motivated EnsureAccountIndexIntegrity: the
// sequence was rewound beneath indices the table had already issued, so the
// next registration re-derived a keypair whose Stellar account existed.
func TestAccountRepository_IndexIntegrityRearmsRewoundSequence(t *testing.T) {
	u := newUser(t, "254711000009")
	repo, err := NewAccountRepository(testDB)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	const spent = 5000
	acc := &models.Account{
		UserID: u.ID, PublicKey: "GREWOUND" + u.ID, AccountIndex: spent, Status: "active",
	}
	if err := repo.Create(ctx, acc); err != nil {
		t.Fatalf("create account: %v", err)
	}

	// Rewind the sequence the way a restored dump would.
	if err := testDB.WithContext(ctx).
		Exec("SELECT setval('account_index_seq', 1, true)").Error; err != nil {
		t.Fatalf("rewind sequence: %v", err)
	}

	next, err := repo.EnsureAccountIndexIntegrity(ctx, 0)
	if err != nil {
		t.Fatalf("EnsureAccountIndexIntegrity: %v", err)
	}
	if next <= spent {
		t.Fatalf("next allocation = %d, want > %d (the spent index)", next, spent)
	}

	issued, err := repo.GetNextAccountIndex(ctx, u.ID)
	if err != nil {
		t.Fatalf("GetNextAccountIndex: %v", err)
	}
	if issued <= spent {
		t.Errorf("re-issued a spent index: got %d, high-water mark is %d", issued, spent)
	}
}

// A soft-deleted account still owns its on-chain Stellar account, so its index
// must stay spent. Deleting the user was how staging masked the collision.
func TestAccountRepository_IndexIntegrityCountsSoftDeleted(t *testing.T) {
	u := newUser(t, "254711000010")
	repo, err := NewAccountRepository(testDB)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	const spent = 6000
	acc := &models.Account{
		UserID: u.ID, PublicKey: "GDELETED" + u.ID, AccountIndex: spent, Status: "active",
	}
	if err := repo.Create(ctx, acc); err != nil {
		t.Fatalf("create account: %v", err)
	}
	if err := repo.Delete(ctx, acc.ID); err != nil {
		t.Fatalf("delete account: %v", err)
	}

	high, err := repo.MaxAccountIndex(ctx)
	if err != nil {
		t.Fatalf("MaxAccountIndex: %v", err)
	}
	if high < spent {
		t.Fatalf("MaxAccountIndex = %d, want >= %d — a soft-deleted index was forgotten", high, spent)
	}
}

// The operator-supplied base covers what the rows cannot: a database rebuilt
// from scratch while the on-chain accounts persisted.
func TestAccountRepository_IndexIntegrityHonoursBase(t *testing.T) {
	repo, err := NewAccountRepository(testDB)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	const base = 90000
	next, err := repo.EnsureAccountIndexIntegrity(ctx, base)
	if err != nil {
		t.Fatalf("EnsureAccountIndexIntegrity: %v", err)
	}
	if next < base {
		t.Errorf("next allocation = %d, want >= base %d", next, base)
	}

	// Re-arming must never rewind a sequence that is already ahead.
	again, err := repo.EnsureAccountIndexIntegrity(ctx, 1)
	if err != nil {
		t.Fatalf("EnsureAccountIndexIntegrity(1): %v", err)
	}
	if again < base {
		t.Errorf("sequence rewound to %d, below the base %d it had reached", again, base)
	}
}
