// Package config is the single source of application configuration. It reads the
// environment once at startup, validates it, and hands back a typed Config that
// the rest of the platform passes around instead of touching os.Getenv directly.
//
// New is the only entry point. It loads every variable, fails fast if a required
// one is missing, applies defaults for the optional ones, and validates the
// Stellar material — treasury, admin, and server secret keys, the USDC issuer,
// and the contract ID are parsed through the SDK so a malformed value is caught
// at boot rather than on first use. The network passphrase is derived from the
// environment: development runs against the test network, everything else against
// the public network.
//
// # Layout
//
// Config is composed of one sub-config per concern — Postgres, Redis, Server,
// Stellar, Payments, Mobile, and Auth — and Payments nests one config per
// provider. Sub-configs carry the small helpers that turn raw fields into usable
// values: PostgresConfig.DSN, RedisConfig.Addr, ServerConfig's listen addresses,
// StellarConfig.NewRpcClient, and so on. Some values are shared or derived rather
// than read twice — the MoneyGram anchor reuses the Stellar network passphrase
// and falls back to the global USDC issuer when its own is unset.
//
// # Provider configuration
//
// MoneyGram is the default cash-pickup anchor and is always wired, so it has its
// own Validate to confirm the anchor fields are present. Its REST API
// credentials (for FX rates) are optional; HasRESTCredentials reports whether
// they are populated, letting callers fall back to another provider's rates when
// they are not.
package config
