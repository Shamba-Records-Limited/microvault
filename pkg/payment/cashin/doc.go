// Package cashin is the collection-side counterpart to offramp.
//
// offramp is payout-shaped: money out to a recipient. cashin is
// collection-shaped: money in toward a loan, via a paybill. The two are
// siblings rather than one abstraction because the request shapes, the
// capability set and the registry resolution domain are all different.
//
// The mandatory capability is Collector; Prompter, StatusReader, Reconciler,
// Reverser and PayerVerifier are optional interfaces that a caller type-asserts
// for. A provider that does not implement one of those must not stub it —
// absence is honest; a stub is a lie (the pkg/payment README rule).
//
// M-Pesa implements all six. MoneyGram implements Collector and StatusReader
// only, and migration of MoneyGram onto cashin is deliberately out of scope.
package cashin
