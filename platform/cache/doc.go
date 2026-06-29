// Package cache manages Redis connection lifecycle and provides a Redis-backed
// idempotency store for safe request replay.
//
// Connections are named and cached process-wide. GetConnection returns the
// existing client for a name or creates one with double-checked locking; the
// underlying client is configured with sensible timeouts (5 s dial, 3 s
// read/write) and a small pool (10 connections, 5 idle). Ready pings a named
// connection under a 1 s timeout and is the hook the health package uses for
// its readiness probe. GetClient, ListConnections, CloseConnection, and
// CloseAll cover lookup and graceful shutdown.
//
// IdempotencyStore is the contract for caching a response keyed by an
// idempotency key. RedisIdempotencyStore is the Redis implementation: it
// serializes an IdempotencyRecord (response body, content type, status code,
// creation time) as JSON under a configurable prefix, applies the caller's
// TTL, and treats a missing key as (nil, nil) rather than an error. Build
// one with NewRedisIdempotencyStore, passing the client from GetConnection
// and a prefix such as "idempotency".
package cache
