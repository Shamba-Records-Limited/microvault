package darajastub

import (
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func tokenFor(t *testing.T, s *Stub) string {
	t.Helper()
	req, _ := http.NewRequestWithContext(t.Context(), http.MethodGet, s.URL()+"/oauth/v1/generate?grant_type=client_credentials", http.NoBody)
	req.SetBasicAuth("key", "secret")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("token request: %v", err)
	}
	defer resp.Body.Close()

	var body struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   any    `json:"expires_in"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode token: %v", err)
	}
	if _, ok := body.ExpiresIn.(string); !ok {
		t.Errorf("expires_in decoded as %T, want a quoted string as Daraja sends it", body.ExpiresIn)
	}
	return body.AccessToken
}

func TestAuth_MintInvalidatesPrevious(t *testing.T) {
	s := New(t)

	first := tokenFor(t, s)
	second := tokenFor(t, s)
	if first == second {
		t.Fatal("two mints produced the same token")
	}
	if s.Mints() != 2 {
		t.Errorf("Mints() = %d, want 2", s.Mints())
	}

	s.HandleAuthed("probe", "/probe", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
	})

	if status := probe(t, s, first); status != http.StatusUnauthorized {
		t.Errorf("superseded token got %d, want 401", status)
	}
	if status := probe(t, s, second); status != http.StatusOK {
		t.Errorf("current token got %d, want 200", status)
	}
}

// A client that recognises only one of Daraja's four spellings of a rejected
// token can recover on one API and not the other three, so the stub cycles them.
func TestAuth_RotatesInvalidTokenCodes(t *testing.T) {
	seen := make(map[string]bool)
	for mints := range len(invalidTokenCodes) {
		s := New(t)
		s.HandleAuthed("probe", "/probe", func(w http.ResponseWriter, _ *http.Request) {})
		for range mints {
			tokenFor(t, s)
		}

		req, _ := http.NewRequestWithContext(t.Context(), http.MethodGet, s.URL()+"/probe", http.NoBody)
		req.Header.Set("Authorization", "Bearer wrong")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("probe: %v", err)
		}
		raw, _ := io.ReadAll(resp.Body)
		resp.Body.Close()

		var body apiError
		_ = json.Unmarshal(raw, &body)
		seen[body.ErrorCode] = true
	}
	if len(seen) != len(invalidTokenCodes) {
		t.Errorf("stub returned %d distinct codes %v, want all %d", len(seen), seen, len(invalidTokenCodes))
	}
}

func TestAuth_RejectsBadCredentials(t *testing.T) {
	s := New(t, WithConsumerCredentials("key", "secret"))

	req, _ := http.NewRequestWithContext(t.Context(), http.MethodGet, s.URL()+"/oauth/v1/generate?grant_type=client_credentials", http.NoBody)
	req.SetBasicAuth("key", "wrong")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("token request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", resp.StatusCode)
	}
}

func TestAuth_RejectsWrongGrantType(t *testing.T) {
	s := New(t)
	req, _ := http.NewRequestWithContext(t.Context(), http.MethodGet, s.URL()+"/oauth/v1/generate?grant_type=password", http.NoBody)
	req.SetBasicAuth("key", "secret")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("token request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", resp.StatusCode)
	}
}

func probe(t *testing.T, s *Stub, token string) int {
	t.Helper()
	req, _ := http.NewRequestWithContext(t.Context(), http.MethodGet, s.URL()+"/probe", http.NoBody)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("probe: %v", err)
	}
	defer resp.Body.Close()
	return resp.StatusCode
}

func TestCredential_DecryptsAndCompares(t *testing.T) {
	s := New(t, WithInitiatorPassword("hunter2"))

	key, err := parsePublic(s.Certificate())
	if err != nil {
		t.Fatalf("parse certificate: %v", err)
	}
	encrypt := func(password string) string {
		//nolint:staticcheck // SA1019: mirrors the client's PKCS #1 v1.5 SecurityCredential.
		cipher, err := rsa.EncryptPKCS1v15(rand.Reader, key, []byte(password))
		if err != nil {
			t.Fatalf("encrypt: %v", err)
		}
		return base64.StdEncoding.EncodeToString(cipher)
	}

	if code, ok := s.checkSecurityCredential(encrypt("hunter2")); !ok || code != 0 {
		t.Errorf("correct password gave code %d, ok %v", code, ok)
	}
	if code, ok := s.checkSecurityCredential(encrypt("wrong")); ok || code != 2001 {
		t.Errorf("wrong password gave code %d, ok %v; want 2001", code, ok)
	}
	if code, ok := s.checkSecurityCredential("not base64!"); ok || code != 2001 {
		t.Errorf("malformed credential gave code %d, ok %v; want 2001", code, ok)
	}

	s.LockCredential()
	if code, ok := s.checkSecurityCredential(encrypt("hunter2")); ok || code != 8006 {
		t.Errorf("locked credential gave code %d, ok %v; want 8006", code, ok)
	}
}

// A credential encrypted with a different certificate must not verify. This is
// the mistake that produces an undiagnosable 2001 at go-live.
func TestCredential_WrongCertificateFails(t *testing.T) {
	s := New(t, WithInitiatorPassword("hunter2"))
	otherCert, _ := newKeyPair(t)

	key, err := parsePublic(otherCert)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	//nolint:staticcheck // SA1019: mirrors the client's PKCS #1 v1.5 SecurityCredential.
	cipher, _ := rsa.EncryptPKCS1v15(rand.Reader, key, []byte("hunter2"))

	if _, ok := s.checkSecurityCredential(base64.StdEncoding.EncodeToString(cipher)); ok {
		t.Error("a credential encrypted with the wrong certificate verified")
	}
}

func TestLedger(t *testing.T) {
	s := New(t)

	s.Credit(174379, AccountUtility, 50_000)
	if got := s.Balance(174379, AccountUtility); got != 50_000 {
		t.Errorf("balance = %d", got)
	}

	if ok := s.ledger.debit(174379, AccountUtility, 20_000); !ok {
		t.Error("debit within balance was refused")
	}
	if got := s.Balance(174379, AccountUtility); got != 30_000 {
		t.Errorf("balance after debit = %d", got)
	}

	if ok := s.ledger.debit(174379, AccountUtility, 40_000); ok {
		t.Error("debit beyond balance was allowed")
	}
	if got := s.Balance(174379, AccountUtility); got != 30_000 {
		t.Errorf("a refused debit changed the balance to %d", got)
	}

	// Accounts do not bleed into one another: draining Utility must leave
	// Working untouched, which is what makes a B2C/B2B mix testable.
	if got := s.Balance(174379, AccountWorking); got != 0 {
		t.Errorf("working balance = %d, want 0", got)
	}
}

func TestFormatBalance(t *testing.T) {
	cases := map[int64]string{0: "0.00", 5: "0.05", 618_683: "6186.83", 100: "1.00"}
	for minor, want := range cases {
		if got := formatBalance(minor); got != want {
			t.Errorf("formatBalance(%d) = %q, want %q", minor, got, want)
		}
	}
}

func TestCallbacks_DeliveredOnlyWhenAsked(t *testing.T) {
	var received [][]byte
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		received = append(received, body)
		w.WriteHeader(http.StatusOK)
	}))
	defer target.Close()

	s := New(t)
	s.queue("probe", CallbackResult, target.URL, map[string]int{"n": 1})
	s.queue("probe", CallbackResult, target.URL, map[string]int{"n": 2})

	if s.Pending() != 2 {
		t.Fatalf("Pending() = %d, want 2", s.Pending())
	}
	// Nothing is delivered on a timer, so waiting changes nothing.
	if len(received) != 0 {
		t.Fatalf("a callback arrived before Deliver was called")
	}

	s.DeliverNext()
	if len(received) != 1 || s.Pending() != 1 {
		t.Fatalf("after DeliverNext: %d received, %d pending", len(received), s.Pending())
	}

	s.Deliver()
	if len(received) != 2 || s.Pending() != 0 {
		t.Fatalf("after Deliver: %d received, %d pending", len(received), s.Pending())
	}
	if string(received[0]) != `{"n":1}`+"\n" && string(received[0]) != `{"n":1}` {
		t.Errorf("callbacks arrived out of order: %s", received[0])
	}
}

func TestCallbacks_Duplicate(t *testing.T) {
	count := 0
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		count++
		w.WriteHeader(http.StatusOK)
	}))
	defer target.Close()

	s := New(t)
	s.DuplicateCallbacks()
	s.queue("probe", CallbackResult, target.URL, map[string]int{"n": 1})

	deliveries := s.Deliver()
	if count != 2 || len(deliveries) != 2 {
		t.Errorf("count = %d, deliveries = %d; want 2 and 2", count, len(deliveries))
	}
}

func TestFaults_FailNextIsConsumedOnce(t *testing.T) {
	s := New(t)
	s.HandleAuthed("probe", "/probe", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
	})
	token := tokenFor(t, s)

	s.FailNext("probe", http.StatusInternalServerError, "500.003.1001", "Internal Server Error")
	if status := probe(t, s, token); status != http.StatusInternalServerError {
		t.Errorf("first probe = %d, want 500", status)
	}
	if status := probe(t, s, token); status != http.StatusOK {
		t.Errorf("second probe = %d, want 200 — the fault should be consumed", status)
	}
}

// A timeout fault changes where the callback lands, not the acknowledgement, so
// it must survive the synchronous leg.
func TestFaults_TimeoutIsNotConsumedByAcknowledgement(t *testing.T) {
	s := New(t)
	s.TimeoutNext("probe")

	if f := s.takeFault("probe"); f != nil {
		t.Error("takeFault consumed a timeout fault")
	}
	if !s.takeTimeout("probe") {
		t.Error("takeTimeout did not see the timeout")
	}
	if s.takeTimeout("probe") {
		t.Error("takeTimeout returned a consumed timeout twice")
	}
}

func TestWithTokenTTL(t *testing.T) {
	s := New(t, WithTokenTTL(120*time.Second))
	req, _ := http.NewRequestWithContext(t.Context(), http.MethodGet, s.URL()+"/oauth/v1/generate?grant_type=client_credentials", http.NoBody)
	req.SetBasicAuth("k", "s")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("token request: %v", err)
	}
	defer resp.Body.Close()

	raw, _ := io.ReadAll(resp.Body)
	if want := `"expires_in":"120"`; !strings.Contains(string(raw), want) {
		t.Errorf("body %s does not contain %s", raw, want)
	}
}
