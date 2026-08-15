// Package yellowcard is the low-level HTTP client for the YellowCard
// mobile-money off-ramp API.
//
// yellowcard.go contains an http.RoundTripper that signs outbound requests
// with YellowCard's YcHmacV1 scheme (HMAC-SHA256 over the timestamp, path,
// method and base64-encoded body hash) and the typed methods that wrap each
// REST endpoint. types.go defines the on-the-wire request/response shapes.
// errors.go defines the package's sentinel errors and the parsed API error
// type.
//
// payload.go defines Options (carrying SettlementMethod — "direct" or
// "fiat") plus DirectSettlementPayload and FiatPayload — the typed values
// that flow through offramp.Request.Options and offramp.Result.Provider for
// callers that need YC-specific input or output.
//
// The package is a pure client library: it knows how to talk to YellowCard
// and nothing about USSD, persistence, or notifications.
package yellowcard
