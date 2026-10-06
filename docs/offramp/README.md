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
| YellowCard | [yellowcard.md](./yellowcard.md) | Mobile money across multiple African countries. The default. |
| Fonbnk | [fonbnk.md](./fonbnk.md) | Mobile money, competing with YellowCard on price |
| MoneyGram | [moneygram.md](./moneygram.md) | SEP-24 anchor cash pickup at an agent |

All three plug into the same internal `offramp.Provider` interface, so the loan
service treats them uniformly. The docs here describe each provider's own quirks.

## Which provider handles a payout

Two decisions, in order.

**The borrower picks the rail.** Mobile money or cash pickup, at the USSD menu.
That is a rail choice, not a price one, and nothing overrides it.

**The relay picks the provider within mobile money.** When
`ENABLE_PAYMENT_PROVIDER_RELAY_SWITCH` is on, YellowCard and Fonbnk are quoted
concurrently and the better post-fee rate wins. Off, everything goes to
YellowCard. See [Payment Relay](../relay/README.md).

Cash pickup always resolves to MoneyGram.

## The big ideas

If you read nothing else, understand these. Note they are framed around
YellowCard's mobile-money rail; MoneyGram's cash-pickup rail is a different
shape (interactive SEP-24, driven by a poller rather than webhooks), see
[moneygram.md](./moneygram.md).

1. **Two ways to settle (YellowCard).** A mobile-money payout can be funded two
   ways, **direct** (we send the provider crypto for this specific payout) or
   **fiat** (the provider pays out of a balance we pre-funded earlier, and we
   keep the crypto). The system tries direct first and automatically falls back
   to fiat when direct can't go through. See
   [yellowcard.md § Settlement modes](./yellowcard.md#settlement-modes).

2. **Status drives the state machine.** We submit a payout and then track its
   lifecycle to complete / failed / refunded, nudging the loan's
   `disbursement_status` and triggering side effects, notify the borrower,
   repay the Vault, alert ops, or kick off a retry. YellowCard **pushes** events
   to our webhook (see
   [yellowcard.md § Webhook events](./yellowcard.md#webhook-events--what-each-one-does));
   MoneyGram does not publish webhooks, so a background **poller** pulls status
   instead (see
   [moneygram.md § Poller lifecycle](./moneygram.md#poller-lifecycle--what-each-status-does)).

3. **Unwound payouts return the USDC to the Vault, once.** When a payout fails
   and the crypto comes back, or never left, the borrowed USDC is repaid to
   the Vault. Every path goes through one entry point in the credit module
   (`DisbursementStatusAdapter.repayVault`, or `RepayVaultForLoan` for the
   off-ramp initiate path), which:

   - **claims** the repay with a compare-and-set on `vault_repay_status`
     (`pending`), so two callers cannot both send it;
   - **records the signed hash and `validUntil` before submitting**, so a
     submit that never reports back can be looked up instead of resent;
   - **caps** attempts at `VAULT_REPAY_MAX_ATTEMPTS` (default 5).

   A submit with no definitive answer becomes `unknown` and keeps its hash; it
   is never resent blindly. The **vault repay reconciler** (credit backend,
   every `VAULT_REPAY_RECONCILE_INTERVAL`, default 10 minutes, under a Postgres
   advisory lock) takes a loan over once `VAULT_REPAY_SETTLEMENT_WINDOW`
   (default 1 hour) has passed:

   - a recorded hash is settled from the ledger (`ResolveSubmitted`: succeeded,
     failed, never landed, or outside RPC retention);
   - a stale claim with no hash is parked as `unknown`;
   - a `failed` repay is retried every `VAULT_REPAY_RETRY_BACKOFF` (default
     1 hour).

   Ops alerts on this path include `Vault repay attempts exhausted`,
   `Vault repay outcome unknown`, `Vault repay outcome unresolvable` and
   `Vault repay claim stale`, all on the page tier. This covers only the
   treasury→Vault leg of an unwound disbursement. Borrower repayments,
   including those settled through the OTC desk, never reach it.

## Conventions used across these docs

- **Money is stored as whole minor units** (cents for fiat, stroops for USDC).
  `1 KES = 100` in the DB; `1 USDC = 10_000_000` stroops. We never store money
  as a floating-point number, floats drift, integers don't.
- **"Local currency"** means the borrower's currency (KES in the examples).
  **"USD"** is the loan's accounting currency. The provider quotes a rate between
  the two.
- **Code references** are clickable links to the file (the function name is
  given alongside, since links resolve to the file, not the line). All
  referenced code lives in this repo: the provider adapters, webhook handling,
  and pollers. The off-ramp exposes interfaces that a host loan
  service implements. That service owns the loan database and lifecycle and is
  out of scope here.
