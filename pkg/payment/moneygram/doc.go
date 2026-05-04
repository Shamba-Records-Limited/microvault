// Package moneygram is a Go SDK for MoneyGram Ramps — the cash-pickup and
// cash-deposit anchor on the Stellar network — implementing the SEP-1, SEP-10,
// and SEP-24 protocols plus MoneyGram's REST FX Rate API.
//
// The package is a pure client library: no database, no SMS delivery, no USSD
// or web flows. Consumers wire those concerns themselves. See the integration
// plan in internal-docs/moneygram-integration.md for the end-to-end design
// this SDK is built to support.
//
// Custodial wallets only — a single Stellar treasury account holds funds for
// many users, who are scoped by a 64-bit positive-integer memo derived from
// their child-account index via ChildAccountMemo.
package moneygram
