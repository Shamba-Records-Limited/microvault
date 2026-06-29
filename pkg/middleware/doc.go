// Package middleware holds the Fiber middleware that wraps the HTTP layer:
// authentication, a uniform response envelope, and the standard cross-cutting
// stack every request passes through.
//
// # Authentication
//
// AuthMiddleware guards the admin routes. RequireAuth reads the JWT from the
// admin_token cookie, validates it, and confirms its claims name the configured
// admin — a missing, invalid, or wrong-admin token is rejected and the cookie
// cleared. When a valid token is inside its refresh window the middleware mints a
// fresh one and resets the cookie, giving the admin a sliding session. The cookie
// is HTTP-only, Secure, and SameSite=Strict. Validated claims are stashed in the
// request context under AdminClaimsKey; downstream handlers read them with
// GetAdminClaims.
//
// # Response envelope
//
// FormatResponse gives every endpoint the same JSON shape — status, code, data,
// and message. Handlers don't write the body themselves: they put their result in
// the data local or, on failure, return a Fiber error and set the error local.
// The middleware reads whichever is present, derives the HTTP status and a
// human-readable message, and marks the envelope success or error.
//
// # Cross-cutting stack
//
// FiberMiddleware registers the rest in one call: security headers (helmet),
// panic recovery, CORS with credentials, a request rate limiter, request logging,
// and a favicon handler. It also mounts the /health and /ready endpoints backed
// by the health checker.
package middleware
