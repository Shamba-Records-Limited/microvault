// Package moneygram is the MoneyGram-flavoured wrapper around the generic
// stellaranchor SDK. The SEP-1/9/10/24 protocol primitives plus JWT cache
// and child-memo derivation live in pkg/payment/stellaranchor; this package
// composes them with MoneyGram's REST OAuth and FX Rate APIs.
//
// The package is a pure client library: no database, no SMS delivery, no
// USSD or web flows. Consumers wire those concerns themselves. See the
// integration plan in internal-docs/moneygram-integration.md for the end-to-
// end design this SDK is built to support.
//
// Custodial wallets only — a single Stellar treasury account holds funds for
// many users, who are scoped by a 64-bit positive-integer memo derived from
// their child-account index via stellaranchor.ChildAccountMemo.
package moneygram
