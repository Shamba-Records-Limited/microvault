package airtel

import (
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"

	pkgErrors "github.com/Shamba-Records-Limited/microvault/pkg/errors"
)

// EncryptPIN seals a wallet PIN for any endpoint that carries one.
//
// The portal specifies RSA, mode ECB, padding PKCS1Padding, key length 1024.
// "ECB" is a category error applied to RSA — there is no block chaining in a
// single-block RSA operation — and what it names is plain PKCS#1 v1.5
// encryption, which is what this does. Airtel holds the private half; the
// public key is published in the portal's own code snippet and is supplied by
// the caller rather than embedded, because it is per-op-co and rotates.
//
// Collection does not carry a PIN. This is here for the endpoints that do —
// Disbursement, Float Sale, Cash-In, the interop payments — none of which this
// package implements yet. A rejection of the sealed value is ROUTER116, which
// is our encryption being wrong; ROUTER115 is the payer typing the wrong PIN.
func EncryptPIN(pin string, pub *rsa.PublicKey) (string, error) {
	errb := airtelErr("encrypt_pin")

	if pub == nil {
		return "", errb.
			Code(pkgErrors.CodeMissingDependency).
			With(pkgErrors.AttrDependency, "rsa public key").
			Errorf("no public key was supplied")
	}
	if pin == "" {
		return "", errb.Code(pkgErrors.CodeMissingDependency).Errorf("no PIN was supplied")
	}
	if len(pin) > pub.Size()-11 {
		return "", errb.Code(pkgErrors.CodeBuildFailed).Errorf("the PIN is too long for the RSA key")
	}

	//nolint:staticcheck // SA1019: the portal specifies RSA/ECB/PKCS1Padding; OAEP is rejected as ROUTER116.
	sealed, err := rsa.EncryptPKCS1v15(rand.Reader, pub, []byte(pin))
	if err != nil {
		return "", errb.Code(pkgErrors.CodeBuildFailed).Wrapf(err, "could not seal the PIN")
	}
	return base64.StdEncoding.EncodeToString(sealed), nil
}
