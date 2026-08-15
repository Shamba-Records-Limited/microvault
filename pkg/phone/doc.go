// Package phone holds the platform's shared phone-number helpers: redaction for
// logs, and three normalisers with deliberately different contracts.
//
// # Redaction
//
// Redact masks the middle of a number for safe logging, keeping the first six
// digits (country code plus network prefix) and the last three, so
// "254711222111" becomes "254711***111". A leading "+" is preserved, and a
// number too short to have a maskable middle is returned unchanged.
//
// # Normalisation
//
// Three functions convert to international form, and they are not
// interchangeable — pick by how much you trust the input:
//
//   - Normalize is the most lenient: it trims whitespace and ensures a leading
//     "+", nothing more. Use it when the number is already known to carry a
//     country code (e.g. a YellowCard destination).
//   - E164 is the strictest: it strips separators, turns a "00" prefix into
//     "+", and returns "" for a national-format number (leading "0", no country
//     code) rather than guess a country. Use it where a wrong number is worse
//     than none, such as the SEP-9 mobile_number sent to anchors.
//   - Format strips non-digits and prepends a country code (default +254) when
//     the number is not already international.
package phone
