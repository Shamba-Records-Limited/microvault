package moneygram

import "errors"

// Sentinel errors. Wrap with fmt.Errorf("moneygram: ...: %w", err) at call sites.
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
	// MoneyGram endpoint. Callers should evict cached tokens and retry once.
	ErrUnauthorized = errors.New("unauthorized")

	// ErrServiceOptionUnavailable is returned by FXRateClient when the
	// requested ServiceOption is not present in the response — e.g. cash
	// pickup is not offered in the destination country.
	ErrServiceOptionUnavailable = errors.New("service option unavailable for corridor")
)
