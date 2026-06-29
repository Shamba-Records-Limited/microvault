// Package validation turns struct-tag validation into user-friendly field errors.
// It wraps go-playground/validator so handlers can validate a request and get
// back a map of field name to a readable message rather than the library's raw
// error.
//
// ValidatorService is the entry point, built with NewValidatorService. Validate
// runs the struct tags on a value and returns the per-field messages; field names
// are reported in snake_case so they match the JSON the client sent. Each
// built-in validation tag has a default message, and the service ships with the
// custom validators the platform needs — for example stellar_xdr, which checks
// that a field decodes as a Stellar transaction envelope.
//
// The validator set is extensible. RegisterValidator adds a new tag with its own
// check and message, and RegisterErrorMessage overrides the wording for an
// existing tag, so callers can tailor validation without forking the package.
package validation
