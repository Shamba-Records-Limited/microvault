package fonbnk

import (
	"github.com/samber/oops"

	pkgErrors "github.com/Shamba-Records-Limited/microvault/pkg/errors"
)

// fonbnkErr starts an error builder for Fonbnk calls in DomainOffRamp.
// withDirection moves an on-ramp corridor into DomainOnRamp.
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
