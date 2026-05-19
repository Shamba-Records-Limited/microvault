// Package payment is the umbrella for the platform's off-ramp surface. It
// is an organizational namespace with no Go code of its own; the actual
// behaviour lives in its sub-packages.
//
// offramp holds the channel-agnostic contract (Provider, capability
// interfaces, Registry). stellaranchor holds the reusable SEP-1/9/10/24
// protocol primitives plus JWT cache and ChildAccountMemo derivation.
// moneygram, yellowcard and fonbnk hold one provider implementation each.
package payment
