// Package fonbnk provides a low-level HTTP client for the Fonbnk on-ramp API.
//
// Currently only request signing is wired; the off-ramp methods are pending
// product confirmation on whether Fonbnk is in scope. The package exists so
// that the auth transport can be reused once endpoints are integrated.
package fonbnk
