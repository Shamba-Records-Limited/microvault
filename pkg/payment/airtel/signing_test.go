package airtel

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"strings"
	"testing"

	"github.com/Shamba-Records-Limited/microvault/pkg/payment/airtel/airtelstub"
)

func testKeyPair(t *testing.T) (key *rsa.PrivateKey, publicPEM string) {
	t.Helper()

	key, err := rsa.GenerateKey(rand.Reader, 1024)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	der, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	if err != nil {
		t.Fatalf("marshal key: %v", err)
	}
	return key, string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der}))
}

// The gateway decrypts x-key, re-encrypts the body it received, and compares.
// Anything less than a full round trip only assumes the signing works.
func TestSignPayload_RoundTrip(t *testing.T) {
	key, _ := testKeyPair(t)
	payload := []byte(`{"reference":"MV-1","transaction":{"amount":500,"id":"mv-1"}}`)

	signature, err := SignPayload(payload, &key.PublicKey)
	if err != nil {
		t.Fatalf("SignPayload: %v", err)
	}

	sealed, err := base64.StdEncoding.DecodeString(signature.Key)
	if err != nil {
		t.Fatalf("x-key is not base64: %v", err)
	}
	//nolint:staticcheck // SA1019: unseals what the client sealed with PKCS #1 v1.5.
	pair, err := rsa.DecryptPKCS1v15(nil, key, sealed)
	if err != nil {
		t.Fatalf("x-key does not decrypt: %v", err)
	}

	parts := strings.SplitN(string(pair), ":", 2)
	if len(parts) != 2 {
		t.Fatalf("x-key = %q, want a key:iv pair", pair)
	}
	aesKey, err := base64.StdEncoding.DecodeString(parts[0])
	if err != nil {
		t.Fatalf("aes key is not base64: %v", err)
	}
	iv, err := base64.StdEncoding.DecodeString(parts[1])
	if err != nil {
		t.Fatalf("iv is not base64: %v", err)
	}
	if len(aesKey) != aesKeyBytes {
		t.Fatalf("aes key is %d bytes, want %d", len(aesKey), aesKeyBytes)
	}
	if len(iv) != aesIVBytes {
		t.Fatalf("iv is %d bytes, want %d", len(iv), aesIVBytes)
	}

	ciphertext, err := base64.StdEncoding.DecodeString(signature.Signature)
	if err != nil {
		t.Fatalf("x-signature is not base64: %v", err)
	}
	plaintext, err := aesCBCDecrypt(ciphertext, aesKey, iv)
	if err != nil {
		t.Fatalf("aesCBCDecrypt: %v", err)
	}
	if string(plaintext) != string(payload) {
		t.Fatalf("x-signature decrypts to %q, want the exact payload", plaintext)
	}
}

// Two signings of one payload must differ: the key and IV are fresh each
// time. A deterministic signature would mean a reused IV.
func TestSignPayload_UsesFreshKeyMaterial(t *testing.T) {
	key, _ := testKeyPair(t)
	payload := []byte(`{"a":1}`)

	first, err := SignPayload(payload, &key.PublicKey)
	if err != nil {
		t.Fatalf("SignPayload: %v", err)
	}
	second, err := SignPayload(payload, &key.PublicKey)
	if err != nil {
		t.Fatalf("SignPayload: %v", err)
	}
	if first.Signature == second.Signature {
		t.Fatal("two signings produced the same ciphertext; the IV is being reused")
	}
}

func TestSignPayload_RequiresAKey(t *testing.T) {
	if _, err := SignPayload([]byte(`{}`), nil); err == nil {
		t.Fatal("signing without a key must fail")
	}
}

func TestParseRSAPublicKey(t *testing.T) {
	key, pemEncoded := testKeyPair(t)
	der, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	cases := map[string]string{
		"pem":          pemEncoded,
		"base64 pkix":  base64.StdEncoding.EncodeToString(der),
		"base64 pkcs1": base64.StdEncoding.EncodeToString(x509.MarshalPKCS1PublicKey(&key.PublicKey)),
		"wrapped pem":  "\n" + pemEncoded + "\n",
	}

	for name, material := range cases {
		t.Run(name, func(t *testing.T) {
			parsed, err := ParseRSAPublicKey(material)
			if err != nil {
				t.Fatalf("ParseRSAPublicKey: %v", err)
			}
			if parsed.N.Cmp(key.N) != 0 {
				t.Fatal("parsed a different key")
			}
		})
	}

	for name, material := range map[string]string{
		"empty":      "",
		"not base64": "!!!!",
		"not a key":  base64.StdEncoding.EncodeToString([]byte("hello")),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := ParseRSAPublicKey(material); err == nil {
				t.Fatal("expected an error")
			}
		})
	}
}

// A client with signing off, talking to an application with signing on, is
// rejected. This is the failure that looks like an auth problem and is not.
func TestPayment_UnsignedAgainstSigningApplication(t *testing.T) {
	client, _ := newStubbed(t, airtelstub.WithRequiredSigning())

	_, err := client.Payment(context.Background(), validPayment())
	if err == nil {
		t.Fatal("an unsigned payment must be refused when signing is required")
	}
	if got := OutcomeFor(CodeCollectionForbidden); got.Kind != OutcomeCredential {
		t.Fatalf("a signature mismatch classifies as %q, want %q", got.Kind, OutcomeCredential)
	}
}

// Signing on, end to end: the stub unseals the key, re-encrypts the body it
// received and compares.
func TestPayment_SignedIsAccepted(t *testing.T) {
	stub := airtelstub.New(t, airtelstub.WithRequiredSigning())
	client, err := New(Config{
		Environment:    EnvironmentStaging,
		ClientID:       "client-id",
		ClientSecret:   "client-secret",
		BaseURL:        stub.URL(),
		SigningEnabled: true,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	if _, err := client.Payment(context.Background(), validPayment()); err != nil {
		t.Fatalf("a signed payment was refused: %v", err)
	}

	txn, ok := stub.Transaction("mv-txn-1")
	if !ok {
		t.Fatal("the stub holds no transaction")
	}
	if !txn.Signed {
		t.Fatal("the stub did not record the request as signed")
	}
}

// The signed bytes must be the bytes sent. A client that signs a re-marshal
// of the same struct can produce different bytes and fail only in production.
func TestPayment_SignatureCoversTheBytesSent(t *testing.T) {
	stub := airtelstub.New(t, airtelstub.WithRequiredSigning())
	client, err := New(Config{
		Environment:    EnvironmentStaging,
		ClientID:       "client-id",
		ClientSecret:   "client-secret",
		BaseURL:        stub.URL(),
		SigningEnabled: true,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	req := validPayment()
	req.Reference = `MV "quoted" & odd`
	req.TransactionID = "mv-txn-odd"

	if _, err := client.Payment(context.Background(), req); err != nil {
		t.Fatalf("a signed payment with escaping was refused: %v", err)
	}
}

func TestEncryptPIN(t *testing.T) {
	key, _ := testKeyPair(t)

	sealed, err := EncryptPIN("1234", &key.PublicKey)
	if err != nil {
		t.Fatalf("EncryptPIN: %v", err)
	}
	raw, err := base64.StdEncoding.DecodeString(sealed)
	if err != nil {
		t.Fatalf("sealed PIN is not base64: %v", err)
	}
	//nolint:staticcheck // SA1019: unseals what EncryptPIN sealed with PKCS #1 v1.5.
	plaintext, err := rsa.DecryptPKCS1v15(nil, key, raw)
	if err != nil {
		t.Fatalf("sealed PIN does not decrypt: %v", err)
	}
	if string(plaintext) != "1234" {
		t.Fatalf("sealed PIN decrypts to %q", plaintext)
	}

	if _, err := EncryptPIN("", &key.PublicKey); err == nil {
		t.Fatal("an empty PIN must be refused")
	}
	if _, err := EncryptPIN("1234", nil); err == nil {
		t.Fatal("encrypting without a key must be refused")
	}
}

// pkcs7Unpad indexes the last byte, so an empty input has to be refused
// rather than panicking. Nothing reaches it with one today; the guard is what
// keeps that true when something else calls it.
func TestPKCS7Unpad_Rejects(t *testing.T) {
	cases := map[string][]byte{
		"empty":            {},
		"zero padding":     append(make([]byte, 15), 0),
		"padding too long": append(make([]byte, 15), 17),
		"inconsistent":     append(append(make([]byte, 13), 1), 3, 3),
	}

	for name, data := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := pkcs7Unpad(data, 16); err == nil {
				t.Fatal("expected the padding to be refused")
			}
		})
	}
}

func TestPKCS7_RoundTrip(t *testing.T) {
	for _, size := range []int{0, 1, 15, 16, 17, 64} {
		data := make([]byte, size)
		for i := range data {
			data[i] = byte(i)
		}

		padded := pkcs7Pad(data, 16)
		if len(padded)%16 != 0 {
			t.Fatalf("padded length %d is not a block multiple", len(padded))
		}

		unpadded, err := pkcs7Unpad(padded, 16)
		if err != nil {
			t.Fatalf("pkcs7Unpad(%d bytes): %v", size, err)
		}
		if len(unpadded) != size {
			t.Fatalf("round trip of %d bytes produced %d", size, len(unpadded))
		}
	}
}
