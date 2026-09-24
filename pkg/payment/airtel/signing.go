package airtel

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"net/http"

	pkgErrors "github.com/Shamba-Records-Limited/microvault/pkg/errors"
)

// Message-signing header names, introduced with Collection v2.0.
const (
	HeaderSignature = "x-signature"
	HeaderKey       = "x-key"
)

// AES-256 with a 128-bit IV, as the portal specifies for AES/CBC/PKCS5Padding.
const (
	aesKeyBytes = 32
	aesIVBytes  = 16
)

// Signature is the pair of headers a signed request carries.
type Signature struct {
	// Signature is the AES-encrypted payload, base64-encoded. Sent as
	// x-signature.
	Signature string

	// Key is the RSA-encrypted "key:iv" pair, base64-encoded. Sent as x-key.
	Key string
}

// SignPayload seals payload for Airtel's message-signing scheme.
//
// The gateway decrypts x-key to recover the AES key and IV, re-encrypts the
// request body it received, and compares the result to x-signature. A mismatch
// is DP00800001026 Forbidden, so the bytes signed here must be byte-identical
// to the bytes sent — which is why the caller hands over the marshalled body
// rather than the struct it came from.
func SignPayload(payload []byte, pub *rsa.PublicKey) (Signature, error) {
	errb := airtelErr("sign_payload")

	if pub == nil {
		return Signature{}, errb.
			Code(pkgErrors.CodeMissingDependency).
			With(pkgErrors.AttrDependency, "rsa public key").
			Errorf("no public key was supplied")
	}

	key := make([]byte, aesKeyBytes)
	if _, err := rand.Read(key); err != nil {
		return Signature{}, errb.Code(pkgErrors.CodeBuildFailed).Wrapf(err, "could not generate an AES key")
	}
	iv := make([]byte, aesIVBytes)
	if _, err := rand.Read(iv); err != nil {
		return Signature{}, errb.Code(pkgErrors.CodeBuildFailed).Wrapf(err, "could not generate an IV")
	}

	ciphertext, err := aesCBCEncrypt(payload, key, iv)
	if err != nil {
		return Signature{}, err
	}

	keyIV := base64.StdEncoding.EncodeToString(key) + ":" + base64.StdEncoding.EncodeToString(iv)

	// PKCS#1 v1.5 caps the plaintext at the modulus size less eleven bytes. A
	// 1024-bit key leaves 117, and the pair is 69, so this only trips on a key
	// smaller than Airtel documents — worth catching here rather than as an
	// opaque gateway rejection.
	if len(keyIV) > pub.Size()-11 {
		return Signature{}, errb.
			Code(pkgErrors.CodeBuildFailed).
			With("rsa_key_bits", pub.Size()*8).
			Errorf("the RSA key is too small to seal the AES key and IV")
	}

	//nolint:staticcheck // SA1019: Airtel specifies PKCS1Padding for the key:iv seal; OAEP is rejected as DP00800001026.
	sealed, err := rsa.EncryptPKCS1v15(rand.Reader, pub, []byte(keyIV))
	if err != nil {
		return Signature{}, errb.Code(pkgErrors.CodeBuildFailed).Wrapf(err, "could not seal the AES key and IV")
	}

	return Signature{
		Signature: base64.StdEncoding.EncodeToString(ciphertext),
		Key:       base64.StdEncoding.EncodeToString(sealed),
	}, nil
}

// signRequest fetches the current public key and applies both headers.
func (c *Client) signRequest(ctx context.Context, req *http.Request, payload []byte) error {
	key, err := c.EncryptionKeys(ctx)
	if err != nil {
		return err
	}
	pub, err := ParseRSAPublicKey(key.PublicKey)
	if err != nil {
		return err
	}
	signature, err := SignPayload(payload, pub)
	if err != nil {
		return err
	}
	req.Header.Set(HeaderSignature, signature.Signature)
	req.Header.Set(HeaderKey, signature.Key)
	return nil
}

// aesCBCEncrypt applies AES/CBC/PKCS5Padding. PKCS#5 and PKCS#7 padding are
// identical at a 16-byte block size; the portal names the former.
func aesCBCEncrypt(plaintext, key, iv []byte) ([]byte, error) {
	errb := airtelErr("aes_encrypt")

	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, errb.Code(pkgErrors.CodeBuildFailed).Wrapf(err, "could not build the AES cipher")
	}
	if len(iv) != block.BlockSize() {
		return nil, errb.
			Code(pkgErrors.CodeBuildFailed).
			With("iv_bytes", len(iv)).
			Errorf("the IV is not one AES block")
	}

	padded := pkcs7Pad(plaintext, block.BlockSize())
	ciphertext := make([]byte, len(padded))
	cipher.NewCBCEncrypter(block, iv).CryptBlocks(ciphertext, padded)
	return ciphertext, nil
}

// aesCBCDecrypt reverses aesCBCEncrypt. Airtel never sends us a signed
// payload, so this exists for the stub and the round-trip tests: a signing
// implementation that is never decrypted is only assumed to work.
func aesCBCDecrypt(ciphertext, key, iv []byte) ([]byte, error) {
	errb := airtelErr("aes_decrypt")

	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, errb.Code(pkgErrors.CodeBuildFailed).Wrapf(err, "could not build the AES cipher")
	}
	if len(iv) != block.BlockSize() {
		return nil, errb.Code(pkgErrors.CodeBuildFailed).Errorf("the IV is not one AES block")
	}
	if len(ciphertext) == 0 || len(ciphertext)%block.BlockSize() != 0 {
		return nil, errb.
			Code(pkgErrors.CodeDecodeFailed).
			With("ciphertext_bytes", len(ciphertext)).
			Errorf("the ciphertext is not a whole number of AES blocks")
	}

	plaintext := make([]byte, len(ciphertext))
	cipher.NewCBCDecrypter(block, iv).CryptBlocks(plaintext, ciphertext)
	return pkcs7Unpad(plaintext, block.BlockSize())
}

func pkcs7Pad(data []byte, blockSize int) []byte {
	padding := blockSize - len(data)%blockSize
	return append(data, bytes.Repeat([]byte{byte(padding)}, padding)...)
}

func pkcs7Unpad(data []byte, blockSize int) ([]byte, error) {
	errb := airtelErr("pkcs7_unpad")

	if len(data) == 0 {
		return nil, errb.Code(pkgErrors.CodeDecodeFailed).Errorf("there is nothing to unpad")
	}

	padding := int(data[len(data)-1])
	if padding == 0 || padding > blockSize || padding > len(data) {
		return nil, errb.Code(pkgErrors.CodeDecodeFailed).Errorf("the padding byte is out of range")
	}
	for _, b := range data[len(data)-padding:] {
		if int(b) != padding {
			return nil, errb.Code(pkgErrors.CodeDecodeFailed).Errorf("the padding is inconsistent")
		}
	}
	return data[:len(data)-padding], nil
}
