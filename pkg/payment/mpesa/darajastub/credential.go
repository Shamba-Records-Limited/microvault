package darajastub

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/pem"
	"fmt"
	"math/big"
	"sync"
	"testing"
	"time"
)

// sharedKeyPair is generated once per test binary. RSA keygen is the slowest
// thing the stub does, and every stub wanting its own key would dominate the
// suite's runtime for no benefit.
var sharedKeyPair = sync.OnceValues(func() ([]byte, *rsa.PrivateKey) {
	return newKeyPair(nil)
})

// newKeyPair builds a self-signed certificate and the private key that decrypts
// what it encrypts. Generated in memory and never written to disk, so there is
// no key material in the repository and nothing to remember to ignore.
//
// t may be nil, in which case a failure panics — it is called from a package
// initialiser where there is no test to fail.
func newKeyPair(t testing.TB) ([]byte, *rsa.PrivateKey) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		fatal(t, "darajastub: generate key: %v", err)
	}
	template := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "darajastub.invalid"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(24 * time.Hour),
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		fatal(t, "darajastub: create certificate: %v", err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), key
}

func fatal(t testing.TB, format string, args ...any) {
	if t == nil {
		panic(fmt.Sprintf(format, args...))
	}
	t.Fatalf(format, args...)
}

// VerifySecurityCredential decrypts a presented credential and compares it to
// the expected initiator password, returning the Daraja result code Safaricom
// would return. Exported so callers outside this package can assert the RSA
// path rather than assert that a string is base64.
func (s *Stub) VerifySecurityCredential(credential string) (resultCode int64, ok bool) {
	return s.checkSecurityCredential(credential)
}

// checkSecurityCredential decrypts a presented credential and compares it to
// the expected initiator password, returning the Daraja result code.
//
// This is the only way to prove the client's RSA path end to end without
// production credentials. Asserting that a credential is non-empty base64 would
// pass for a credential encrypted with the wrong certificate, which is exactly
// the mistake that produces an undiagnosable 2001 at go-live.
func (s *Stub) checkSecurityCredential(credential string) (resultCode int64, ok bool) {
	if s.isCredentialLocked() {
		return 8006, false
	}

	encrypted, err := base64.StdEncoding.DecodeString(credential)
	if err != nil {
		return 2001, false
	}
	//nolint:staticcheck // SA1019: mirrors Safaricom's PKCS #1 v1.5 SecurityCredential. This is the decrypting side the
	// deprecation warns about, which is safe only because it is a test stub: it is never reachable by an attacker and
	// never handles a real key. Do not lift this function into production code.
	plaintext, err := rsa.DecryptPKCS1v15(rand.Reader, s.privateKey, encrypted)
	if err != nil {
		return 2001, false
	}

	s.mu.Lock()
	expected := s.initiatorPassword
	s.mu.Unlock()

	if string(plaintext) != expected {
		return 2001, false
	}
	return 0, true
}
