package stellaranchor

import "errors"

// Sentinel errors. Wrap these with an oops builder at the call site — see
// tomlErr, authErr and anchorErr — so the error carries a domain, a code and
// the attributes needed to diagnose it, while errors.Is against the sentinel
// keeps working for callers that branch on the kind of failure.
var (
	// ErrInvalidConfig is returned by constructors when required configuration
	// is missing or malformed.
	ErrInvalidConfig = errors.New("invalid config")

	// ErrTOMLFetch is returned when the SEP-1 stellar.toml cannot be retrieved.
	ErrTOMLFetch = errors.New("toml fetch failed")

	// ErrTOMLValidation is returned when a fetched TOML is missing required
	// fields or fails sanity checks (network passphrase mismatch, signing key
	// rotation, etc.).
	ErrTOMLValidation = errors.New("toml validation failed")

	// ErrUnauthorized is returned for 401 responses from any authenticated
	// anchor endpoint. Callers should evict cached tokens and retry once.
	ErrUnauthorized = errors.New("unauthorized")

	// ErrInvalidAmount is returned when an anchor sends a decimal amount
	// string that cannot be parsed.
	ErrInvalidAmount = errors.New("invalid amount")
)
