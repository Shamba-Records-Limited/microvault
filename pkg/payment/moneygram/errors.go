package moneygram

import "errors"

// ErrServiceOptionUnavailable is returned by FXRateClient when the requested
// ServiceOption is not present in the response — e.g. cash pickup is not
// offered in the destination country.
//
// Anchor-protocol-level sentinels (ErrInvalidConfig, ErrUnauthorized, etc.)
// live in the stellaranchor package since they're shared across anchors.
var ErrServiceOptionUnavailable = errors.New("service option unavailable for corridor")
