# MoneyGram Off-Ramp (Cash Pickup)

How a USDC loan becomes cash in a borrower's hand through MoneyGram (MG),
including the SEP-24 interactive flow, the poller that drives it, the treasury
send, refund settlement, and every config override on this path.

This doc favours plain explanation over exhaustive API detail. The code it
points at lives in this repo. The off-ramp defines a set of interfaces here
(status updates, completion recording, notifications); a host loan service
supplies the concrete implementations and owns the loan database.

---

## The short version

MoneyGram is a different shape from YellowCard. There is **no webhook** and no
synchronous payout: MG is a SEP-24 anchor, so the withdrawal is *interactive*
and the lifecycle is driven by a background **poller**.

1. A borrower asks to cash out at an agent. The loan service borrows USDC from
   the Vault into the treasury, then asks the off-ramp to start a withdrawal.
2. The MG adapter creates a SEP-24 interactive withdrawal and gets back a
   **webview URL**. That URL is SMS'd to the borrower — they open it, complete
   KYC, and pick the payout currency/amount.
3. Meanwhile the **poller** ([`pkg/services/mgpoller/poller.go`](../../pkg/services/mgpoller/poller.go))
   watches the MG transaction. When MG says `pending_user_transfer_start`, the
   poller sends the loan's USDC from the treasury to MG's anchor account.
4. MG pays out cash at the agent and the transaction moves to
   `pending_user_transfer_complete`, then `completed`. The poller records the
   locked payout + reference number, SMSes the borrower, and advances the loan.

Three files carry most of the weight:

- [`pkg/mobile/ussd/adapters/offramp_moneygram.go`](../../pkg/mobile/ussd/adapters/offramp_moneygram.go) — creating the SEP-24 withdrawal and returning the webview URL.
- [`pkg/services/mgpoller/poller.go`](../../pkg/services/mgpoller/poller.go) — polling MG and driving the loan state machine afterwards.
- [`pkg/payment/moneygram/`](../../pkg/payment/moneygram/) — the MG client: SEP-24 protocol (via the shared `stellaranchor` SDK), OAuth, and the FX rate cascade.

---

## Why a poller (not a webhook)

MG's SEP-24 anchor does not call us back. Status only moves when we ask, so the
integration plan makes polling the canonical driver. `mgpoller` runs a ticker
(default every 30s), fetches every active `ramp_provider="moneygram"` loan, and
reacts to the latest SEP-24 transaction status. See
[§ Poller lifecycle](#poller-lifecycle--what-each-status-does).

This is the biggest contrast with [yellowcard.md](./yellowcard.md): there, YC
pushes events to our webhook; here, we pull.

---

## The interactive (SEP-24) flow

Unlike a mobile-money push, an MG cash pickup can't be completed headless — the
borrower must open a webview to do KYC and confirm the payout. So `Initiate`
does *not* move money; it only **registers intent** and returns the URL.

`Initiate` ([`offramp_moneygram.go`](../../pkg/mobile/ussd/adapters/offramp_moneygram.go)):

1. Builds a SEP-9 customer prefill (name, phone, country) so the webview is
   partially filled.
2. Derives a **child-account memo** — a 64-bit memo from the user's child-account
   index — that scopes the custodial treasury wallet to this user
   (`stellaranchor.ChildAccountMemo`).
3. Posts a SEP-24 withdraw request to MG with the USDC `amount` and our funds
   account.
4. Returns the MG transaction ID + interactive URL on the `OffRampResult`.

The USSD layer persists the URL and the child-account index on the loan
(`ramp_interactive_url`, `ramp_child_account_index`) and SMSes the link. The
index is persisted so the poller can **re-derive the SEP-10 auth memo** after a
restart.

> **Custodial model:** a single Stellar treasury account holds funds for many
> users; the memo is what ties an on-chain payment back to one borrower.

---

## Poller lifecycle — what each status does

The poller maps each SEP-24 `Transaction.Status` to a loan-state change plus
side effects. The active set is everything not terminal; terminal statuses stop
the loan being polled further.

| MG status | What we do |
|---|---|
| `pending_user_transfer_start` | MG is ready for our crypto. **Send USDC** from treasury to MG's anchor account using the memo MG provided (see below). |
| `pending_user_transfer_complete` | Cash is collectable. Backfill `amount_out`, currency, and the cash-pickup reference; move the loan to `processing`; SMS the reference to the borrower. |
| `completed` | Terminal. Mark `completed`; notify disbursement complete. |
| `refunded` | Terminal-ish. Settle the refund (see [§ Refunds](#refunds--settling-a-refund)). |
| `expired` / `error` | Terminal. Mark `failed`; alert ops. |
| `on_hold` | MG is doing extra checks. Alert ops, change nothing. |
| `pending_trust` | Likely an anchor trustline problem. Alert ops. |
| `pending_user` / `pending_stellar` / `incomplete` | Transient. Log and wait for the next tick. |

A few things worth calling out:

- **The send is claimed before it's submitted.** In `pending_user_transfer_start`
  the poller first writes a durable "send attempt" claim (`RecordSendAttempt`)
  and only then calls `SendUSDC`. If the process dies between submission and
  recording the tx hash, the next tick sees the claim and refuses to pay twice —
  a duplicate treasury payment is worse than a stalled loan. The claim is only
  released (`ClearSendAttempt`) when the payment *definitively* did not move
  funds (e.g. an on-ledger `PAYMENT_UNDERFUNDED`), which is safe to retry.
- **Idempotency is local, not trusted to MG.** MG can leave
  `stellar_transaction_id` empty for minutes after we paid, so the poller's own
  `HasStellarSend` flag — not MG's echo — is the guard against re-sending.
- **Payout drift is alerted, not blocked.** At `pending_user_transfer_complete`,
  if MG's locked `amount_out` diverges from the KES the borrower typed at USSD
  entry by more than `PayoutDriftAlertPct` (default 2%), ops gets an alert. The
  payout still proceeds.

---

## The treasury send (and the 2-decimal rule)

When the poller sees `pending_user_transfer_start`, it sends the loan's
principal — in stroops — from the treasury to MG's anchor account.

**Amounts must be whole USDC cents.** MG's cash-out methods reconcile at 2
decimal places, but USDC on Stellar has 7 (stroops). Sending the raw
7-decimal FX conversion (e.g. `23.430178` when MG expects `23.43`) leaves the
withdrawal stuck — MG's expected amount never matches what arrived. So every
cash-out principal is rounded to a whole cent (round-half-up) at loan-request
time, *before* it's stored, borrowed, or sent — keeping the stored principal,
the vault borrow, the on-chain send, and MG's expected amount identical. The
on-chain transfer also renders the amount without trailing zeros (`23.43`, not
`23.4300000`) so automated string checks on MG's side don't flag it.

> See the loan adapter's `roundToCentStroops` (credit module) and
> `SendUSDC` in [`pkg/stellar/classic/classic.go`](../../pkg/stellar/classic/classic.go).

---

## Refunds — settling a refund

If MG refunds a withdrawal (borrower never collected, or MG cancelled), the USDC
comes back on-chain and must be returned to the Vault. The poller:

1. On `refunded`, marks the loan `refund_pending` and waits for MG to publish
   the refund's Stellar payment details.
2. **Verifies on-ledger** — it does not trust MG's reported amounts. A
   `PaymentVerifier` sums the actual USDC payments that landed back in our
   wallet (`NetStroops`).
3. Compares against the principal we sent:
   - **Equal** — repay the Vault in full.
   - **Shortfall** (`ShortfallStroops > 0`) — repay what came back; treasury
     absorbs the difference; alert ops for manual settlement.
   - **Excess** — repay the principal; the excess stays in the funds wallet for
     an admin to settle; alert ops.
4. Marks the loan `refund_received` and stops.

If MG reports `refunded` but never publishes payment details after
`RefundSettleMaxAttempts` (default 20) polls, ops is alerted to settle manually —
the vault is not repaid against a refund we can't confirm landed.

---

## FX rates — the cascade

MG cash pickup needs a USD→local rate to quote the borrower's payout. Rates come
from a cascading orchestrator ([`pkg/payment/moneygram/fx.go`](../../pkg/payment/moneygram/fx.go)):

1. **Primary** — MG's own FX Rate REST API (OAuth client-credentials), with a
   safety buffer deducted (`MONEYGRAM_FX_ENTRY_BUFFER_PCT`, default 1%) to hedge
   drift between USSD entry and webview confirmation.
2. **Fallback** — YellowCard's rate, with a larger buffer
   (`MONEYGRAM_FX_ENTRY_BUFFER_PCT_FALLBACK`, default 1.5%).
3. **Stale cache** — the last-known-good rate, if it's younger than
   `StaleCacheMaxAge` (default 24h). The source label is preserved but marked
   `cached_*` so downstream drift detection knows it's stale.

If all three fail, `ErrNoRateAvailable` — the loan request is rejected. The
orchestrator also enforces the corridor amount bounds (min/max USD).

---

## Config & overrides on this path

Everything tunable on the MG off-ramp. All live under `MoneyGramConfig`
([`pkg/config/config.go`](../../pkg/config/config.go)) from `MONEYGRAM_*` env vars.

### Required at boot

| Setting | Notes |
|---|---|
| `MONEYGRAM_HOME_DOMAIN` | MG's anchor home domain (used to resolve the SEP-1 TOML) |
| `MONEYGRAM_SERVER_SIGNING_KEY` | MG's anchor signing key, pinned & validated against the TOML |
| `MONEYGRAM_USDC_ISSUER` (or `USDC_ISSUER`) | The USDC asset issuer |
| `MONEYGRAM_AUTH_SECRET` (or `TREASURY_SECRET_KEY`) | Signs SEP-10 auth; must be a valid Stellar secret |
| `MONEYGRAM_FUNDS_SECRET` (or `TREASURY_SECRET_KEY`) | The funds wallet — withdrawal source |

### Optional / cadence

| Setting | Default | Notes |
|---|---|---|
| `MONEYGRAM_TRANSFER_SERVER_URL` | resolved from TOML | Override the SEP-24 transfer server |
| `MONEYGRAM_CLIENT_ID` / `MONEYGRAM_CLIENT_SECRET` | — | OAuth for the FX Rate REST API |
| `MONEYGRAM_OAUTH_URL` / `MONEYGRAM_FX_RATE_URL` | MG defaults | Override REST endpoints |
| `MONEYGRAM_FX_ENTRY_BUFFER_PCT` | 1% | Buffer on the primary (MG) rate |
| `MONEYGRAM_FX_ENTRY_BUFFER_PCT_FALLBACK` | 1.5% | Buffer on the fallback (YC) rate |
| `MONEYGRAM_POLL_INTERVAL` | 30s | Poller tick |
| `MONEYGRAM_POLL_MAX_BATCH` | 100 | Max loans evaluated per tick |
| `MONEYGRAM_REFUND_MAX_ATTEMPTS` | 20 | Polls to wait for refund details before alerting |

### Amount corridor

The provider advertises a fixed cash-pickup corridor — `MinWithdrawUSD` /
`MaxWithdrawUSD` in [`pkg/payment/moneygram/corridor.go`](../../pkg/payment/moneygram/corridor.go) (15 / 2500 USD). The FX orchestrator's own min/max caps are a
separate, looser bound used for rate validation.

---

## Failure modes & ops playbook

| Symptom | What it means | Action |
|---|---|---|
| `MoneyGram USDC send outcome UNKNOWN` alert | `SendUSDC` errored ambiguously; payment may have landed | **Manual** — verify on-chain before clearing `ramp_stellar_tx_hash`; clearing allows a re-send |
| `MoneyGram USDC send failed on ledger` | Payment failed on-ledger (e.g. underfunded), no funds moved | Safe — claim released, poller retries once the cause is fixed |
| Loan stuck at `pending_user_transfer_start` with `has_stellar_send=true` | We sent, but MG hasn't observed it | Wait; if it persists, verify the tx on-chain and with MG |
| `PAYOUT DRIFT` alert | MG's locked payout diverges >2% from what the borrower saw | Investigate FX drift between entry and webview confirmation |
| `MoneyGram transaction on hold` / `pending_trust` | MG compliance hold / anchor trustline issue | Coordinate with MG; check anchor trustline |
| `MoneyGram refund shortfall / excess` alert | MG returned less/more than we sent | Shortfall: treasury absorbed it, settle manually. Excess: held in funds wallet for admin settlement |
| `MoneyGram refund has no settlement details` | `refunded` but no payment details after max attempts | **Manual** — verify on-chain, settle, and repay the vault by hand |
| `ErrNoRateAvailable` | All FX sources (MG, YC, stale cache) failed | Reject the loan; restore a rate source |

---

## Related

- [Off-ramp overview](./README.md) — shared conventions and how MG differs from
  the webhook-driven YellowCard flow.
- [yellowcard.md](./yellowcard.md) — the mobile-money off-ramp: two settlement
  modes, the direct→fiat pivot, and the webhook state machine.
- [Soroban / Vault](../soroban/vault.md) — the borrow and repay calls the
  off-ramp triggers on-chain.
