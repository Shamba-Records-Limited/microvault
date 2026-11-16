// Package iso3166 lists ISO 3166-1 countries and their first-level ISO
// 3166-2 subdivisions, generated from a pinned iso-codes release.
package iso3166

//go:generate go run gen.go

import "strings"

// Country is one ISO 3166-1 entry.
type Country struct {
	Code string
	Name string
}

var byCode = func() map[string]string {
	m := make(map[string]string, len(Countries))
	for _, c := range Countries {
		m[c.Code] = c.Name
	}
	return m
}()

// Name returns the English name for an alpha-2 code, case-insensitively.
func Name(code string) (string, bool) {
	name, ok := byCode[strings.ToUpper(code)]
	return name, ok
}

// Valid reports whether code is an ISO 3166-1 alpha-2 code.
func Valid(code string) bool {
	_, ok := Name(code)
	return ok
}

// Subdivision is one first-level ISO 3166-2 entry, such as a state or county.
type Subdivision struct {
	Code string
	Name string
}

// SubdivisionsOf returns a country's first-level subdivisions, sorted by
// name; nil for a country without any.
func SubdivisionsOf(country string) []Subdivision {
	return subdivisions[strings.ToUpper(country)]
}

// SubdivisionName returns the name for an ISO 3166-2 code such as "KE-30".
func SubdivisionName(code string) (string, bool) {
	code = strings.ToUpper(code)
	country, _, ok := strings.Cut(code, "-")
	if !ok {
		return "", false
	}
	for _, s := range subdivisions[country] {
		if s.Code == code {
			return s.Name, true
		}
	}
	return "", false
}

// ValidSubdivision reports whether code is a first-level subdivision of
// country.
func ValidSubdivision(country, code string) bool {
	country = strings.ToUpper(country)
	if !strings.HasPrefix(strings.ToUpper(code), country+"-") {
		return false
	}
	_, ok := SubdivisionName(code)
	return ok
}
