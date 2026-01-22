package validation

import (
	"github.com/go-playground/validator/v10"
	"github.com/stellar/go-stellar-sdk/xdr"
)

// validateStellarXDR validates that a string is a valid Stellar XDR transaction.
func validateStellarXDR(fl validator.FieldLevel) bool {
	xdrStr := fl.Field().String()

	if xdrStr == "" {
		return false
	}

	// Attempt to unmarshal the XDR string into a Stellar transaction envelope
	var txEnvelope xdr.TransactionEnvelope
	err := xdr.SafeUnmarshalBase64(xdrStr, &txEnvelope)

	return err == nil
}
