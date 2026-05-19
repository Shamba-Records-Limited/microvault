// Package moneygram is the MoneyGram-flavoured wrapper around the generic
// stellaranchor SDK.
//
// The SEP-1/9/10/24 protocol primitives plus JWT cache and child-memo
// derivation live in pkg/payment/stellaranchor; this package composes them
// with MoneyGram's REST OAuth (oauth.go) and FX Rate API (fxrate.go), and
// adds a cascading FX orchestrator (fx.go) that falls back across rate
// sources. payload.go defines the Options and CashPickupPayload types that
// flow through offramp.Request / offramp.Result.
//
// The package is a pure client library — no database, no SMS delivery, no
// USSD or web flows. Wiring those concerns is the consumer's job.
//
// Custodial wallets only: a single Stellar treasury account holds funds for
// many users, scoped by a 64-bit positive-integer memo derived from each
// user's child-account index via stellaranchor.ChildAccountMemo.
package moneygram
