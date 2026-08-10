package models

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

func mustDate(t *testing.T, s string) Date {
	t.Helper()
	tm, err := time.Parse("2006-01-02", s)
	if err != nil {
		t.Fatal(err)
	}
	return Date{Time: tm}
}

func TestDate_MarshalJSON(t *testing.T) {
	if b, _ := (Date{}).MarshalJSON(); string(b) != "null" {
		t.Errorf("zero Date MarshalJSON = %s, want null", b)
	}
	b, err := mustDate(t, "2026-07-25").MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != `"2026-07-25"` {
		t.Errorf("MarshalJSON = %s", b)
	}
}

func TestDate_Value(t *testing.T) {
	v, _ := (Date{}).Value()
	if v != nil {
		t.Errorf("zero Date Value = %v, want nil", v)
	}
	v, err := mustDate(t, "2026-07-25").Value()
	if err != nil {
		t.Fatal(err)
	}
	if v != "2026-07-25" {
		t.Errorf("Value = %v, want 2026-07-25", v)
	}
}

func TestDate_Scan(t *testing.T) {
	// nil clears to zero.
	var d Date
	if err := d.Scan(nil); err != nil || !d.IsZero() {
		t.Errorf("Scan(nil): err=%v zero=%v", err, d.IsZero())
	}
	// time.Time, []byte and string all land on the same date.
	for name, in := range map[string]any{
		"time.Time": time.Date(2026, 7, 25, 0, 0, 0, 0, time.UTC),
		"bytes":     []byte("2026-07-25"),
		"string":    "2026-07-25",
		"timestamp": "2026-07-25T13:00:00Z", // longer strings are truncated to the date
	} {
		var got Date
		if err := got.Scan(in); err != nil {
			t.Errorf("Scan(%s) error: %v", name, err)
			continue
		}
		if got.Format("2006-01-02") != "2026-07-25" {
			t.Errorf("Scan(%s) = %s", name, got.Format("2006-01-02"))
		}
	}
	// Unsupported type and bad format error.
	if err := (&Date{}).Scan(42); err == nil {
		t.Error("Scan(int) should error")
	}
	if err := (&Date{}).Scan("not-a-date"); err == nil {
		t.Error("Scan(bad string) should error")
	}
}

func TestBeforeCreate_GeneratesUUID(t *testing.T) {
	// Each hook stamps a fresh UUID; tx is unused so nil is fine.
	u := &User{}
	if err := u.BeforeCreate(nil); err != nil || u.ID == "" {
		t.Fatalf("User.BeforeCreate: err=%v id=%q", err, u.ID)
	}
	if _, err := uuid.Parse(u.ID); err != nil {
		t.Errorf("User.ID is not a valid UUID: %q", u.ID)
	}

	a := &Account{}
	if err := a.BeforeCreate(nil); err != nil || a.ID == "" {
		t.Fatalf("Account.BeforeCreate: err=%v id=%q", err, a.ID)
	}

	tx := &Transaction{}
	if err := tx.BeforeCreate(nil); err != nil || tx.ID == "" {
		t.Fatalf("Transaction.BeforeCreate: err=%v id=%q", err, tx.ID)
	}

	sq := &SecurityQuestion{}
	if err := sq.BeforeCreate(nil); err != nil || sq.ID == "" {
		t.Fatalf("SecurityQuestion.BeforeCreate: err=%v id=%q", err, sq.ID)
	}
}
