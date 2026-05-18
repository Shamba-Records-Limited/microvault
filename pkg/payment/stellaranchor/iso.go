package stellaranchor

// iso2to3 maps ISO-3166-1 alpha-2 country codes to alpha-3.
// MoneyGram's SEP-9 address_country_code requires alpha-3.
//
// Limited to the corridors microvault and similar wallets are expected to
// serve initially. Extend as new corridors come online; missing entries
// cause CountryISO3 to return "" so callers can omit the field rather than
// guess.
var iso2to3 = map[string]string{
	// East Africa
	"KE": "KEN", "UG": "UGA", "TZ": "TZA", "RW": "RWA", "BI": "BDI", "ET": "ETH", "SS": "SSD",
	// West Africa
	"NG": "NGA", "GH": "GHA", "SN": "SEN", "CI": "CIV", "ML": "MLI", "BF": "BFA",
	// Southern Africa
	"ZA": "ZAF", "ZM": "ZMB", "ZW": "ZWE", "MW": "MWI", "MZ": "MOZ", "BW": "BWA", "NA": "NAM",
	// North Africa
	"EG": "EGY", "MA": "MAR", "TN": "TUN", "DZ": "DZA",
	// Common destination countries (for diaspora corridors)
	"US": "USA", "GB": "GBR", "CA": "CAN", "AU": "AUS",
	"FR": "FRA", "DE": "DEU", "IT": "ITA", "ES": "ESP", "NL": "NLD",
	"AE": "ARE", "SA": "SAU", "QA": "QAT", "KW": "KWT", "OM": "OMN",
	"IN": "IND", "PK": "PAK", "BD": "BGD", "PH": "PHL", "ID": "IDN",
	"CN": "CHN", "JP": "JPN", "KR": "KOR", "MY": "MYS", "SG": "SGP",
	"BR": "BRA", "MX": "MEX", "AR": "ARG", "CO": "COL", "CL": "CHL",
}

// CountryISO3 returns the ISO-3166-1 alpha-3 code for the given alpha-2 code,
// or "" if the alpha-2 is not in the map. Callers should omit the SEP-9
// address_country_code field entirely when this returns "" — sending a wrong
// or guessed code is worse than sending none.
func CountryISO3(iso2 string) string {
	return iso2to3[iso2]
}
