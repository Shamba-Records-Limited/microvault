package mpesa

import (
	"strings"

	pkgErrors "github.com/Shamba-Records-Limited/microvault/pkg/errors"
	"github.com/Shamba-Records-Limited/microvault/pkg/phone"
)

// MaskedMSISDN is a phone number as Daraja discloses it on a C2B confirmation:
// "2547 ***** 126". It is a distinct type so it can never be passed where a
// real number is expected — the masking is not reversible, and a masked value
// silently used as an MSISDN would attribute payments to a number that does not
// exist.
type MaskedMSISDN string

// String renders the masked value.
func (m MaskedMSISDN) String() string { return string(m) }

// Masked reports whether the value actually carries mask characters. A C2B
// confirmation is documented as masked, so a value without them is worth
// noticing rather than trusting.
func (m MaskedMSISDN) Masked() bool { return strings.Contains(string(m), "*") }

// NormalizeMSISDN renders a Kenyan number in the 2547XXXXXXXX form Daraja
// requires: twelve digits, no plus, no separators.
//
// A national-format number cannot be resolved without country context, so
// pkg/phone yields nothing for it and this treats it as Kenyan, which is the
// only country this rail serves.
func NormalizeMSISDN(value string) (string, error) {
	errb := mpesaErr("normalize_msisdn").With("msisdn", phone.Redact(value))

	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return "", errb.
			Code(pkgErrors.CodeMissingPhoneNumber).
			Errorf("no phone number was supplied")
	}

	digits := onlyDigits(trimmed)
	switch {
	case strings.HasPrefix(digits, "254"):
	case strings.HasPrefix(digits, "0"):
		digits = "254" + strings.TrimPrefix(digits, "0")
	case len(digits) == 9 && strings.HasPrefix(digits, "7"), len(digits) == 9 && strings.HasPrefix(digits, "1"):
		digits = "254" + digits
	}

	if len(digits) != 12 || !strings.HasPrefix(digits, "254") {
		return "", errb.
			Code(pkgErrors.CodeMissingPhoneNumber).
			Errorf("phone number is not a Kenyan MSISDN")
	}
	return digits, nil
}

func onlyDigits(value string) string {
	var b strings.Builder
	for _, r := range value {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	return b.String()
}
