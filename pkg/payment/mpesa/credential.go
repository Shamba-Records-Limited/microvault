package mpesa

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"embed"
	"encoding/base64"
	"encoding/pem"

	pkgErrors "github.com/Shamba-Records-Limited/microvault/pkg/errors"
)

//go:embed certs
var certFS embed.FS

// embeddedCertificate returns Safaricom's published certificate for the
// environment.
func embeddedCertificate(env Environment) ([]byte, error) {
	name := "certs/sandbox.cer"
	if env == EnvironmentProduction {
		name = "certs/production.cer"
	}
	raw, err := certFS.ReadFile(name)
	if err != nil {
		return nil, mpesaErr("certificate").
			Code(pkgErrors.CodeMissingDependency).
			With("certificate", name).
			Wrapf(err, "could not read the embedded certificate")
	}
	return raw, nil
}

// SecurityCredential encrypts the initiator password with Safaricom's public
// key, as every Initiator-bearing endpoint requires.
//
// The padding is PKCS #1 v1.5, which the standard library deprecates as
// dangerous. It is not a choice: Safaricom defines the SecurityCredential as a
// v1.5 ciphertext, and a credential produced with OAEP is one Daraja cannot
// decrypt — it comes back as result code 2001, indistinguishable from a wrong
// password. Nothing changes here until Safaricom changes the protocol.
//
// The exposure is narrower than the deprecation implies. The attack it warns
// about is a Bleichenbacher padding oracle, which is a risk to the party
// performing the decryption; we only ever encrypt, with a public key, and the
// plaintext is a credential the holder already knows.
//
// The certificate is a carrier for a public key and not a trust anchor:
// Safaricom's published certificates expired years ago and have never been
// rotated, so nothing here checks a chain or an expiry date.
func (c *Client) SecurityCredential() (string, error) {
	errb := mpesaErr("security_credential")

	if c.initiatorPassword == "" {
		return "", errb.
			Code(pkgErrors.CodeMissingDependency).
			With(pkgErrors.AttrDependency, "initiator password").
			Errorf("initiator password is not configured")
	}

	publicKey, err := parsePublicKey(c.certificate)
	if err != nil {
		return "", err
	}

	//nolint:staticcheck // SA1019: Daraja's SecurityCredential is defined as PKCS #1 v1.5; OAEP is rejected as 2001.
	encrypted, err := rsa.EncryptPKCS1v15(rand.Reader, publicKey, []byte(c.initiatorPassword))
	if err != nil {
		return "", errb.
			Code(pkgErrors.CodeEncodeFailed).
			Wrapf(err, "could not encrypt the initiator password")
	}
	return base64.StdEncoding.EncodeToString(encrypted), nil
}

// parsePublicKey reads an RSA public key from a PEM or DER certificate.
func parsePublicKey(raw []byte) (*rsa.PublicKey, error) {
	errb := mpesaErr("security_credential")

	der := raw
	if block, _ := pem.Decode(raw); block != nil {
		der = block.Bytes
	}

	certificate, err := x509.ParseCertificate(der)
	if err != nil {
		return nil, errb.
			Code(pkgErrors.CodeDecodeFailed).
			Wrapf(err, "could not parse the certificate")
	}

	publicKey, ok := certificate.PublicKey.(*rsa.PublicKey)
	if !ok {
		return nil, errb.
			Code(pkgErrors.CodeDecodeFailed).
			With("public_key_algorithm", certificate.PublicKeyAlgorithm.String()).
			Errorf("certificate does not carry an RSA public key")
	}
	return publicKey, nil
}
