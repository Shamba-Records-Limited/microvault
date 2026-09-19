package airtel

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/Shamba-Records-Limited/microvault/pkg/payment/airtel/airtelstub"
)

func TestEnvironment(t *testing.T) {
	if !EnvironmentProduction.IsProduction() {
		t.Fatal("production does not report itself as production")
	}
	if EnvironmentStaging.IsProduction() {
		t.Fatal("staging reports itself as production")
	}
	if !EnvironmentStaging.Valid() || !EnvironmentProduction.Valid() {
		t.Fatal("a documented environment is invalid")
	}
	if Environment("uat").Valid() {
		t.Fatal("an undocumented environment is valid")
	}
}

func TestClientAccessors(t *testing.T) {
	client, err := New(Config{
		Environment:    EnvironmentStaging,
		ClientID:       "a",
		ClientSecret:   "b",
		Country:        "KE",
		Currency:       "KES",
		SigningEnabled: true,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if client.Environment() != EnvironmentStaging {
		t.Fatalf("Environment() = %q", client.Environment())
	}
	if !client.SigningEnabled() {
		t.Fatal("SigningEnabled() = false after configuring it on")
	}
}

func TestTransactionStatus_Valid(t *testing.T) {
	for _, status := range []TransactionStatus{StatusSuccess, StatusFailed, StatusAmbiguous, StatusInProgress, StatusExpired} {
		if !status.Valid() {
			t.Fatalf("%q is a documented status but reports invalid", status)
		}
	}
	if ParseTransactionStatus("TR").Valid() {
		t.Fatal("TR belongs to Merchant Collection and is not in this package's set")
	}
	if ParseTransactionStatus(" ts ") != StatusSuccess {
		t.Fatal("a padded lowercase status did not normalise")
	}
}

func TestAirtelError_Error(t *testing.T) {
	withCode := &AirtelError{StatusCode: 200, Status: Status{ResponseCode: CodeCollectionRefused, Message: "Refused"}}
	if got := withCode.Error(); got != "airtel DP00800001008: Refused" {
		t.Fatalf("Error() = %q", got)
	}

	bare := &AirtelError{StatusCode: 502, Status: Status{Message: "bad gateway"}}
	if got := bare.Error(); got != "airtel returned 502: bad gateway" {
		t.Fatalf("Error() = %q", got)
	}

	silent := &AirtelError{StatusCode: 500}
	if got := silent.Error(); got == "" {
		t.Fatal("an error with no message rendered empty")
	}
}

func TestAuthResponse_Seconds(t *testing.T) {
	cases := map[string]struct {
		raw  string
		want int64
	}{
		"number":   {raw: `180`, want: 180},
		"quoted":   {raw: `"180"`, want: 180},
		"absent":   {raw: ``, want: defaultTokenSeconds},
		"null":     {raw: `null`, want: defaultTokenSeconds},
		"nonsense": {raw: `"soon"`, want: defaultTokenSeconds},
		"zero":     {raw: `0`, want: defaultTokenSeconds},
		"negative": {raw: `-5`, want: defaultTokenSeconds},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			resp := authResponse{ExpiresIn: json.RawMessage(tc.raw)}
			if got := resp.seconds(); got != tc.want {
				t.Fatalf("seconds() = %d, want %d", got, tc.want)
			}
		})
	}
}

// A dropped connection is the ambiguity that makes a retry dangerous: the
// caller cannot tell whether Airtel accepted the payment.
func TestPayment_DroppedConnection(t *testing.T) {
	client, stub := newStubbed(t)
	stub.DropNext(airtelstub.RoutePayment)

	if _, err := client.Payment(context.Background(), validPayment()); err == nil {
		t.Fatal("expected a dropped connection to surface as an error")
	}
}

func TestKeyStore_Delete(t *testing.T) {
	store := NewMemoryKeyStore()
	ctx := context.Background()

	if err := store.Set(ctx, "k", EncryptionKey{PublicKey: "x"}); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if _, ok := store.Get(ctx, "k"); !ok {
		t.Fatal("the key was not stored")
	}
	if err := store.Delete(ctx, "k"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, ok := store.Get(ctx, "k"); ok {
		t.Fatal("the key survived a delete")
	}
}

func TestTokenStore_Delete(t *testing.T) {
	store := NewMemoryTokenStore()
	ctx := context.Background()

	if err := store.Set(ctx, "k", "token", time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if err := store.Delete(ctx, "k"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, _, ok := store.Get(ctx, "k"); ok {
		t.Fatal("the token survived a delete")
	}
}

// A signed call cannot proceed without a key, and the failure has to name
// that rather than surfacing as a signature mismatch later.
func TestSignedPayment_FailsWhenKeysAreUnavailable(t *testing.T) {
	stub := airtelstub.New(t)
	client, err := New(Config{
		Environment:    EnvironmentStaging,
		ClientID:       "client-id",
		ClientSecret:   "client-secret",
		BaseURL:        stub.URL(),
		SigningEnabled: true,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	stub.FailNext(airtelstub.RouteEncryptionKeys, http.StatusForbidden, CodeEncryptionFailed, "Error while fetching encryption key")

	if _, err := client.Payment(context.Background(), validPayment()); err == nil {
		t.Fatal("a signed payment with no key must fail")
	}
}
