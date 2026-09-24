package airtel

import (
	"context"
	"testing"
	"time"

	"github.com/Shamba-Records-Limited/microvault/pkg/payment/airtel/airtelstub"
)

// Checking before prompting turns a downstream failure into an answer given
// before the borrower was told to expect a prompt.
func TestUserEnquiry_PreflightStates(t *testing.T) {
	cases := map[string]struct {
		msisdn         string
		wantTransact   bool
		wantLookupFail bool
	}{
		"healthy": {msisdn: "0733123456", wantTransact: true},
		"barred":  {msisdn: "0733000001"},
		"no pin":  {msisdn: "0733000002"},
		"unknown": {msisdn: "0733000003", wantLookupFail: true},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			client, _ := newStubbed(t)

			resp, err := client.UserEnquiry(context.Background(), tc.msisdn)
			if tc.wantLookupFail {
				if err == nil {
					t.Fatal("expected an unknown subscriber to fail the lookup")
				}
				return
			}
			if err != nil {
				t.Fatalf("UserEnquiry: %v", err)
			}
			if resp.CanTransact() != tc.wantTransact {
				t.Fatalf("CanTransact() = %v, want %v (barred %v, pin set %v)",
					resp.CanTransact(), tc.wantTransact, resp.Data.IsBarred, resp.Data.IsPINSet)
			}
		})
	}
}

func TestParseFormattedAmount(t *testing.T) {
	cases := map[string]struct {
		in      string
		want    int64
		wantErr bool
	}{
		"grouped":      {in: "37,600.00", want: 3_760_000},
		"plain":        {in: "500.00", want: 50_000},
		"no decimals":  {in: "500", want: 50_000},
		"cents":        {in: "1,234.56", want: 123_456},
		"spaced":       {in: " 1,000.50 ", want: 100_050},
		"zero":         {in: "0.00", want: 0},
		"empty":        {in: "", wantErr: true},
		"not a number": {in: "lots", wantErr: true},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got, err := ParseFormattedAmount(tc.in)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("ParseFormattedAmount(%q) = %d, want an error", tc.in, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseFormattedAmount(%q): %v", tc.in, err)
			}
			if got != tc.want {
				t.Fatalf("ParseFormattedAmount(%q) = %d, want %d", tc.in, got, tc.want)
			}
		})
	}
}

// A balance that cannot be parsed must not read as zero: a float alert that
// pages on a shortfall that does not exist is worse than no alert.
func TestBalanceEnquiry_FormattedString(t *testing.T) {
	client, stub := newStubbed(t)
	stub.SetBalanceMinor(3_760_000)

	resp, err := client.BalanceEnquiry(context.Background())
	if err != nil {
		t.Fatalf("BalanceEnquiry: %v", err)
	}
	if resp.Data.Balance != "37,600.00" {
		t.Fatalf("balance = %q, want the grouped rendering", resp.Data.Balance)
	}

	minor, err := resp.Minor()
	if err != nil {
		t.Fatalf("Minor: %v", err)
	}
	if minor != 3_760_000 {
		t.Fatalf("Minor() = %d, want 3760000", minor)
	}

	var broken BalanceResponse
	broken.Data.Balance = "not-a-balance"
	if _, err := broken.Minor(); err == nil {
		t.Fatal("an unparseable balance must fail rather than read as zero")
	}
}

func TestTransactionsSummary(t *testing.T) {
	client, stub := newStubbed(t)

	for _, id := range []string{"mv-1", "mv-2"} {
		req := validPayment()
		req.TransactionID = id
		if _, err := client.Payment(context.Background(), req); err != nil {
			t.Fatalf("Payment %s: %v", id, err)
		}
	}

	// One that never settles must not appear in a settlement sweep.
	stub.NextOutcome(airtelstub.StatusInProgress, "DP00800001006")
	pending := validPayment()
	pending.TransactionID = "mv-3"
	if _, err := client.Payment(context.Background(), pending); err != nil {
		t.Fatalf("Payment mv-3: %v", err)
	}

	resp, err := client.TransactionsSummary(context.Background(), SummaryRequest{
		From: time.Now().Add(-time.Hour),
		To:   time.Now().Add(time.Hour),
	})
	if err != nil {
		t.Fatalf("TransactionsSummary: %v", err)
	}

	settled := resp.Settled()
	if len(settled) != 2 {
		t.Fatalf("settled %d transactions, want 2", len(settled))
	}
	for _, entry := range settled {
		if entry.Transaction.AirtelMoneyID == "" {
			t.Fatalf("a settled entry carries no receipt: %+v", entry.Transaction)
		}
		minor, err := entry.AmountMinor()
		if err != nil {
			t.Fatalf("AmountMinor: %v", err)
		}
		if minor != 50_000 {
			t.Fatalf("amount = %d minor, want 50000", minor)
		}
	}
}

func TestTransactionsSummary_WindowValidation(t *testing.T) {
	client, _ := newStubbed(t)
	now := time.Now()

	cases := map[string]SummaryRequest{
		"no bounds":     {},
		"no start":      {To: now},
		"no end":        {From: now},
		"inverted":      {From: now, To: now.Add(-time.Hour)},
		"zero duration": {From: now, To: now},
	}

	for name, req := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := client.TransactionsSummary(context.Background(), req); err == nil {
				t.Fatal("expected the window to be refused before the wire")
			}
		})
	}
}

func TestEncryptionKeys_CachedUntilExpiry(t *testing.T) {
	client, _ := newStubbed(t)

	first, err := client.EncryptionKeys(context.Background())
	if err != nil {
		t.Fatalf("EncryptionKeys: %v", err)
	}
	if first.PublicKey == "" {
		t.Fatal("no key material was returned")
	}
	if first.ValidUpto.IsZero() {
		t.Fatal("no expiry was parsed")
	}
	if _, err := ParseRSAPublicKey(first.PublicKey); err != nil {
		t.Fatalf("the returned key does not parse: %v", err)
	}

	second, err := client.EncryptionKeys(context.Background())
	if err != nil {
		t.Fatalf("EncryptionKeys: %v", err)
	}
	if second.PublicKey != first.PublicKey {
		t.Fatal("the key was re-fetched rather than served from the cache")
	}
}

func TestParseValidUpto(t *testing.T) {
	cases := map[string]struct {
		in     string
		isZero bool
	}{
		"rfc3339":   {in: "2026-12-31T23:59:59Z"},
		"date only": {in: "2026-12-31"},
		"spaced":    {in: "2026-12-31 23:59:59"},
		"epoch":     {in: "1798761599"},
		"epoch ms":  {in: "1798761599000"},
		"empty":     {in: "", isZero: true},
		"nonsense":  {in: "whenever", isZero: true},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got := parseValidUpto(tc.in)
			if got.IsZero() != tc.isZero {
				t.Fatalf("parseValidUpto(%q) = %v, zero = %v, want zero = %v", tc.in, got, got.IsZero(), tc.isZero)
			}
		})
	}
}

// A key with no stated expiry must not be treated as already expired, or
// every signed call re-fetches.
func TestEncryptionKey_ZeroExpiryNeverExpires(t *testing.T) {
	key := EncryptionKey{PublicKey: "x"}
	if key.Expired(time.Now()) {
		t.Fatal("a key with no stated expiry reported itself expired")
	}

	dated := EncryptionKey{PublicKey: "x", ValidUpto: time.Now().Add(-time.Hour)}
	if !dated.Expired(time.Now()) {
		t.Fatal("a key past its expiry did not report itself expired")
	}
}
