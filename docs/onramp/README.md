# Microvault On-Ramp

Developer reference for the **on-ramp**: the path that turns real money into
USDC held by the platform.

It is the mirror of the [off-ramp](../offramp/README.md). Where that path takes
a borrower's USDC loan and pays out local currency, this one takes local
currency — cash at a counter, or a mobile-money balance — and credits USDC to
the treasury. The dominant consumer today is **loan repayment**: a borrower
settling their debt without ever holding crypto.

## Providers

| Provider | Doc | Rail | Status |
|---|---|---|---|
| MoneyGram | [moneygram.md](./moneygram.md) | SEP-24 anchor cash-in at an agent | Implemented |
| YellowCard | — | Mobile-money collections | Not yet implemented |
| Fonbnk | — | Mobile-money STK push | Not yet implemented |

## The big ideas

1. **The borrower drives the clock, not us.** An off-ramp is something we
   started and the provider must finish; it resolves in minutes. An on-ramp is
   something the borrower must finish and nothing on our side compels them to.
   Windows are measured in hours or days, and most of that time nothing happens.
   Cadence therefore belongs in a per-row database column, not a ticker — see
   [moneygram.md § Why a deposit is not a withdrawal backwards](./moneygram.md#why-a-deposit-is-not-a-withdrawal-backwards).

2. **One treasury address, many borrowers.** Inbound funds all land on the same
   Stellar account, so a **memo** is what attributes a payment to a loan. Child
   accounts hold no USDC trustline — adding one per borrower would cost a
   sponsored reserve for an account that only ever passes funds through.

3. **The provider's deadline wins.** Whatever window we configure, the provider
   states its own and that is the one that binds. Acting on ours keeps dead
   transactions alive and tells borrowers they have time they do not.

4. **Notify on arrival, settle afterwards.** The moment funds reach the
   treasury, the borrower is done and is told so. Everything after that —
   repaying the Vault, retrying a failed on-chain leg — is our problem and not
   something they should hear about.

## Conventions used across these docs

- **Money is stored as whole minor units** (cents for fiat, stroops for USDC).
  `1 KES = 100` in the DB; `1 USDC = 10_000_000` stroops. We never store money
  as a floating-point number — floats drift, integers don't.
- **Send markers are written before the send, not after.** A failing SMS
  provider must not be retried on every tick for the rest of a multi-day window.
  The cost is that a failed send is terminal until a human clears the marker,
  which is why those failures raise an ops alert naming the column to clear.
- **Code references** are clickable links to the file (the function name is
  given alongside, since links resolve to the file, not the line). Some links
  point into the sibling credit module, which owns the loan database.
