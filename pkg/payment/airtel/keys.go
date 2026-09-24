package airtel

import (
	"context"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/pem"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	pkgErrors "github.com/Shamba-Records-Limited/microvault/pkg/errors"
)

const pathEncryptionKeys = "/v1/rsa/encryption-keys"

// EncryptionKey is the consumer's RSA public key as Airtel issues it.
type EncryptionKey struct {
	// KeyID identifies the key Airtel expects the payload to be sealed with.
	KeyID int64

	// PublicKey is the key material exactly as returned, PEM or bare base64.
	PublicKey string

	// ValidUpto is when the key stops being accepted. Keys expire, so a cache
	// without an expiry check eventually signs everything with a dead key and
	// reports DP00800001026 forever.
	ValidUpto time.Time
}

// Expired reports whether the key is past its stated validity.
func (k EncryptionKey) Expired(now time.Time) bool {
	return !k.ValidUpto.IsZero() && !now.Before(k.ValidUpto)
}

// KeyStore caches encryption keys. Implementations must be safe for
// concurrent use.
type KeyStore interface {
	Get(ctx context.Context, key string) (EncryptionKey, bool)
	Set(ctx context.Context, key string, value EncryptionKey) error
	Delete(ctx context.Context, key string) error
}

// MemoryKeyStore is the default in-process KeyStore.
type MemoryKeyStore struct {
	mu      sync.RWMutex
	entries map[string]EncryptionKey
}

// NewMemoryKeyStore builds an empty in-process store.
func NewMemoryKeyStore() *MemoryKeyStore {
	return &MemoryKeyStore{entries: make(map[string]EncryptionKey)}
}

// Get returns the cached key.
func (m *MemoryKeyStore) Get(_ context.Context, key string) (EncryptionKey, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	entry, ok := m.entries[key]
	return entry, ok
}

// Set caches value under key.
func (m *MemoryKeyStore) Set(_ context.Context, key string, value EncryptionKey) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.entries[key] = value
	return nil
}

// Delete evicts key.
func (m *MemoryKeyStore) Delete(_ context.Context, key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.entries, key)
	return nil
}

type encryptionKeysResponse struct {
	Status Status `json:"status"`
	Data   struct {
		KeyID     int64  `json:"key_id"`
		Key       string `json:"key"`
		ValidUpto string `json:"valid_upto"`
	} `json:"data"`
}

func (r *encryptionKeysResponse) envelope() Status { return r.Status }

// keyCacheKey namespaces the cache by environment and credential.
func (c *Client) keyCacheKey() string {
	sum := sha256.Sum256([]byte(c.clientID))
	return "airtel:rsa:" + string(c.env) + ":" + hex.EncodeToString(sum[:])[:16]
}

// EncryptionKeys fetches the consumer's RSA public key, using the cached copy
// while it is still valid.
//
// The endpoint is gated on product subscription — the account must carry at
// least one of Collection, Disbursement, Cash-In, Cash-Out or ATM Withdrawal —
// so a rejection here is a commercial gap, not a transient failure.
func (c *Client) EncryptionKeys(ctx context.Context) (EncryptionKey, error) {
	cacheKey := c.keyCacheKey()
	if cached, ok := c.keys.Get(ctx, cacheKey); ok && !cached.Expired(c.now()) {
		return cached, nil
	}

	errb := airtelErr("encryption_keys")
	ep := endpoint{method: http.MethodGet, path: pathEncryptionKeys, headers: headerUpper}

	resp, err := call[encryptionKeysResponse](ctx, c, errb, ep, ep.path, nil)
	if err != nil {
		return EncryptionKey{}, err
	}
	if resp.Data.Key == "" {
		return EncryptionKey{}, errb.
			Code(pkgErrors.CodeIncompleteResponse).
			Errorf("Airtel returned no encryption key")
	}

	key := EncryptionKey{
		KeyID:     resp.Data.KeyID,
		PublicKey: resp.Data.Key,
		ValidUpto: parseValidUpto(resp.Data.ValidUpto),
	}
	if err := c.keys.Set(ctx, cacheKey, key); err != nil {
		return EncryptionKey{}, errb.
			Code(pkgErrors.CodeStateWriteFailed).
			Wrapf(err, "could not cache the encryption key")
	}
	return key, nil
}

// validUptoLayouts are the renderings a valid_upto has been seen in. The
// portal documents the field as an expiry without stating its format, so this
// accepts the plausible set and yields the zero time for anything else.
//
// A zero ValidUpto means "no stated expiry", which Expired treats as never
// expiring. That is the safe direction: the alternative is treating an
// unparseable expiry as already expired and re-fetching the key on every
// single signed call.
var validUptoLayouts = []string{
	time.RFC3339,
	"2006-01-02T15:04:05",
	"2006-01-02 15:04:05",
	"2006-01-02",
	"02-01-2006 15:04:05",
	"02/01/2006 15:04:05",
}

func parseValidUpto(value string) time.Time {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return time.Time{}
	}

	if epoch, err := strconv.ParseInt(trimmed, 10, 64); err == nil && epoch > 0 {
		if epoch > 1e12 {
			return time.UnixMilli(epoch).UTC()
		}
		return time.Unix(epoch, 0).UTC()
	}

	for _, layout := range validUptoLayouts {
		if parsed, err := time.Parse(layout, trimmed); err == nil {
			return parsed.UTC()
		}
	}
	return time.Time{}
}

// ParseRSAPublicKey decodes the key material Airtel returns.
//
// The portal does not state an encoding, and the three it plausibly uses — PEM,
// base64 PKIX, base64 PKCS#1 — are all cheap to try. Guessing wrong produces a
// signature the gateway rejects with DP00800001026, which is indistinguishable
// at the call site from a genuine signing bug, so the guessing happens here
// where it can fail loudly instead.
func ParseRSAPublicKey(material string) (*rsa.PublicKey, error) {
	errb := airtelErr("parse_rsa_public_key")

	trimmed := strings.TrimSpace(material)
	if trimmed == "" {
		return nil, errb.Code(pkgErrors.CodeMissingDependency).Errorf("no key material was supplied")
	}

	var der []byte
	if strings.Contains(trimmed, "-----BEGIN") {
		block, _ := pem.Decode([]byte(trimmed))
		if block == nil {
			return nil, errb.Code(pkgErrors.CodeDecodeFailed).Errorf("key material is not valid PEM")
		}
		der = block.Bytes
	} else {
		decoded, err := base64.StdEncoding.DecodeString(stripWhitespace(trimmed))
		if err != nil {
			return nil, errb.Code(pkgErrors.CodeDecodeFailed).Wrapf(err, "key material is not valid base64")
		}
		der = decoded
	}

	if parsed, err := x509.ParsePKIXPublicKey(der); err == nil {
		pub, ok := parsed.(*rsa.PublicKey)
		if !ok {
			return nil, errb.Code(pkgErrors.CodeDecodeFailed).Errorf("key material is not an RSA public key")
		}
		return pub, nil
	}
	if pub, err := x509.ParsePKCS1PublicKey(der); err == nil {
		return pub, nil
	}
	return nil, errb.Code(pkgErrors.CodeDecodeFailed).Errorf("key material is neither PKIX nor PKCS#1")
}

func stripWhitespace(value string) string {
	return strings.Map(func(r rune) rune {
		switch r {
		case ' ', '\t', '\n', '\r':
			return -1
		}
		return r
	}, value)
}
