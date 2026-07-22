package stellaranchor

import "strings"

// Customer carries the subset of SEP-9 fields MoneyGram honours when passed
// at SEP-24 withdrawal initiation. MoneyGram silently ignores SEP-9 keys it
// does not support (email_address, id_*, occupation, etc.).
//
// All fields are optional from MoneyGram's perspective; populate as much as
// you can to reduce friction in MoneyGram's interactive webview.
type Customer struct {
	FirstName          string `json:"first_name,omitempty"`
	LastName           string `json:"last_name,omitempty"`
	MobileNumber       string `json:"mobile_number,omitempty"` // E.164, e.g. "+254712345678"
	BirthDate          string `json:"birth_date,omitempty"`    // YYYY-MM-DD
	Address            string `json:"address,omitempty"`       // line1[, line2]
	City               string `json:"city,omitempty"`
	PostalCode         string `json:"postal_code,omitempty"`
	AddressCountryCode string `json:"address_country_code,omitempty"` // ISO-3, e.g. "KEN"
}

// SplitFullName splits a full-name string on the first whitespace into
// (first, last). One-token names become (token, ""); empty input yields
// ("", ""). Used to map a single stored full_name field onto SEP-9
// first_name / last_name keys.
func SplitFullName(fullName string) (first, last string) {
	s := strings.TrimSpace(fullName)
	if s == "" {
		return "", ""
	}
	if i := strings.IndexAny(s, " \t"); i >= 0 {
		return s[:i], strings.TrimSpace(s[i+1:])
	}
	return s, ""
}

// E164 normalises a phone number to the "+<country><subscriber>" form SEP-9
// requires for mobile_number. Anchors reject or silently drop values without
// the leading "+", so numbers stored plus-less (the Africa's Talking USSD
// callback strips it) must be restored here.
//
// Separators are removed, a "00" international prefix becomes "+", and a bare
// international number gains a "+". A national-format number (leading "0", no
// country code) cannot be resolved without country context and yields "" so
// the caller omits the field rather than sending a wrong one.
func E164(phone string) string {
	var b strings.Builder
	for _, r := range phone {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	digits := b.String()
	if digits == "" {
		return ""
	}
	digits = strings.TrimPrefix(digits, "00")
	if digits == "" || strings.HasPrefix(digits, "0") {
		return ""
	}
	return "+" + digits
}
