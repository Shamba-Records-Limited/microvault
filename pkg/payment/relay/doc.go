// Package relay routes one transaction to whichever ramp provider offers the
// best price for it.
//
// # What "best" means
//
// Every quote is reduced to one comparable number: RateQuote.EffectiveRate,
// the fiat that changes hands per unit of crypto after every fee on both legs.
// Maximise it on an off-ramp — more shillings for the same USDC — and minimise
// it on an on-ramp, where it is a cost. Never compare providers on a headline
// rate: Fonbnk's exchangeRateAfterFees mixes a pre-fee local amount with a
// post-fee USD one and moves opposite to the user's outcome, and YellowCard
// publishes no priced quote at all.
//
// # Why the amount matters
//
// Fees are banded and often flat. A 30 KES fee is 1.2% of a 2,498 KES payout
// and 15% of a 200 KES one, so the cheapest provider at one size is not the
// cheapest at another. RateRequest therefore always carries the real amount
// and nothing caches a rate per corridor.
//
// # The switch
//
// Router is a kill switch, not a feature gate. With routing off it quotes only
// the default provider — YellowCard — so flipping
// ENABLE_PAYMENT_PROVIDER_RELAY_SWITCH off is safe to do under load and
// restores the previous behaviour exactly. With routing on, one provider
// failing is tolerated and logged; every provider failing is an error.
//
// # The margin guard
//
// Best routes. BestWithinMargin additionally quotes the opposite direction and
// refuses when a round trip would sell crypto for less than it costs to buy
// back, beyond the configured floor. That doubles the quote calls, so it
// belongs on treasury-scale movements rather than on every borrower
// transaction. Spread exposes the same figures for monitoring.
//
// # Adding a provider
//
// This package knows nothing about any vendor. Implement RateSource, register
// it, and it competes on equal terms with everything else:
//
//	registry := relay.NewRegistry()
//	registry.MustRegister(sources.NewYellowCardSource(...))
//	registry.MustRegister(myBankSource{})
//	router, err := relay.New(relay.Config{Registry: registry, Default: "yellowcard"})
//
// A source only has to turn a corridor and an amount into one EffectiveRate.
// Use SourceErr to build failures so they land in the same domain as the
// platform's own, and return an error rather than a zero rate — a zero would
// win an on-ramp by being smallest.
//
// Implement the optional DirectionalSource on a one-way rail so the Router
// skips it for the direction it cannot serve, rather than calling it and
// logging a failure on every transaction.
//
// The platform's own sources live in the sources subpackage, which is the only
// thing here that imports a vendor. Importing relay alone pulls in no provider
// client.
//
// # Provider quirks the platform's sources absorb
//
// YellowCard publishes buy and sell rates named from the customer's side: sell
// is USD to local and belongs to an off-ramp, buy is local to USD and belongs
// to an on-ramp. Swapping them inverts every routing decision without failing
// anything. Its channels are ramp-scoped too, so an off-ramp must not be
// priced against a deposit channel's fees.
//
// Fonbnk prices a quote for real, with the amount on the crypto leg in both
// directions — the side we always know — so one call prices the corridor at
// the size actually being moved.
package relay
