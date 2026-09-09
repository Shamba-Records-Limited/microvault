package darajastub

import (
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"errors"
)

// parsePublic reads the RSA public key out of a PEM certificate.
func parsePublic(raw []byte) (*rsa.PublicKey, error) {
	block, _ := pem.Decode(raw)
	if block == nil {
		return nil, errors.New("darajastub: certificate is not PEM")
	}
	certificate, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return nil, err
	}
	key, ok := certificate.PublicKey.(*rsa.PublicKey)
	if !ok {
		return nil, errors.New("darajastub: certificate does not carry an RSA key")
	}
	return key, nil
}
