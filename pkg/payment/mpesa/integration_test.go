package mpesa

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"
	"testing"

	"github.com/Shamba-Records-Limited/microvault/pkg/payment/mpesa/darajastub"
)

func stubClient(t *testing.T, opts ...darajastub.Option) (*Client, *darajastub.Stub) {
	t.Helper()
	stub := darajastub.New(t, append([]darajastub.Option{
		darajastub.WithConsumerCredentials("consumer-key", "consumer-secret"),
		darajastub.WithInitiatorPassword("stub-initiator-password"),
	}, opts...)...)

	c, err := New(Config{
		Environment:       EnvironmentSandbox,
		ConsumerKey:       "consumer-key",
		ConsumerSecret:    "consumer-secret",
		InitiatorName:     "apiop37",
		InitiatorPassword: "stub-initiator-password",
		BaseURL:           stub.URL(),
		Certificate:       stub.Certificate(),
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return c, stub
}

type probeResponse struct {
	OK bool `json:"ok"`
}

// Against an adversarial stub — one that invalidates the previous token on
// every mint, as Daraja does — the single-flight has to hold. A permissive
// stub would let this pass with no single-flight at all.
func TestIntegration_ConcurrentCallersMintOnce(t *testing.T) {
	c, stub := stubClient(t)
	stub.HandleAuthed("probe", "/probe", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(probeResponse{OK: true})
	})

	var wg sync.WaitGroup
	errs := make([]error, 32)
	for i := range errs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, errs[i] = call[probeResponse](context.Background(), c, mpesaErr("probe"), http.MethodGet, "/probe", nil)
		}()
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Errorf("caller %d: %v", i, err)
		}
	}
	if got := stub.Mints(); got != 1 {
		t.Errorf("mints = %d, want 1", got)
	}
}

// The token this process holds can be invalidated by another replica minting
// one. The client must notice, evict and recover without the caller seeing it.
func TestIntegration_RecoversFromSupersededToken(t *testing.T) {
	c, stub := stubClient(t)
	stub.HandleAuthed("probe", "/probe", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(probeResponse{OK: true})
	})

	if _, err := call[probeResponse](context.Background(), c, mpesaErr("probe"), http.MethodGet, "/probe", nil); err != nil {
		t.Fatalf("first call: %v", err)
	}
	if got := stub.Mints(); got != 1 {
		t.Fatalf("mints = %d, want 1", got)
	}

	stub.ExpireToken()

	out, err := call[probeResponse](context.Background(), c, mpesaErr("probe"), http.MethodGet, "/probe", nil)
	if err != nil {
		t.Fatalf("call after the token was superseded: %v", err)
	}
	if !out.OK {
		t.Error("recovered call did not return the payload")
	}
	if got := stub.Mints(); got != 2 {
		t.Errorf("mints = %d, want 2 (one recovery mint)", got)
	}
}

// Proving the RSA path end to end needs a counterparty that holds the private
// key. Asserting the credential is non-empty base64 would pass for one
// encrypted with the wrong certificate — the mistake that yields an
// undiagnosable 2001 at go-live.
func TestIntegration_SecurityCredentialVerifiesAgainstStub(t *testing.T) {
	c, stub := stubClient(t)

	credential, err := c.SecurityCredential()
	if err != nil {
		t.Fatalf("SecurityCredential: %v", err)
	}
	if code, ok := stub.VerifySecurityCredential(credential); !ok || code != 0 {
		t.Errorf("stub rejected the credential: code %d", code)
	}
}

func TestIntegration_SecurityCredentialWithWrongPassword(t *testing.T) {
	_, stub := stubClient(t)

	c, err := New(Config{
		Environment: EnvironmentSandbox, ConsumerKey: "consumer-key", ConsumerSecret: "consumer-secret",
		InitiatorPassword: "not-the-password",
		BaseURL:           stub.URL(),
		Certificate:       stub.Certificate(),
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	credential, err := c.SecurityCredential()
	if err != nil {
		t.Fatalf("SecurityCredential: %v", err)
	}
	if code, ok := stub.VerifySecurityCredential(credential); ok || code != 2001 {
		t.Errorf("wrong password gave code %d, ok %v; want 2001", code, ok)
	}
}

func TestIntegration_LockedCredential(t *testing.T) {
	c, stub := stubClient(t)
	stub.LockCredential()

	credential, _ := c.SecurityCredential()
	if code, ok := stub.VerifySecurityCredential(credential); ok || code != 8006 {
		t.Errorf("locked credential gave code %d, ok %v; want 8006", code, ok)
	}
}
