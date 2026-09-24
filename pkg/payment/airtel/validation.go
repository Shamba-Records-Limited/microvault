package airtel

import (
	"strings"

	"github.com/samber/oops"

	pkgErrors "github.com/Shamba-Records-Limited/microvault/pkg/errors"
)

// Field limits checked locally. Airtel documents no length for Collection's
// reference; the 25 characters its sibling Merchant Collection documents is
// the only figure the catalogue gives, so it is the conservative ceiling.
const (
	maxReference     = 25
	maxTransactionID = 64
)

// validatePayment checks what can be checked before the wire.
//
// Every one of these would otherwise come back as ROUTER003 or a bare 400,
// which names no field and leaves the caller guessing which of six it was.
func validatePayment(errb oops.OopsErrorBuilder, req PaymentRequest) error {
	if strings.TrimSpace(req.TransactionID) == "" {
		return errb.
			Code(pkgErrors.CodeMissingDependency).
			Errorf("a partner-unique transaction id is required")
	}
	if len(req.TransactionID) > maxTransactionID {
		return errb.
			Code(pkgErrors.CodeBuildFailed).
			With("length", len(req.TransactionID)).
			Errorf("the transaction id is too long")
	}
	if strings.TrimSpace(req.Reference) == "" {
		return errb.
			Code(pkgErrors.CodeMissingDependency).
			Errorf("a reference is required")
	}
	if len(req.Reference) > maxReference {
		return errb.
			Code(pkgErrors.CodeBuildFailed).
			With("length", len(req.Reference)).
			With("limit", maxReference).
			Errorf("the reference is too long")
	}
	if req.AmountKES <= 0 {
		return errb.
			Code(pkgErrors.CodeInvalidAmount).
			With(pkgErrors.AttrAmountLocal, req.AmountKES).
			Errorf("the amount must be positive")
	}
	return nil
}
