package moneygram

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
	StateOrProvince    string `json:"state_or_province,omitempty"`    // ISO-3166-2; only USA/CAN/MEX
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
