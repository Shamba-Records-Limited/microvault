package elliptic

import (
	"errors"
	"net/http"

	"github.com/samber/oops"

	pkgErrors "github.com/Shamba-Records-Limited/microvault/pkg/errors"
)

// ErrNotInBlockchain is Elliptic's 404 NotInBlockchain: the address has
// never appeared on chain. Per the source design doc §4, this is the
// expected first outcome for a freshly generated depositor keypair, not a
// failure — callers check for it with errors.Is and turn it into
// compliance.VerdictUnscreenable rather than treating it as an error.
var ErrNotInBlockchain = errors.New("elliptic: address has no on-chain history")

func ellipticErr(op string) oops.OopsErrorBuilder {
	return oops.In(pkgErrors.DomainCompliance).Tags("elliptic").With(pkgErrors.AttrOperation, op)
}

// classifyStatus maps Elliptic's documented status codes (source design doc
// §4) to the shared error vocabulary. A nil return means the caller should
// proceed to decode the body as a success.
func classifyStatus(op string, statusCode int, body []byte, header http.Header) error {
	switch statusCode {
	case http.StatusOK:
		return nil
	case http.StatusBadRequest:
		return ellipticErr(op).Code(pkgErrors.CodeBuildFailed).
			With(pkgErrors.AttrStatusCode, statusCode).With("body", string(body)).
			Errorf("elliptic rejected the request as malformed")
	case http.StatusUnauthorized:
		return ellipticErr(op).Code(pkgErrors.CodeUnauthorized).
			Errorf("elliptic rejected the request's signature or key")
	case http.StatusForbidden:
		return ellipticErr(op).Code(pkgErrors.CodeMerchantNotPermitted).
			Errorf("the asset or feature is not on our elliptic plan")
	case http.StatusNotFound:
		// A verdict, not an error — see ErrNotInBlockchain's doc comment.
		return ellipticErr(op).Code(pkgErrors.CodeNotFound).
			Wrapf(ErrNotInBlockchain, "elliptic has no on-chain record of the address")
	case http.StatusTooManyRequests:
		return ellipticErr(op).Code(pkgErrors.CodeHTTPError).
			With("rate_limit_reset", header.Get("X-RateLimit-Reset")).
			Errorf("elliptic rate limit exceeded")
	default:
		return ellipticErr(op).Code(pkgErrors.CodeHTTPError).
			With(pkgErrors.AttrStatusCode, statusCode).With("body", string(body)).
			Errorf("elliptic returned an unexpected status")
	}
}
