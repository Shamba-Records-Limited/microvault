package moneygram

import "errors"

// ErrServiceOptionUnavailable is returned by FXRateClient when the requested
// ServiceOption is not present in the response — e.g. cash pickup is not
// offered in the destination country.
var ErrServiceOptionUnavailable = errors.New("service option unavailable for corridor")
