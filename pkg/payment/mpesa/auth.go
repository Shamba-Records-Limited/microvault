package mpesa

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	pkgErrors "github.com/Shamba-Records-Limited/microvault/pkg/errors"
)

const pathAuth = "/oauth/v1/generate?grant_type=client_credentials"

// tokenSkew is subtracted from a token's stated lifetime. Daraja tokens last
// an hour, and a minute of headroom costs one extra mint a day while removing
// the race where a token expires between the check and the call.
const tokenSkew = time.Minute

// TokenStore caches access tokens. Implementations must be safe for concurrent
// use.
//
// Daraja invalidates the previous access token whenever a new one is minted, so
// two replicas each holding a private cache will evict each other and spend
// most of their calls recovering. A shared store makes the mint happen once for
// the whole cluster.
type TokenStore interface {
	Get(ctx context.Context, key string) (token string, expiresAt time.Time, ok bool)
	Set(ctx context.Context, key, token string, expiresAt time.Time) error
	Delete(ctx context.Context, key string) error
}

// MemoryTokenStore is the default in-process TokenStore. Correct for a single
// replica and for tests; see TokenStore for why a deployment with more than one
// replica should supply a shared implementation.
type MemoryTokenStore struct {
	mu      sync.RWMutex
	entries map[string]memoryToken
}

type memoryToken struct {
	token     string
	expiresAt time.Time
}

// NewMemoryTokenStore builds an empty in-process store.
func NewMemoryTokenStore() *MemoryTokenStore {
	return &MemoryTokenStore{entries: make(map[string]memoryToken)}
}

// Get returns the cached token for key.
func (m *MemoryTokenStore) Get(_ context.Context, key string) (string, time.Time, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	entry, ok := m.entries[key]
	return entry.token, entry.expiresAt, ok
}

// Set caches token under key.
func (m *MemoryTokenStore) Set(_ context.Context, key, token string, expiresAt time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.entries[key] = memoryToken{token: token, expiresAt: expiresAt}
	return nil
}

// Delete evicts key.
func (m *MemoryTokenStore) Delete(_ context.Context, key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.entries, key)
	return nil
}

// defaultTokenSeconds is Daraja's documented token lifetime, used when the
// response does not state a usable one.
const defaultTokenSeconds = 3599

// authResponse is Daraja's token body.
//
// expires_in is held raw rather than as a json.Number because Daraja sends it
// as a quoted string and json.Number rejects any quoted value that is not a
// number. Decoding it strictly would turn a malformed expiry — a hint we have a
// documented default for — into a failure to obtain a token at all.
type authResponse struct {
	AccessToken string          `json:"access_token"`
	ExpiresIn   json.RawMessage `json:"expires_in"`
}

// seconds reports the stated lifetime, falling back to Daraja's documented
// default when the value is absent, quoted nonsense, or non-positive.
func (a authResponse) seconds() int64 {
	raw := strings.Trim(strings.TrimSpace(string(a.ExpiresIn)), `"`)
	if raw == "" || raw == "null" {
		return defaultTokenSeconds
	}
	parsed, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || parsed <= 0 {
		return defaultTokenSeconds
	}
	return parsed
}

// singleFlight collapses concurrent mints into one. Scoped to this package
// because the need is one key and one operation; a general implementation would
// be a dependency the package does not otherwise need.
type singleFlight struct {
	mu     sync.Mutex
	active *flightCall
}

type flightCall struct {
	done  chan struct{}
	token string
	err   error
}

func newSingleFlight() *singleFlight { return &singleFlight{} }

// Do runs fn unless a call is already in flight, in which case it waits for
// that call and returns its result.
func (s *singleFlight) Do(ctx context.Context, fn func() (string, error)) (string, error) {
	s.mu.Lock()
	if inflight := s.active; inflight != nil {
		s.mu.Unlock()
		select {
		case <-inflight.done:
			return inflight.token, inflight.err
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}
	call := &flightCall{done: make(chan struct{})}
	s.active = call
	s.mu.Unlock()

	call.token, call.err = fn()

	s.mu.Lock()
	s.active = nil
	s.mu.Unlock()
	close(call.done)

	return call.token, call.err
}

// tokenKey namespaces the cache by environment and credential. The consumer key
// is hashed so a shared store's keyspace does not carry it.
func (c *Client) tokenKey() string {
	sum := sha256.Sum256([]byte(c.consumerKey))
	return "mpesa:token:" + string(c.env) + ":" + hex.EncodeToString(sum[:])[:16]
}

// AccessToken returns a cached token, minting one if none is live.
func (c *Client) AccessToken(ctx context.Context) (string, error) {
	if token, ok := c.cachedToken(ctx); ok {
		return token, nil
	}

	return c.mint.Do(ctx, func() (string, error) {
		if token, ok := c.cachedToken(ctx); ok {
			return token, nil
		}
		return c.mintToken(ctx)
	})
}

func (c *Client) cachedToken(ctx context.Context) (string, bool) {
	token, expiresAt, ok := c.tokens.Get(ctx, c.tokenKey())
	if !ok || token == "" {
		return "", false
	}
	if !c.now().Before(expiresAt) {
		return "", false
	}
	return token, true
}

func (c *Client) mintToken(ctx context.Context) (string, error) {
	errb := mpesaErr("access_token")

	basic := base64.StdEncoding.EncodeToString([]byte(c.consumerKey + ":" + c.consumerSecret))
	body, err := send[authResponse](ctx, c, errb, http.MethodGet, pathAuth, nil, func(req *http.Request) {
		req.Header.Set("Authorization", "Basic "+basic)
	})
	if err != nil {
		return "", err
	}
	if body.AccessToken == "" {
		return "", errb.
			Code(pkgErrors.CodeIncompleteResponse).
			Errorf("Daraja returned no access token")
	}

	expiresAt := c.now().Add(time.Duration(body.seconds())*time.Second - tokenSkew)
	if err := c.tokens.Set(ctx, c.tokenKey(), body.AccessToken, expiresAt); err != nil {
		return "", errb.
			Code(pkgErrors.CodeStateWriteFailed).
			Wrapf(err, "could not cache the access token")
	}
	return body.AccessToken, nil
}

// evictToken drops the cached token so the next call mints a fresh one.
func (c *Client) evictToken(ctx context.Context) {
	_ = c.tokens.Delete(ctx, c.tokenKey())
}
