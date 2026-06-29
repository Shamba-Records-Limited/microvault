// Package stellar is the single entry point for everything the platform does on
// the Stellar network — both classic operations (accounts, trustlines, payments)
// and Vault smart-contract calls.
//
// Service composes classic.Service and soroban.Service, so one handle covers
// off-chain and on-chain work. Build it with NewService, passing the RPC client,
// network passphrase, treasury and admin keys, the Vault contract ID, and the
// USDC issuer. The package also re-exports the request/response types and
// sentinel errors from the types package, so callers need only import this one.
//
// The sub-packages hold the implementations: classic for off-chain Stellar ops,
// soroban for the Vault contract client, rpc for transaction confirmation, types
// for the shared DTOs and errors, and testing for the RPC mock.
//
// One thing worth knowing up front: child accounts are fully sponsored by the
// treasury and hold zero XLM. They exist as tracking and audit markers, never
// hold USDC, and are created without a trustline. See the classic package for
// the full custody and sponsorship model, and docs/stellar/client.md for the
// complete reference.
package stellar
