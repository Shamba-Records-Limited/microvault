package stellaranchor

import (
	"context"
	"sync"
	"time"
)

// JWTCache is an in-memory store of SEP-10 JWTs keyed by child-memo. Each
// entry is held until its parsed `exp` minus SafetyMargin. Safe for
// concurrent use.
type JWTCache struct {
	auth         *AuthClient
	safetyMargin time.Duration

	mu      sync.Mutex
	entries map[int64]jwtEntry
}

type jwtEntry struct {
	token   string
	expires time.Time
}

// NewJWTCache wraps an AuthClient with caching. SafetyMargin is how long
// before nominal expiry the cache treats a token as stale and refreshes;
// defaults to 60s when zero.
func NewJWTCache(auth *AuthClient, safetyMargin time.Duration) *JWTCache {
	if safetyMargin <= 0 {
		safetyMargin = 60 * time.Second
	}
	return &JWTCache{
		auth:         auth,
		safetyMargin: safetyMargin,
		entries:      make(map[int64]jwtEntry),
	}
}

// Get returns a valid JWT for the given child-memo, calling AuthClient
// only when the cache is empty or stale.
func (c *JWTCache) Get(ctx context.Context, childMemo int64) (string, error) {
	c.mu.Lock()
	if e, ok := c.entries[childMemo]; ok && time.Now().Add(c.safetyMargin).Before(e.expires) {
		c.mu.Unlock()
		return e.token, nil
	}
	c.mu.Unlock()

	res, err := c.auth.Authenticate(ctx, childMemo)
	if err != nil {
		return "", err
	}

	c.mu.Lock()
	c.entries[childMemo] = jwtEntry{token: res.JWT, expires: res.ExpiresAt}
	c.mu.Unlock()
	return res.JWT, nil
}

// Invalidate evicts the cached token for childMemo. Use after a 401 from a
// SEP-24 endpoint — the next Get call will fetch a fresh token.
func (c *JWTCache) Invalidate(childMemo int64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.entries, childMemo)
}

// InvalidateAll clears the entire cache. Useful when the SEP-1 SIGNING_KEY
// has rotated and every cached token is now invalid.
func (c *JWTCache) InvalidateAll() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries = make(map[int64]jwtEntry)
}
