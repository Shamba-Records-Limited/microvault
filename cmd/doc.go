// Package cmd holds the application's entry points. It is an organizational
// namespace with no Go code of its own; each sub-directory is a separate
// main package that builds into a binary.
//
// microvault is the API server — it wires configuration, platform
// infrastructure (Postgres, Redis, Stellar), the domain services, and the
// mobile/USSD/SMS channels into a Fiber app and serves HTTP until signalled.
// migrate is the database migration CLI — a thin wrapper around
// platform/database that exposes up, down, version, and force subcommands.
package cmd
