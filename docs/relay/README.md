# Payment Relay

Developer reference for the **relay**: the switch that decides which payment
provider handles one transaction, based on which of them prices it best.

It sits above the ramps rather than inside either one. The
[off-ramp](../offramp/README.md) knows how to pay a borrower through YellowCard,
Fonbnk or MoneyGram; the relay decides *which*.

Code: [`pkg/payment/relay`](../../pkg/payment/relay) for the mechanism,
[`pkg/payment/relay/sources`](../../pkg/payment/relay/sources) for the two
providers we ship.

---

## The switch

`ENABLE_PAYMENT_PROVIDER_RELAY_SWITCH`, unset or `false` by default.

| Setting | Behaviour |
|---|---|
| Off | Every mobile-money payout goes to YellowCard. The other providers are not even quoted. |
| On | YellowCard and Fonbnk are quoted concurrently and the better post-fee rate wins. |

**This is a kill switch, not a feature gate.** Turning it off is always safe to
do under load: it restores the previous dispatch exactly, and a test asserts the
non-default sources record zero calls when it is off. A non-boolean value is
rejected at boot rather than silently read as false. A typo in a Coolify
variable would otherwise disable routing invisibly.

---

## What "best" means

Every provider's quote is reduced to one comparable number:
`RateQuote.EffectiveRate`, the **fiat that changes hands per unit of crypto,
after every fee on both legs**.

| Direction | Optimise | Because |
|---|---|---|
| Off-ramp (selling USDC) | Maximise | More shillings for the same USDC |
| On-ramp (buying USDC) | Minimise | It is a cost, not a yield |

**Never compare providers on a headline rate.** Two concrete traps:

- Fonbnk's `exchangeRateAfterFees` is `amountBeforeFees / amountAfterFeesUsd`,
  a pre-fee local amount over a post-fee USD one. It moves *opposite* to the
  user's outcome: on one observed quote it rose from 124.91 to 126.43 while the
  borrower's payout fell from 2,498 to 2,468 KES. Ranking on it picks the worse
  provider.
- YellowCard publishes no priced quote at all, only a headline rate and a
  per-channel fee. Its effective rate is computed in the source.

### Why the amount is always carried

Fees are banded and often flat. A 30 KES fee is 1.2% of a 2,498 KES payout and
15% of a 200 KES one, so the cheapest provider at one size is not the cheapest
at another. `RateRequest` therefore always carries the real amount, and nothing
caches a rate per corridor.

---

## Scope: what the relay may and may not decide

**Mobile money only.** Cash pickup is a rail the borrower chose at the USSD
menu, not a price decision. A better rate must never move someone away from
collecting cash at an agent, so a cash-pickup loan is not even quoted.

**Never over an explicit pin.** If the caller attached provider `Options`, they
have already chosen; the relay does not second-guess it.

**A relay failure is not a loan failure.** If no provider can be quoted, the
routing call returns empty and the off-ramp registry's own alias resolves as
before. The borrower is disbursed through the default provider.

---

## How routing reaches dispatch

The off-ramp registry resolves a provider from a `PayoutMethod` alias. The relay
therefore returns an alias to pin rather than a provider handle:

| Winner | Alias pinned |
|---|---|
| YellowCard | `mobile_money` (unchanged, the registry's own default) |
| Fonbnk | `mobile_money_fonbnk` |

Fonbnk deliberately registers under a *separate* alias. That is what makes the
switch a genuine no-op when off: the unrouted default still resolves
`mobile_money` to YellowCard, whether or not Fonbnk is wired.

A winner with no alias mapping falls back rather than pinning something the
registry cannot resolve.

---

## The margin guard

`Best` routes. `BestWithinMargin` additionally quotes the **opposite** direction
and refuses when a round trip would sell crypto for less than it costs to buy
back, beyond `MinRoundTripMarginPct`, error code `margin_too_low`.

That doubles the quote calls, so it is a separate method rather than something
on the borrower hot path. It belongs on treasury-scale movements.

`Spread` exposes the same figures for monitoring without refusing anything. On
the observed sandbox corridor it reports **−6.97%**: buying USDC cost 132.65
KES, selling yielded 123.40.

---

## Adding a provider

The relay package imports no vendor. Implement `RateSource`, register it, and it
competes on equal terms:

```go
registry := relay.NewRegistry()
registry.MustRegister(sources.NewYellowCardSource(...))
registry.MustRegister(myBankSource{})

router, err := relay.New(relay.Config{
    Registry: registry,
    Enabled:  cfg.Payments.EnableProviderRelaySwitch,
    Default:  "yellowcard",
})
```

Three rules for a source:

- **Return an error, never a zero rate.** A zero wins an on-ramp by being
  smallest.
- **Use `relay.SourceErr`** so failures land in the same domain and attribute
  keys as the platform's own.
- **Implement the optional `DirectionalSource`** on a one-way rail, MoneyGram
  cash pickup pays out but cannot collect, so the Router skips it for the
  direction it cannot serve instead of logging a failure per transaction.

The platform's own sources live in the `sources` subpackage, which is the only
thing under `relay` that imports a provider client.

---

## Failure behaviour

| Situation | Result |
|---|---|
| One source errors | Logged, excluded, routing continues on the rest |
| Every source errors | `rate_unavailable`; the caller falls back to the default |
| A source panics | Recovered at the goroutine boundary, excluded like any failure. Surfaces as `panic_recovered` when it was the only source. |
| No source serves the direction | `corridor_unavailable`, not a misleading "all providers failed" |

Sources are quoted concurrently. A provider source is third-party-facing code
running on our goroutines, which is why the panic recovery is there rather than
trusted not to be needed.

---

## Related

- [Off-ramp overview](../offramp/README.md): the providers being routed between.
- [fonbnk.md](../offramp/fonbnk.md): why a Fonbnk quote costs one call, and what
  its effective rate is derived from.
- [yellowcard.md](../offramp/yellowcard.md): the buy/sell rate semantics the
  YellowCard source depends on.
