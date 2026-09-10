// Package vaultwatch is the on-chain allowlist's detect-and-quarantine
// watcher (Option D of the compliance design, defense-in-depth alongside the
// contract's own gating). It watches the vault contract's Soroban events on
// a wall-clock cadence and confirms every deposit, mint and share transfer's
// participants were actually allowlisted at the time.
//
// This is a bespoke ticker rather than an mgpoller.Runner[T], for the same
// reason the M-Pesa Pull sweep is: it walks a ledger window, not a queue of
// due rows.
//
// It is a canary, not a second enforcer. Once the contract's own gating is
// live, an unallowlisted deposit or transfer should be impossible — a
// mismatch here means the gate itself is broken (a bug, a bad upgrade, or a
// misconfiguration), which is why a mismatch is logged at alert level rather
// than silently corrected.
package vaultwatch
