package compliance

import "errors"

var (
	// ErrInvalidAddress is a submitted address that isn't a well-formed
	// Stellar Ed25519 public key — rejected before any Elliptic call.
	ErrInvalidAddress = errors.New("address is not a valid stellar public key")

	// ErrCounterpartyMissing means an address's counterparty association
	// could not be loaded — should not happen given the foreign key, but
	// screening needs the counterparty's elliptic_customer_reference, so
	// this is checked explicitly rather than trusting a nil pointer.
	ErrCounterpartyMissing = errors.New("address has no associated counterparty")

	// ErrKYBNotConfigured means no KYB provider is configured.
	ErrKYBNotConfigured = errors.New("kyb verification is not configured")

	// ErrKYBAlreadyApproved means the counterparty's KYB is already approved.
	ErrKYBAlreadyApproved = errors.New("kyb is already approved for this counterparty")

	// ErrKYBNotVerified means the KYB provider has not approved the business,
	// so it cannot be approved here.
	ErrKYBNotVerified = errors.New("the kyb provider has not approved this business yet")
)
