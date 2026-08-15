package stellaranchor

import "errors"

// Sentinel errors. Wrap with fmt.Errorf("stellaranchor: ...: %w", err) at call
// sites. Anchor-specific consumers (e.g. moneygram, future SDKs) should re-
// wrap with their own prefix so log readers can tell which anchor failed.
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
