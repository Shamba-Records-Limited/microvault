// Package routes wires the public HTTP route group onto a Fiber app.
//
// PublicRoutes mounts everything under /api/v1 and is the single registration
// point called from cmd/microvault. It takes the controllers built in main.go
// — auth, USSD, webhook, SMS callback — and binds each to its path and method.
// The auth routes run through the FormatResponse middleware so responses share
// the platform's envelope; the USSD, SMS, and webhook routes are passed
// through verbatim because their callers (telecom gateways, payment
// providers) expect a specific raw shape rather than the JSON envelope.
//
// Webhook routes carry no auth middleware — YellowCard webhooks are
// authenticated by HMAC signature verification inside the handler, not by a
// session or JWT. New webhook routes should follow the same pattern: verify
// the caller's signature within the handler, never trust the transport.
package routes
