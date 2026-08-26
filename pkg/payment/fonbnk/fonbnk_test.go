package fonbnk

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestPadBase64(t *testing.T) {
	cases := map[string]string{
		"abc":  "abc=", // len 3 -> +1
		"ab":   "ab==", // len 2 -> +2
		"abcd": "abcd", // len 4 -> +0
		"":     "",     // empty -> +0
	}
	for in, want := range cases {
		if got := padBase64(in); got != want {
			t.Errorf("padBase64(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestGenerateSignature(t *testing.T) {
	// "c2VjcmV0" is valid base64 ("secret").
	sig, err := generateSignature("c2VjcmV0", "1700000000000", "/v1/quote")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if sig == "" {
		t.Error("signature should be non-empty")
	}
	// Deterministic: same inputs -> same signature.
	sig2, _ := generateSignature("c2VjcmV0", "1700000000000", "/v1/quote")
	if sig != sig2 {
		t.Error("signature should be deterministic for identical inputs")
	}
	// Different endpoint -> different signature.
	sig3, _ := generateSignature("c2VjcmV0", "1700000000000", "/v1/other")
	if sig == sig3 {
		t.Error("signature should change with the endpoint")
	}
	// A secret that isn't valid base64 (even after padding) errors.
	if _, err := generateSignature("!!!!", "1", "/x"); err == nil {
		t.Error("expected error for invalid base64 secret")
	}
}

type capTransport struct {
	req  *http.Request
	resp *http.Response
}

func (c *capTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	c.req = r
	return c.resp, nil
}

func TestRoundTrip_InjectsHeaders(t *testing.T) {
	cap := &capTransport{resp: &http.Response{StatusCode: 200, Body: http.NoBody}}
	tr := &fonbnkTransport{clientID: "cid-1", clientSecret: "c2VjcmV0", base: cap}

	req := httptest.NewRequest(http.MethodGet, "https://api.fonbnk.test/v1/quote?amount=10", http.NoBody)
	if _, err := tr.RoundTrip(req); err != nil {
		t.Fatalf("RoundTrip: %v", err)
	}
	h := cap.req.Header
	if h.Get("x-client-id") != "cid-1" {
		t.Errorf("x-client-id = %q", h.Get("x-client-id"))
	}
	for _, key := range []string{"x-timestamp", "x-signature"} {
		if h.Get(key) == "" {
			t.Errorf("%s header not set", key)
		}
	}
	if h.Get("Content-Type") != "application/json" {
		t.Errorf("Content-Type = %q", h.Get("Content-Type"))
	}
}

func TestRoundTrip_SignatureError(t *testing.T) {
	tr := &fonbnkTransport{clientID: "c", clientSecret: "!!!!", base: &capTransport{}}
	req := httptest.NewRequest(http.MethodGet, "https://api.fonbnk.test/x", http.NoBody)
	if _, err := tr.RoundTrip(req); err == nil {
		t.Error("expected RoundTrip to fail on signature error")
	}
}

func TestNewFonbnkAdapter(t *testing.T) {
	a := NewFonbnkAdapter("id", "secret", "https://api.fonbnk.test")
	if a == nil || a.baseURL != "https://api.fonbnk.test" || a.httpClient == nil {
		t.Errorf("adapter not wired: %+v", a)
	}
}
