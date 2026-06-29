// Package platform holds the process-level infrastructure the application
// runs on top of. It is an organizational namespace with no Go code of its
// own; the actual behaviour lives in its sub-packages.
//
// cache manages Redis connection lifecycle and provides a Redis-backed
// idempotency store for safe request replay. database manages PostgreSQL
// connection lifecycle and runs the embedded schema migrations. Both expose
// a Ready hook that pkg/health consults for its readiness probe.
package platform
