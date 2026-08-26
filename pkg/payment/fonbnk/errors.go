package fonbnk

import (
	"github.com/samber/oops"

	pkgErrors "github.com/Shamba-Records-Limited/microvault/pkg/errors"
)

// fonbnkErr starts an error builder for Fonbnk calls. Both directions share
// DomainOffRamp; direction is an attribute where it is knowable.
func fonbnkErr(op string) oops.OopsErrorBuilder {
	return oops.
		In(pkgErrors.DomainOffRamp).
		Tags("fonbnk").
		With(pkgErrors.AttrProvider, ProviderName).
		With(pkgErrors.AttrOperation, op)
}

// orderErr scopes an error to one order.
func orderErr(op, orderID string) oops.OopsErrorBuilder {
	return fonbnkErr(op).With(pkgErrors.AttrOrderID, orderID)
}
