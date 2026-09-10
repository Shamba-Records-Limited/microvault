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
)
