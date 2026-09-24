package airtel

import (
	"strings"

	pkgErrors "github.com/Shamba-Records-Limited/microvault/pkg/errors"
	"github.com/Shamba-Records-Limited/microvault/pkg/phone"
)

// MSISDN is a subscriber number in the national form Airtel requires: nine
// digits, no country code, no leading zero.
//
// It is a distinct type because the requirement is the exact inverse of
// Daraja's. M-Pesa wants 2547XXXXXXXX and Airtel's Collection documentation
// says, in bold, not to send the country code. Both rails serve the same
// borrowers from the same platform, and a bare string moves between them
// without complaint.
type MSISDN string

// String renders the national form.
func (m MSISDN) String() string { return string(m) }

// nationalDigits is the length of a Kenyan subscriber number without its
// country code or trunk zero.
const nationalDigits = 9

// NormalizeMSISDN renders a Kenyan number in the national form Airtel wants.
//
// It accepts every shape the platform stores — +254733…, 254733…, 0733…, and
// the bare national form — and refuses anything that is not a Kenyan
// subscriber number. A national-format number carries no country context, so
// pkg/phone yields nothing for it; this rail serves Kenya only, which is what
// makes assuming the country safe here and nowhere more general.
func NormalizeMSISDN(value string) (MSISDN, error) {
	errb := airtelErr("normalize_msisdn").With("msisdn", phone.Redact(value))

	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return "", errb.
			Code(pkgErrors.CodeMissingPhoneNumber).
			Errorf("no phone number was supplied")
	}

	digits := onlyDigits(trimmed)
	switch {
	case strings.HasPrefix(digits, "254"):
		digits = strings.TrimPrefix(digits, "254")
	case strings.HasPrefix(digits, "0"):
		digits = strings.TrimPrefix(digits, "0")
	}

	if len(digits) != nationalDigits {
		return "", errb.
			Code(pkgErrors.CodeMissingPhoneNumber).
			With("digits", len(digits)).
			Errorf("phone number is not a Kenyan MSISDN")
	}
	if !strings.HasPrefix(digits, "7") && !strings.HasPrefix(digits, "1") {
		return "", errb.
			Code(pkgErrors.CodeMissingPhoneNumber).
			Errorf("phone number is not a Kenyan mobile number")
	}
	return MSISDN(digits), nil
}

func onlyDigits(value string) string {
	return strings.Map(func(r rune) rune {
		if r >= '0' && r <= '9' {
			return r
		}
		return -1
	}, value)
}
