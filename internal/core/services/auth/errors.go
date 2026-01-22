package auth

import "errors"

// Authentication errors that can be mapped to HTTP status codes by handlers.
var (
	// ErrChallengeNotFound indicates the requested challenge does not exist or has expired.
	ErrChallengeNotFound = errors.New("challenge not found")

	// ErrChallengeExpired indicates the challenge has exceeded its validity period.
	ErrChallengeExpired = errors.New("challenge expired")

	// ErrInvalidTransaction indicates the provided transaction is malformed or invalid.
	ErrInvalidTransaction = errors.New("invalid transaction")

	// ErrTransactionMismatch indicates the signed transaction does not match the original challenge.
	ErrTransactionMismatch = errors.New("transaction does not match challenge")

	// ErrInvalidSignature indicates the transaction is not signed by the expected keypair.
	ErrInvalidSignature = errors.New("invalid or missing signature")

	// ErrInvalidToken indicates the JWT token is malformed, expired, or has an invalid signature.
	ErrInvalidToken = errors.New("invalid token")

	// ErrInvalidTokenClaims indicates the token claims are invalid or cannot be parsed.
	ErrInvalidTokenClaims = errors.New("invalid token claims")

	// ErrUnexpectedSigningMethod indicates the token uses an unsupported signing algorithm.
	ErrUnexpectedSigningMethod = errors.New("unexpected signing method")
)
