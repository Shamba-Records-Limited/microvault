//go:build integration

package pilot

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"

	"gorm.io/gorm"

	"github.com/Shamba-Records-Limited/microvault/pkg/config"
	"github.com/Shamba-Records-Limited/microvault/platform/database"
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
	os.Exit(m.Run())
}

func newStore(t *testing.T) *Store {
	t.Helper()
	testDB.Exec("TRUNCATE pilot_users")
	testDB.Exec("DELETE FROM users WHERE mobile_number LIKE '2547990000%'")
	s, err := NewStore(testDB)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func addUser(t *testing.T, mobile, nationalID string) string {
	t.Helper()
	var id string
	err := testDB.Raw(`INSERT INTO users (id, mobile_number, national_id, full_name) VALUES (gen_random_uuid(), ?, ?, 'Test') RETURNING id`,
		mobile, nationalID).Scan(&id).Error
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func TestStore_GateLookups(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()

	if _, err := s.Add(ctx, User{PhoneNumber: "0799000011", NationalID: "1234 5678", FullName: "Jane Doe"}); err != nil {
		t.Fatal(err)
	}

	if ok, err := s.PhoneActive(ctx, "254799000011"); err != nil || !ok {
		t.Errorf("PhoneActive = %v, %v", ok, err)
	}
	if ok, _ := s.Matches(ctx, "254799000011", "12345678"); !ok {
		t.Error("Matches should accept the listed ID")
	}
	if ok, _ := s.Matches(ctx, "254799000011", "87654321"); ok {
		t.Error("Matches should reject another ID")
	}

	userID := addUser(t, "254799000011", "12345678")
	if ok, _ := s.UserActive(ctx, userID); !ok {
		t.Error("UserActive should accept a listed user")
	}

	other := addUser(t, "254799000012", "11112222")
	unlisted, err := s.Unlisted(ctx)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, u := range unlisted {
		if u.ID == userID {
			t.Error("a listed user must not be reported unlisted")
		}
		found = found || u.ID == other
	}
	if !found {
		t.Error("an unlisted user must be reported")
	}
}

func TestStore_RevokeCoversEveryRowForThePerson(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()

	for _, p := range []string{"254799000021", "254799000022"} {
		if _, err := s.Add(ctx, User{PhoneNumber: p, NationalID: "55556666", FullName: "Jane Doe"}); err != nil {
			t.Fatal(err)
		}
	}
	userID := addUser(t, "254799000022", "55556666")

	revoked, err := s.Revoke(ctx, Selector{Phone: "254799000021"}, "test")
	if err != nil || len(revoked) != 2 {
		t.Fatalf("Revoke = %d rows, %v", len(revoked), err)
	}
	if ok, _ := s.UserActive(ctx, userID); ok {
		t.Error("revoking the old SIM must lock the person out on the new one too")
	}

	restored, err := s.Restore(ctx, Selector{NationalID: "55556666"})
	if err != nil || len(restored) != 2 {
		t.Fatalf("Restore = %d rows, %v", len(restored), err)
	}
	if ok, _ := s.UserActive(ctx, userID); !ok {
		t.Error("restore should reinstate access")
	}

	if _, err := s.Revoke(ctx, Selector{Phone: "254799000099"}, "test"); !errors.Is(err, ErrNotFound) {
		t.Errorf("revoking an unknown phone = %v, want ErrNotFound", err)
	}
}

func TestStore_Invites(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()

	for _, p := range []string{"254799000031", "254799000032", "254799000033"} {
		if _, err := s.Add(ctx, User{PhoneNumber: p, NationalID: p, FullName: "Jane Doe"}); err != nil {
			t.Fatal(err)
		}
	}
	addUser(t, "254799000032", "254799000032")
	if _, err := s.Revoke(ctx, Selector{Phone: "254799000033"}, "test"); err != nil {
		t.Fatal(err)
	}

	pending, err := s.PendingInvites(ctx)
	if err != nil || len(pending) != 1 || pending[0].PhoneNumber != "254799000031" {
		t.Fatalf("PendingInvites = %+v, %v", pending, err)
	}

	if err := s.RecordInvite(ctx, "254799000031", errors.New("gateway down")); err != nil {
		t.Fatal(err)
	}
	u, _ := s.Get(ctx, "254799000031")
	if u.InvitedAt != nil || u.InviteCount != 1 || u.LastInviteError == nil {
		t.Errorf("after a failed send: %+v", u)
	}
	if pending, _ := s.PendingInvites(ctx); len(pending) != 1 {
		t.Error("a failed send stays pending")
	}

	if err := s.RecordInvite(ctx, "254799000031", nil); err != nil {
		t.Fatal(err)
	}
	u, _ = s.Get(ctx, "254799000031")
	if u.InvitedAt == nil || u.InviteCount != 2 || u.LastInviteError != nil {
		t.Errorf("after a successful send: %+v", u)
	}
	if pending, _ := s.PendingInvites(ctx); len(pending) != 0 {
		t.Error("an invited row is no longer pending")
	}
}
