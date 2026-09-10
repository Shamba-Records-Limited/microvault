package mpesa

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/pem"
	"math/big"
	"testing"
	"time"
)

// testCertificate returns a self-signed PEM certificate and the private key
// that decrypts what it encrypts. Generated per run and never written to disk.
func testCertificate(t *testing.T) ([]byte, *rsa.PrivateKey) {
	t.Helper()

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	template := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "stub.safaricom.test"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("CreateCertificate: %v", err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), key
}

func TestSecurityCredential_RoundTrips(t *testing.T) {
	certificate, key := testCertificate(t)

	c, err := New(Config{
		Environment:       EnvironmentSandbox,
		ConsumerKey:       "k",
		ConsumerSecret:    "s",
		InitiatorName:     "apiop37",
		InitiatorPassword: "correct horse battery staple",
		Certificate:       certificate,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	credential, err := c.SecurityCredential()
	if err != nil {
		t.Fatalf("SecurityCredential: %v", err)
	}

	encrypted, err := base64.StdEncoding.DecodeString(credential)
	if err != nil {
		t.Fatalf("credential is not base64: %v", err)
	}
	//nolint:staticcheck // SA1019: Daraja defines the SecurityCredential as PKCS #1 v1.5; this decrypts what the client encrypts.
	plaintext, err := rsa.DecryptPKCS1v15(rand.Reader, key, encrypted)
	if err != nil {
		t.Fatalf("DecryptPKCS1v15: %v", err)
	}
	if string(plaintext) != "correct horse battery staple" {
		t.Errorf("decrypted to %q", plaintext)
	}
}

// PKCS#1 v1.5 padding is randomised, so two credentials for the same password
// differ. Anything that caches or compares them by value is wrong.
func TestSecurityCredential_NotDeterministic(t *testing.T) {
	certificate, _ := testCertificate(t)
	c, _ := New(Config{
		Environment: EnvironmentSandbox, ConsumerKey: "k", ConsumerSecret: "s",
		InitiatorPassword: "pw", Certificate: certificate,
	})

	first, _ := c.SecurityCredential()
	second, _ := c.SecurityCredential()
	if first == second {
		t.Error("two credentials for the same password were identical")
	}
}

func TestSecurityCredential_MissingPassword(t *testing.T) {
	certificate, _ := testCertificate(t)
	c, _ := New(Config{
		Environment: EnvironmentSandbox, ConsumerKey: "k", ConsumerSecret: "s",
		Certificate: certificate,
	})
	if _, err := c.SecurityCredential(); err == nil {
		t.Error("expected an error when the initiator password is unset")
	}
}

// The reference SDK asserts the public key type without checking, which panics
// on a non-RSA certificate.
func TestParsePublicKey_NonRSA(t *testing.T) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	template := &x509.Certificate{
		SerialNumber: big.NewInt(2),
		Subject:      pkix.Name{CommonName: "ecdsa.test"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("CreateCertificate: %v", err)
	}

	if _, err := parsePublicKey(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})); err == nil {
		t.Error("expected an error for a non-RSA certificate")
	}
}

func TestParsePublicKey_Garbage(t *testing.T) {
	if _, err := parsePublicKey([]byte("not a certificate")); err == nil {
		t.Error("expected an error for a malformed certificate")
	}
}

// Safaricom's published certificates expired in 2016 and 2018 and have never
// been rotated. They must still parse, because they are a public-key carrier
// and not a trust anchor.
func TestEmbeddedCertificates_ParseDespiteExpiry(t *testing.T) {
	for _, env := range []Environment{EnvironmentSandbox, EnvironmentProduction} {
		raw, err := embeddedCertificate(env)
		if err != nil {
			t.Fatalf("%s: embeddedCertificate: %v", env, err)
		}
		key, err := parsePublicKey(raw)
		if err != nil {
			t.Fatalf("%s: parsePublicKey: %v", env, err)
		}
		if key.N.BitLen() != 2048 {
			t.Errorf("%s: key is %d bits, want 2048", env, key.N.BitLen())
		}
	}
}

// The two environments must not share a certificate: encrypting with the wrong
// one produces a credential Safaricom rejects as 2001, which is industrially
// hard to diagnose.
func TestEmbeddedCertificates_DifferPerEnvironment(t *testing.T) {
	sandbox, _ := embeddedCertificate(EnvironmentSandbox)
	production, _ := embeddedCertificate(EnvironmentProduction)
	if string(sandbox) == string(production) {
		t.Error("sandbox and production certificates are identical")
	}
}
