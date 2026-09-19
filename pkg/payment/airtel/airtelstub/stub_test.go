package airtelstub

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// The stub is driven here over plain HTTP rather than through the client, so
// a shared misreading between the two cannot hide a stub bug.

func token(t *testing.T, s *Stub) string {
	t.Helper()

	body, _ := json.Marshal(map[string]string{
		"client_id":     "id",
		"client_secret": "secret",
		"grant_type":    "client_credentials",
	})
	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, s.URL()+pathAuth, bytes.NewReader(body))
	if err != nil {
		t.Fatalf("build token request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("token: %v", err)
	}
	defer resp.Body.Close()

	var decoded struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int64  `json:"expires_in"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&decoded); err != nil {
		t.Fatalf("decode token: %v", err)
	}
	if decoded.AccessToken == "" {
		t.Fatalf("no token issued (status %d)", resp.StatusCode)
	}
	return decoded.AccessToken
}

func do(t *testing.T, s *Stub, method, path, bearer string, body any) (status int, decoded map[string]any) {
	t.Helper()

	var reader io.Reader = http.NoBody
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("encode: %v", err)
		}
		reader = bytes.NewReader(encoded)
	}

	req, err := http.NewRequestWithContext(t.Context(), method, s.URL()+path, reader)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+bearer)
	req.Header.Set("X-Country", "KE")
	req.Header.Set("X-Currency", "KES")
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	defer resp.Body.Close()

	_ = json.NewDecoder(resp.Body).Decode(&decoded)
	return resp.StatusCode, decoded
}

func payment(id string) map[string]any {
	return map[string]any{
		"reference":   "MV-1",
		"subscriber":  map[string]any{"msisdn": "733123456"},
		"transaction": map[string]any{"amount": 500, "id": id},
	}
}

func TestStub_TokenTTLAndCredentials(t *testing.T) {
	s := New(t, WithCredentials("id", "secret"), WithTokenTTL(90*time.Second), WithHMACKey("key"))

	if s.HMACKey() != "key" {
		t.Fatalf("HMACKey() = %q", s.HMACKey())
	}
	if got := token(t, s); got == "" {
		t.Fatal("no token")
	}
	if s.TokensIssued() != 1 {
		t.Fatalf("issued %d tokens", s.TokensIssued())
	}

	body, _ := json.Marshal(map[string]string{
		"client_id":     "wrong",
		"client_secret": "wrong",
		"grant_type":    "client_credentials",
	})
	bad, err := http.NewRequestWithContext(t.Context(), http.MethodPost, s.URL()+pathAuth, bytes.NewReader(body))
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	bad.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(bad)
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("bad credentials gave %d, want 401", resp.StatusCode)
	}
}

func TestStub_RejectsUnknownToken(t *testing.T) {
	s := New(t)
	if status, _ := do(t, s, http.MethodPost, pathPayment, "not-a-token", payment("mv-1")); status != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", status)
	}
}

// The header contract is enforced so a client that drops one fails here.
func TestStub_RequiresLocaleHeaders(t *testing.T) {
	s := New(t)
	bearer := token(t, s)

	cases := map[string]map[string]string{
		"no country":  {"X-Currency": "KES"},
		"no currency": {"X-Country": "KE"},
	}

	for name, headers := range cases {
		t.Run(name, func(t *testing.T) {
			req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, s.URL()+pathBalance, http.NoBody)
			if err != nil {
				t.Fatalf("build: %v", err)
			}
			req.Header.Set("Authorization", "Bearer "+bearer)
			for k, v := range headers {
				req.Header.Set(k, v)
			}

			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatalf("do: %v", err)
			}
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400", resp.StatusCode)
			}
		})
	}
}

// A country-coded MSISDN is rejected, which is what makes the client's
// normalisation worth testing against this stub.
func TestStub_RejectsCountryCodedMSISDN(t *testing.T) {
	s := New(t)
	bearer := token(t, s)

	body := payment("mv-1")
	body["subscriber"] = map[string]any{"msisdn": "254733123456"}

	status, _ := do(t, s, http.MethodPost, pathPayment, bearer, body)
	if status != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", status)
	}
}

func TestStub_ReceiptOnlyOnSuccess(t *testing.T) {
	s := New(t)
	bearer := token(t, s)

	s.NextOutcome(StatusInProgress, codeInProcess)
	if status, _ := do(t, s, http.MethodPost, pathPayment, bearer, payment("mv-pending")); status != http.StatusOK {
		t.Fatalf("payment status = %d", status)
	}

	_, body := do(t, s, http.MethodGet, pathPayments+"mv-pending", bearer, nil)
	transaction := body["data"].(map[string]any)["transaction"].(map[string]any)
	if _, present := transaction["airtel_money_id"]; present {
		t.Fatal("an in-progress enquiry disclosed a receipt")
	}

	s.Settle("mv-pending")
	_, settled := do(t, s, http.MethodGet, pathPayments+"mv-pending", bearer, nil)
	transaction = settled["data"].(map[string]any)["transaction"].(map[string]any)
	if transaction["airtel_money_id"] == "" || transaction["airtel_money_id"] == nil {
		t.Fatal("a settled enquiry disclosed no receipt")
	}

	held, ok := s.Transaction("mv-pending")
	if !ok || held.Status != StatusSuccess {
		t.Fatalf("stub state after Settle: %+v", held)
	}
	if len(s.Transactions()) != 1 {
		t.Fatalf("Transactions() returned %d", len(s.Transactions()))
	}
}

func TestStub_EnquiryAndRefundNotFound(t *testing.T) {
	s := New(t)
	bearer := token(t, s)

	if status, _ := do(t, s, http.MethodGet, pathPayments+"nope", bearer, nil); status != http.StatusNotFound {
		t.Fatalf("enquiry status = %d, want 404", status)
	}

	refund := map[string]any{"transaction": map[string]any{"airtel_money_id": "AM999"}}
	if status, _ := do(t, s, http.MethodPost, pathRefund, bearer, refund); status != http.StatusNotFound {
		t.Fatalf("refund status = %d, want 404", status)
	}

	empty := map[string]any{"transaction": map[string]any{"airtel_money_id": ""}}
	if status, _ := do(t, s, http.MethodPost, pathRefund, bearer, empty); status != http.StatusBadRequest {
		t.Fatalf("refund status = %d, want 400", status)
	}
}

func TestStub_UserEnquiryStates(t *testing.T) {
	s := New(t)
	bearer := token(t, s)

	if status, _ := do(t, s, http.MethodGet, pathUsers+"733000003", bearer, nil); status != http.StatusNotFound {
		t.Fatalf("unknown subscriber gave %d, want 404", status)
	}

	_, body := do(t, s, http.MethodGet, pathUsers+"733000001", bearer, nil)
	data := body["data"].(map[string]any)
	if data["is_barred"] != true {
		t.Fatal("the barred subscriber is not reported barred")
	}

	_, pinless := do(t, s, http.MethodGet, pathUsers+"733000002", bearer, nil)
	if pinless["data"].(map[string]any)["is_pin_set"] != false {
		t.Fatal("the PIN-less subscriber is reported as having a PIN")
	}
}

func TestStub_Faults(t *testing.T) {
	s := New(t)
	bearer := token(t, s)

	s.FailNext(RouteBalance, http.StatusInternalServerError, "ROUTER001", "Application wallet not configured")
	if status, _ := do(t, s, http.MethodGet, pathBalance, bearer, nil); status != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", status)
	}
	if status, _ := do(t, s, http.MethodGet, pathBalance, bearer, nil); status != http.StatusOK {
		t.Fatalf("the fault was not consumed: status = %d", status)
	}

	// Dropped on a POST, not a GET: Go's transport silently retries an
	// idempotent request when a reused connection closes, which would mask
	// the drop here and does not happen for a payment.
	s.DropNext(RoutePayment)
	encoded, err := json.Marshal(payment("mv-dropped"))
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, s.URL()+pathPayment, bytes.NewReader(encoded))
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+bearer)
	req.Header.Set("X-Country", "KE")
	req.Header.Set("X-Currency", "KES")
	req.Header.Set("Content-Type", "application/json")
	if _, err := http.DefaultClient.Do(req); err == nil {
		t.Fatal("a dropped connection returned a response")
	}
}

func TestStub_CallbacksAreNotDeliveredUntilFlushed(t *testing.T) {
	var received int
	sink := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		received++
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(sink.Close)

	s := New(t)
	s.SetCallbackURL(sink.URL)
	bearer := token(t, s)

	if status, _ := do(t, s, http.MethodPost, pathPayment, bearer, payment("mv-1")); status != http.StatusOK {
		t.Fatalf("payment status = %d", status)
	}

	s.QueueCallback("mv-1", StatusSuccess)
	if s.Pending() != 1 {
		t.Fatalf("Pending() = %d, want 1", s.Pending())
	}
	if received != 0 {
		t.Fatal("a callback was delivered before it was flushed")
	}

	s.Deliver()
	if received != 1 {
		t.Fatalf("received %d callbacks after Deliver", received)
	}
	if s.Pending() != 0 {
		t.Fatalf("Pending() = %d after Deliver", s.Pending())
	}
}

func TestStub_Close(t *testing.T) {
	s := New(t)
	s.Close()
	closed, err := http.NewRequestWithContext(t.Context(), http.MethodGet, s.URL()+pathAuth, http.NoBody)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if _, err := http.DefaultClient.Do(closed); err == nil {
		t.Fatal("the stub answered after Close")
	}
}

func TestFormatMinor(t *testing.T) {
	cases := map[int64]string{
		0:         "0.00",
		5:         "0.05",
		50:        "0.50",
		100:       "1.00",
		123456:    "1,234.56",
		3_760_000: "37,600.00",
		-100:      "-1.00",
	}

	for minor, want := range cases {
		if got := formatMinor(minor); got != want {
			t.Fatalf("formatMinor(%d) = %q, want %q", minor, got, want)
		}
	}
}

func TestEpochParam(t *testing.T) {
	if got := epochParam("1798761599"); got.IsZero() {
		t.Fatal("a valid epoch parsed as zero")
	}
	for _, in := range []string{"", "0", "-1", "soon"} {
		if got := epochParam(in); !got.IsZero() {
			t.Fatalf("epochParam(%q) = %v, want zero", in, got)
		}
	}
}
