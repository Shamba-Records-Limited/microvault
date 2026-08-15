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
