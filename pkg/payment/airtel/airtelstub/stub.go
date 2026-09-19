package airtelstub

import (
	"crypto/rsa"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// Route names an endpoint family, for fault injection.
type Route string

// The routes the stub serves.
const (
	RouteAuth           Route = "auth"
	RouteEncryptionKeys Route = "encryption_keys"
	RoutePayment        Route = "payment"
	RouteRefund         Route = "refund"
	RouteEnquiry        Route = "enquiry"
	RouteUserEnquiry    Route = "user_enquiry"
	RouteBalance        Route = "balance"
	RouteSummary        Route = "summary"
)

// Paths the stub serves, spelled as Airtel spells them. They are literals
// rather than shared constants on purpose: a client that drifts from the
// documented path should fail here.
const (
	pathAuth       = "/auth/oauth2/token"
	pathKeys       = "/v1/rsa/encryption-keys"
	pathPayment    = "/merchant/v1/payments/"
	pathRefund     = "/standard/v1/payments/refund"
	pathPayments   = "/standard/v1/payments/"
	pathUsers      = "/standard/v1/users/"
	pathBalance    = "/standard/v2/users/balance"
	pathSummary    = "/merchant/v1/transactions"
	defaultHMACKey = "stub-callback-hmac-key"
)

// Response codes the stub emits. Duplicated from the client's vocabulary
// deliberately; see the package doc.
const (
	codeSuccess    = "DP00800001001"
	codeInProcess  = "DP00800001006"
	codeAmbiguous  = "DP00800001000"
	codeForbidden  = "DP00800001026"
	codeNotFound   = "DP00800001025"
	codeKeysOK     = "DP02010001001"
	codeKYCOK      = "DP02200000001"
	codeKYCMissing = "DP02200000002"
	codeAccountOK  = "DP02100000001"
	codeESBSuccess = "ESB000010"
)

// Option configures a Stub.
type Option func(*Stub)

// WithCredentials sets the client id and secret the token endpoint expects.
// Defaults accept anything non-empty.
func WithCredentials(id, secret string) Option {
	return func(s *Stub) { s.clientID, s.clientSecret = id, secret }
}

// WithTokenTTL sets the lifetime the token endpoint advertises. Airtel's own
// is 180 seconds; zero produces a token that is already expired, for
// exercising the refresh path.
func WithTokenTTL(ttl time.Duration) Option {
	return func(s *Stub) { s.tokenTTL = ttl }
}

// WithHMACKey sets the callback signing key.
func WithHMACKey(key string) Option {
	return func(s *Stub) { s.hmacKey = key }
}

// WithHashOverTransaction makes callback hashes cover only the transaction
// member, rather than the whole body without its hash. Airtel documents the
// mechanism without saying which, so both are reachable.
func WithHashOverTransaction() Option {
	return func(s *Stub) { s.hashOverTransaction = true }
}

// WithRequiredSigning rejects any payment or refund that arrives without
// valid x-key and x-signature headers, as an application with Collection v2
// message signing enabled does.
func WithRequiredSigning() Option {
	return func(s *Stub) { s.requireSigning = true }
}

// WithCallbackURL sets where callbacks are delivered.
func WithCallbackURL(url string) Option {
	return func(s *Stub) { s.callbackURL = url }
}

// Stub is an in-process Airtel. Point an airtel.Config at URL and drive it.
type Stub struct {
	t      testing.TB
	server *httptest.Server
	mux    *http.ServeMux

	mu sync.Mutex

	clientID     string
	clientSecret string

	privateKey *rsa.PrivateKey
	publicKey  string

	tokenTTL     time.Duration
	currentToken string
	issued       int

	hmacKey             string
	hashOverTransaction bool
	requireSigning      bool
	callbackURL         string

	transactions map[string]*Transaction
	order        []string
	balanceMinor int64

	nextOutcome *outcome
	faults      map[Route]*fault

	pending   []queuedCallback
	deliverer *http.Client
}

// outcome is the resolution queued for the next payment.
type outcome struct {
	status string
	code   string
}

// New starts a stub and registers its shutdown with t.
func New(t testing.TB, opts ...Option) *Stub {
	t.Helper()

	key, publicPEM := sharedKeyPair(t)
	s := &Stub{
		t:            t,
		mux:          http.NewServeMux(),
		privateKey:   key,
		publicKey:    publicPEM,
		tokenTTL:     180 * time.Second,
		hmacKey:      defaultHMACKey,
		transactions: make(map[string]*Transaction),
		balanceMinor: 3_760_000,
		faults:       make(map[Route]*fault),
		deliverer:    &http.Client{Timeout: 5 * time.Second},
	}
	for _, opt := range opts {
		opt(s)
	}

	s.routeAuth()
	s.routeKeys()
	s.routeCollection()
	s.routeLookups()

	s.server = httptest.NewServer(s.mux)
	t.Cleanup(s.server.Close)
	return s
}

// URL is the stub's base URL.
func (s *Stub) URL() string { return s.server.URL }

// Close stops the stub. Registered with t automatically; calling it early is
// safe.
func (s *Stub) Close() { s.server.Close() }

// HMACKey is the callback signing key, for a client's CallbackHMACKey.
func (s *Stub) HMACKey() string { return s.hmacKey }

// SetCallbackURL points callback delivery at a handler.
func (s *Stub) SetCallbackURL(url string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.callbackURL = url
}

// SetBalanceMinor sets what the balance endpoint reports.
func (s *Stub) SetBalanceMinor(minor int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.balanceMinor = minor
}

// TokensIssued reports how many tokens have been minted, so a test can show
// the single-flight collapsed concurrent mints rather than assuming it.
func (s *Stub) TokensIssued() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.issued
}

// handle registers an unauthenticated route.
func (s *Stub) handle(path string, fn http.HandlerFunc) {
	s.mux.HandleFunc(path, fn)
}

// handleAuthed registers a route behind the bearer check and fault queue.
func (s *Stub) handleAuthed(route Route, path string, fn http.HandlerFunc) {
	s.mux.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) {
		if !s.authorize(w, r) {
			return
		}
		if f := s.takeFault(route); f != nil {
			f.write(w)
			return
		}
		fn(w, r)
	})
}

// envelope is Airtel's universal status wrapper.
type envelope struct {
	Code         string `json:"code"`
	Message      string `json:"message"`
	Success      bool   `json:"success"`
	ResultCode   string `json:"result_code"`
	ResponseCode string `json:"response_code"`
}

func ok(responseCode, message string) envelope {
	return envelope{
		Code:         "200",
		Message:      message,
		Success:      true,
		ResultCode:   codeESBSuccess,
		ResponseCode: responseCode,
	}
}

func failed(httpStatus, responseCode, message string) envelope {
	return envelope{
		Code:         httpStatus,
		Message:      message,
		Success:      false,
		ResponseCode: responseCode,
	}
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

// writeEnvelope renders a status-only response.
func writeEnvelope(w http.ResponseWriter, status int, env envelope) {
	writeJSON(w, status, map[string]any{"status": env})
}

// requireLocaleHeaders enforces the casing an endpoint documents.
//
// HTTP headers are case-insensitive and Go canonicalises them, so this cannot
// catch a client sending X-Country where x-country is documented. What it does
// catch is the header being absent altogether, which is the failure the
// casing confusion actually produces — a client that renames the header to a
// spelling the gateway does not read at all.
func requireLocaleHeaders(w http.ResponseWriter, r *http.Request) bool {
	if strings.TrimSpace(r.Header.Get("X-Country")) == "" {
		writeEnvelope(w, http.StatusBadRequest, failed("400", "ROUTER003", "Mandatory request header missing: country"))
		return false
	}
	if strings.TrimSpace(r.Header.Get("X-Currency")) == "" {
		writeEnvelope(w, http.StatusBadRequest, failed("400", "ROUTER119", "Mandatory request header missing: currency"))
		return false
	}
	return true
}
