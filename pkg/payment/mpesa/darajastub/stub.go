package darajastub

import (
	"crypto/rsa"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

// Route names an endpoint family, for fault injection.
type Route string

// The routes the stub serves.
const (
	RouteAuth Route = "auth"
)

// Option configures a Stub.
type Option func(*Stub)

// WithConsumerCredentials sets the key and secret the stub expects on the
// token endpoint. Defaults accept anything non-empty.
func WithConsumerCredentials(key, secret string) Option {
	return func(s *Stub) { s.consumerKey, s.consumerSecret = key, secret }
}

// WithInitiatorPassword sets the plaintext the stub expects a
// SecurityCredential to decrypt to.
func WithInitiatorPassword(password string) Option {
	return func(s *Stub) { s.initiatorPassword = password }
}

// WithTokenTTL sets the lifetime the token endpoint advertises. Zero produces
// a token that is already expired, for exercising the refresh path.
func WithTokenTTL(ttl time.Duration) Option {
	return func(s *Stub) { s.tokenTTL = ttl }
}

// Stub is an in-process Daraja. Point a mpesa.Config at URL and drive it.
type Stub struct {
	t      testing.TB
	server *httptest.Server
	mux    *http.ServeMux

	mu sync.Mutex

	consumerKey       string
	consumerSecret    string
	initiatorPassword string

	privateKey  *rsa.PrivateKey
	certificate []byte

	tokenTTL     time.Duration
	currentToken string
	issued       int

	passkey         string
	checkouts       map[string]*Checkout
	registrations   map[uint]registration
	identities      map[string]string
	pullRegistered  map[uint]bool
	pullPageSize    int
	pullStuckOffset bool

	ledger       *ledger
	transactions []Transaction

	pending []queuedCallback

	faults           map[Route]*fault
	duplicate        bool
	credentialLocked bool

	deliverer *http.Client
}

// New starts a stub and registers its shutdown with t.
func New(t testing.TB, opts ...Option) *Stub {
	t.Helper()

	certificate, key := sharedKeyPair()
	s := &Stub{
		t:                 t,
		mux:               http.NewServeMux(),
		initiatorPassword: "stub-initiator-password",
		passkey:           "stub-passkey",
		checkouts:         make(map[string]*Checkout),
		registrations:     make(map[uint]registration),
		identities:        make(map[string]string),
		pullRegistered:    make(map[uint]bool),
		pullPageSize:      2,
		privateKey:        key,
		certificate:       certificate,
		tokenTTL:          time.Hour,
		ledger:            newLedger(),
		faults:            make(map[Route]*fault),
		deliverer:         &http.Client{Timeout: 5 * time.Second},
	}
	for _, opt := range opts {
		opt(s)
	}

	s.routeAuth()
	s.routeExpress()
	s.routeC2B()
	s.routeShared()
	s.routePull()

	s.server = httptest.NewServer(s.mux)
	t.Cleanup(s.server.Close)
	return s
}

// URL is the stub's base URL.
func (s *Stub) URL() string { return s.server.URL }

// Close stops the stub. Registered with t automatically; calling it early is
// safe.
func (s *Stub) Close() { s.server.Close() }

// Certificate is the PEM certificate whose private half the stub holds. Pass it
// as mpesa.Config.Certificate so a SecurityCredential can be decrypted and
// checked rather than merely inspected.
func (s *Stub) Certificate() []byte { return s.certificate }

// handle registers an unauthenticated route.
func (s *Stub) handle(path string, fn func(http.ResponseWriter, *http.Request)) {
	s.mux.HandleFunc(path, fn)
}

// HandleAuthed registers an extra route behind the bearer check. Endpoint
// files use it, and tests use it to probe transport behaviour without
// depending on a particular Daraja endpoint existing yet.
func (s *Stub) HandleAuthed(route Route, path string, fn http.HandlerFunc) {
	s.handleAuthed(route, path, fn)
}

// handleAuthed registers a route behind the bearer check, which is where a
// superseded token is rejected.
func (s *Stub) handleAuthed(route Route, path string, fn func(http.ResponseWriter, *http.Request)) {
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

// writeJSON renders a response body.
func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

// apiError is Daraja's synchronous error body.
type apiError struct {
	RequestID    string `json:"requestId"`
	ErrorCode    string `json:"errorCode"`
	ErrorMessage string `json:"errorMessage"`
}

func writeAPIError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, apiError{RequestID: "stub-request", ErrorCode: code, ErrorMessage: message})
}
