package airtel

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/Shamba-Records-Limited/microvault/pkg/payment/airtel/airtelstub"
)

// receiver records the callback bodies delivered to it.
type receiver struct {
	mu     sync.Mutex
	bodies [][]byte
	server *httptest.Server
}

func newReceiver(t *testing.T) *receiver {
	t.Helper()

	r := &receiver{}
	r.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		body, _ := io.ReadAll(req.Body)
		r.mu.Lock()
		r.bodies = append(r.bodies, body)
		r.mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(r.server.Close)
	return r
}

func (r *receiver) received() [][]byte {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([][]byte(nil), r.bodies...)
}

func TestParseCallback(t *testing.T) {
	raw := []byte(`{"transaction":{"id":"mv-1","message":"Success","status_code":"TS","airtel_money_id":"AM1"}}`)

	cb, err := ParseCallback(raw)
	if err != nil {
		t.Fatalf("ParseCallback: %v", err)
	}
	if cb.Transaction.ID != "mv-1" {
		t.Fatalf("id = %q", cb.Transaction.ID)
	}
	if cb.TransactionStatus() != StatusSuccess {
		t.Fatalf("status = %q", cb.TransactionStatus())
	}
}

// A body that cannot be correlated must not be staged.
func TestParseCallback_RejectsBodyWithoutTransactionID(t *testing.T) {
	for name, raw := range map[string]string{
		"no id":          `{"transaction":{"status_code":"TS"}}`,
		"no transaction": `{"hash":"abc"}`,
		"garbage":        `not json`,
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := ParseCallback([]byte(raw)); err == nil {
				t.Fatal("expected the callback to be rejected")
			}
		})
	}
}

// No callback is terminal — not even TF. Airtel documents it as carrying
// intermediate or final status with nothing to distinguish them.
func TestCallback_IsNeverTerminal(t *testing.T) {
	for _, status := range []string{"TS", "TF"} {
		var cb Callback
		cb.Transaction.ID = "mv-1"
		cb.Transaction.StatusCode = status

		if cb.Terminal() {
			t.Fatalf("a %s callback reported itself terminal", status)
		}
		if !cb.TransactionStatus().ShouldEnquire(SourceCallback) {
			t.Fatalf("a %s callback does not ask for an enquiry", status)
		}
	}

	// The same values from an enquiry are terminal, which is the difference
	// the source argument exists to carry.
	if !StatusFailed.Terminal(SourceEnquiry) {
		t.Fatal("a TF enquiry should be terminal")
	}
	if StatusInProgress.Terminal(SourceEnquiry) {
		t.Fatal("a TIP enquiry is not terminal")
	}
}

func TestVerifyCallbackHash(t *testing.T) {
	const key = "callback-key"
	body := []byte(`{"transaction":{"id":"mv-1","status_code":"TS"}}`)

	t.Run("without hash", func(t *testing.T) {
		hash := SignCallback(body, key)

		var generic map[string]any
		if err := json.Unmarshal(body, &generic); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		generic["hash"] = hash
		signed, err := json.Marshal(generic)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}

		result, err := VerifyCallbackHash(signed, key)
		if err != nil {
			t.Fatalf("VerifyCallbackHash: %v", err)
		}
		if !result.Verified {
			t.Fatal("a correctly signed callback did not verify")
		}
		if result.Variant != VariantWithoutHash {
			t.Fatalf("variant = %q, want %q", result.Variant, VariantWithoutHash)
		}
	})

	t.Run("tampered", func(t *testing.T) {
		var generic map[string]any
		if err := json.Unmarshal(body, &generic); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		generic["hash"] = SignCallback(body, key)
		generic["transaction"].(map[string]any)["id"] = "mv-2"
		tampered, err := json.Marshal(generic)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}

		result, err := VerifyCallbackHash(tampered, key)
		if err != nil {
			t.Fatalf("VerifyCallbackHash: %v", err)
		}
		if result.Verified {
			t.Fatal("a tampered callback verified")
		}
	})

	t.Run("wrong key", func(t *testing.T) {
		var generic map[string]any
		if err := json.Unmarshal(body, &generic); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		generic["hash"] = SignCallback(body, key)
		signed, err := json.Marshal(generic)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}

		result, err := VerifyCallbackHash(signed, "not-the-key")
		if err != nil {
			t.Fatalf("VerifyCallbackHash: %v", err)
		}
		if result.Verified {
			t.Fatal("a callback verified under the wrong key")
		}
	})

	t.Run("no key configured", func(t *testing.T) {
		if _, err := VerifyCallbackHash(body, ""); err == nil {
			t.Fatal("verifying without a key must fail rather than pass")
		}
	})

	t.Run("no hash present", func(t *testing.T) {
		if _, err := VerifyCallbackHash(body, key); err == nil {
			t.Fatal("verifying a body with no hash must fail rather than pass")
		}
	})
}

// The behaviour with no M-Pesa equivalent: one transaction, two callbacks,
// the first of which is not the last word.
func TestCallback_IntermediateThenFinal(t *testing.T) {
	sink := newReceiver(t)
	stub := airtelstub.New(t, airtelstub.WithCallbackURL(sink.server.URL))

	client, err := New(Config{
		Environment:     EnvironmentStaging,
		ClientID:        "client-id",
		ClientSecret:    "client-secret",
		BaseURL:         stub.URL(),
		CallbackHMACKey: stub.HMACKey(),
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	stub.NextOutcome(airtelstub.StatusInProgress, "DP00800001006")
	if _, err := client.Payment(context.Background(), validPayment()); err != nil {
		t.Fatalf("Payment: %v", err)
	}

	stub.DeliverIntermediateThenFinal("mv-txn-1", airtelstub.StatusSuccess)

	bodies := sink.received()
	if len(bodies) != 2 {
		t.Fatalf("received %d callbacks, want 2", len(bodies))
	}

	first, err := ParseCallback(bodies[0])
	if err != nil {
		t.Fatalf("first callback: %v", err)
	}
	if first.TransactionStatus() != StatusInProgress {
		t.Fatalf("first callback status = %q, want TIP", first.TransactionStatus())
	}
	if first.Transaction.AirtelMoneyID != "" {
		t.Fatalf("an in-progress callback disclosed a receipt: %q", first.Transaction.AirtelMoneyID)
	}

	final, err := ParseCallback(bodies[1])
	if err != nil {
		t.Fatalf("final callback: %v", err)
	}
	if final.TransactionStatus() != StatusSuccess {
		t.Fatalf("final callback status = %q, want TS", final.TransactionStatus())
	}
	if final.Transaction.AirtelMoneyID == "" {
		t.Fatal("a settled callback disclosed no receipt")
	}

	// Both verify, and neither is terminal on its own.
	for i, raw := range bodies {
		result, err := VerifyCallbackHash(raw, stub.HMACKey())
		if err != nil {
			t.Fatalf("callback %d: %v", i, err)
		}
		if !result.Verified {
			t.Fatalf("callback %d did not verify", i)
		}
	}
	if first.Terminal() || final.Terminal() {
		t.Fatal("a callback reported itself terminal")
	}

	// The enquiry is what settles it.
	enquiry, err := client.Enquiry(context.Background(), "mv-txn-1")
	if err != nil {
		t.Fatalf("Enquiry: %v", err)
	}
	if !enquiry.TransactionStatus().Terminal(SourceEnquiry) {
		t.Fatal("the enquiry is not terminal")
	}
	receipt, ok := enquiry.Receipt()
	if !ok || receipt != final.Transaction.AirtelMoneyID {
		t.Fatalf("enquiry receipt %q does not match the callback's %q", receipt, final.Transaction.AirtelMoneyID)
	}
}

// The transaction-member reading must verify too, and report itself, so the
// first live callback records which reading Airtel uses rather than leaving
// it inferred.
func TestVerifyCallbackHash_TransactionVariant(t *testing.T) {
	sink := newReceiver(t)
	stub := airtelstub.New(t,
		airtelstub.WithCallbackURL(sink.server.URL),
		airtelstub.WithHashOverTransaction(),
	)

	client, err := New(Config{
		Environment:     EnvironmentStaging,
		ClientID:        "client-id",
		ClientSecret:    "client-secret",
		BaseURL:         stub.URL(),
		CallbackHMACKey: stub.HMACKey(),
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	if _, err := client.Payment(context.Background(), validPayment()); err != nil {
		t.Fatalf("Payment: %v", err)
	}
	stub.QueueCallback("mv-txn-1", airtelstub.StatusSuccess)
	stub.Deliver()

	bodies := sink.received()
	if len(bodies) != 1 {
		t.Fatalf("received %d callbacks, want 1", len(bodies))
	}

	result, err := VerifyCallbackHash(bodies[0], stub.HMACKey())
	if err != nil {
		t.Fatalf("VerifyCallbackHash: %v", err)
	}
	if !result.Verified {
		t.Fatal("a transaction-signed callback did not verify")
	}
	if result.Variant != VariantTransaction {
		t.Fatalf("variant = %q, want %q", result.Variant, VariantTransaction)
	}
}
