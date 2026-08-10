// Package stellaranchor provides the reusable Stellar-anchor primitives that
// any SEP-24-compliant anchor implementation can compose on top of.
//
// Protocol surface:
//   - sep1.go    — TOML fetch + validation of WEB_AUTH / TRANSFER_SERVER /
//     SIGNING_KEY / NETWORK_PASSPHRASE / USDC issuer
//   - sep9.go    — KYC Customer payload + SplitFullName helper
//   - sep10.go   — challenge / cosign / token submit flow
//   - sep24.go   — interactive withdraw initiation, transaction lookup, and
//     the Status enum that callers poll on
//
// Supporting machinery:
//   - client.go    — top-level Client that wires Auth + Anchor + JWTCache
//     from a single Config
//   - jwt_cache.go — per-memo JWT cache so SEP-10 tokens are reused until
//     near expiry
//   - memo.go      — ChildAccountMemo derivation (per-user 64-bit positive
//     integer memo) for custodial-wallet anchors
//   - iso.go       — ISO-3166 alpha-2 to alpha-3 mapping for SEP-9 country
//     fields
//   - util.go      — shared low-level helpers
//   - errors.go    — protocol-level sentinels and parsed error types
//
// Anchor-specific packages (e.g. moneygram) embed *stellaranchor.Client and
// layer their own REST / OAuth / FX surface on top. Nothing here knows about
// any specific anchor, channel, database, or notification system.
package stellaranchor
