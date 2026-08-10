# Microvault Off-Ramp

Developer reference for the **off-ramp**: the path that turns a borrower's USDC
loan into real money in their mobile-money wallet.

On-chain, a loan borrows USDC from the Vault into the treasury (see
[Soroban docs](../soroban/README.md)). The off-ramp is everything that happens
*after* that: handing the USDC to a payment provider, having them pay out local
currency (e.g. KES) to a phone number, and reacting to whether that payout
succeeds, fails, or has to be retried a different way.

## Providers

| Provider | Doc | Rail |
|---|---|---|
| YellowCard | [yellowcard.md](./yellowcard.md) | Mobile money across multiple African countries |
| MoneyGram | _(planned doc)_ | SEP-24 anchor withdrawal |

Both providers plug into the same internal `offramp.Provider` interface, so the
loan service treats them uniformly. The docs here describe each provider's own
quirks.

## The two big ideas

If you read nothing else, understand these two:

1. **Two ways to settle.** A payout can be funded two ways — **direct** (we send
   the provider crypto for this specific payout) or **fiat** (the provider pays
   out of a balance we pre-funded earlier, and we keep the crypto). The system
   tries direct first and automatically falls back to fiat when direct can't go
   through. See [yellowcard.md § Settlement modes](./yellowcard.md#settlement-modes).

2. **Webhooks drive the state machine.** We submit a payout and then *wait*. The
   provider calls our webhook as the payout moves through its lifecycle
   (processing to complete / failed / refunded). Each event nudges the loan's
   `disbursement_status` and may trigger a side effect — notify the borrower,
   repay the Vault, alert ops, or kick off a retry. See
   [yellowcard.md § Webhook events](./yellowcard.md#webhook-events--what-each-one-does).

## Conventions used across these docs

- **Money is stored as whole minor units** (cents for fiat, stroops for USDC).
  `1 KES = 100` in the DB; `1 USDC = 10_000_000` stroops. We never store money
  as a floating-point number — floats drift, integers don't.
- **"Local currency"** means the borrower's currency (KES in the examples).
  **"USD"** is the loan's accounting currency. The provider quotes a rate between
  the two.
- **Code references** are clickable links to the file (the function name is
  given alongside, since links resolve to the file, not the line). All
  referenced code lives in this repo: the provider adapters, webhook handling,
  and pollers. The off-ramp exposes interfaces that a host loan
  service implements — that service owns the loan database and lifecycle and is
  out of scope here.
