# Fonbnk (Mobile Money, Both Directions)

How a loan is disbursed through Fonbnk, and what its API demands that no other
provider here does.

Fonbnk competes with YellowCard on the mobile-money rail; which of them handles
a given payout is decided by the [relay](../relay/README.md). One client covers
both directions, so the on-ramp side is documented here too even though nothing
consumes it yet.

Code: [`pkg/payment/fonbnk`](../../pkg/payment/fonbnk) (client),
[`offramp_fonbnk.go`](../../pkg/mobile/ussd/adapters/offramp_fonbnk.go) (the
`offramp.Provider` adapter).

---

## Blocker: the create-users permission

`POST /api/v2/order` and everything downstream of it, confirm, cancel,
intermediate-action, answer:

```
403 This feature is not available for this merchant, please contact support
```

until Fonbnk enables the **create-users** permission on the merchant account.
Quoting and discovery are **not** gated, so nothing that stops short of a real
order reveals this. Sandbox and production are separate accounts and need it
separately.

The client maps that 403 to its own code, `merchant_not_permitted`, with a hint
naming the fix, so it is distinguishable from an ordinary auth failure.

**Today: quoting and relay routing work. Disbursement does not.**

---

## Which API

Fonbnk publishes two generations and its docs site serves both. This integration
targets **server-to-server v2** under `/api/v2`. The older v1.5 surface is
on-ramp only. v2 models both directions with one symmetric deposit/payout order.

| Environment | Base URL |
|---|---|
| Sandbox | `https://sandbox-api.fonbnk.com` |
| Production | `https://api.fonbnk.com` |

Separate systems, separate credentials, separate orders. Nothing crosses.

---

## The disbursement flow

An off-ramp is a **crypto deposit** (our treasury pays in) and a **fiat payout**
(the borrower receives). Ordering is the safety property:

1. **Create the order**, optionally from a quote the relay already locked, so the
   borrower is disbursed at the rate that was compared.
2. **Read the deposit target** from `transferInstructions.transferDetails`, the
   recipient wallet address, and a memo when the payout wallet needs one.
3. **Send USDC** from the treasury to that address.
4. **Confirm the deposit** with the on-chain transaction hash.

| Step fails | State | Behaviour |
|---|---|---|
| Order creation | Nothing moved | Error; zero send calls |
| No deposit address on the order | Nothing moved | Error; order cancelled |
| Treasury send | Nothing moved | Error; order cancelled |
| Confirm | **USDC already gone** | **Succeeds**, payload flagged `Confirmed: false` |

That last row is deliberate. Once the treasury has paid, the disbursement has
happened, and Fonbnk detects incoming payments on its own, confirm only
accelerates settlement. Returning an error there would throw away the order
reference for a step that was never load-bearing. It logs `CRITICAL` and flags
the payload rather than hiding it.

---

## Order lifecycle

Deposit, then payout, then refund if the payout cannot be delivered.

| Phase | Status | Meaning |
|---|---|---|
| Deposit | `deposit_awaiting` | Waiting for the payer. Show transfer instructions. |
| | `deposit_validating` | Confirmed, or a provider reported an incoming payment. |
| | `deposit_successful` | Checked out; the payout starts. |
| | `deposit_invalid` | Wrong amount, narration or sending account. |
| | `deposit_canceled` | Cancelled before payment. |
| | `deposit_expired` | Not paid by `expiresAt`, or an intermediate action was retried past its cap. |
| Payout | `payout_pending` | Funds going out. |
| | `payout_successful` | Delivered. **Terminal.** |
| | `payout_failed` | Not terminal. A retry re-enters `payout_pending`. |
| Refund | `refund_initiated` / `refund_pending` | Refund decided / being sent. |
| | `refund_successful` | **Terminal.** |
| | `refund_failed` | Not terminal. A retry re-enters `refund_initiated`. |

### The trap

**`deposit_canceled` and `deposit_expired` are not endings.** If the payer's
money turns up late, Fonbnk still accepts it: the order moves to
`deposit_successful` and the payout runs. `deposit_invalid` can likewise be
walked back by Fonbnk support.

A consumer must not release funds, reverse a ledger entry or close a loan on
those alone. Only `payout_successful` and `refund_successful` are final, which
is exactly what `fonbnk.IsTerminal` encodes.

**Statuses can also be skipped.** Branch on the status received, never on the
one expected next.

---

## Rates

`Cashout.RateAfterFees` (`exchangeRateAfterFees` on the wire) is
`amountBeforeFees / amountAfterFeesUsd`. A pre-fee local amount over a post-fee
USD one. It is a per-leg diagnostic, not a price, and it moves opposite to the
user's outcome. `Quote.EffectiveRate` is the figure to compare: what actually
leaves us over what actually arrives.

**The amount rides the crypto leg** in both directions, because that is the side
we always know. A loan is denominated in USDC, not shillings. One quote call
therefore prices the corridor at the size actually being moved, with no probe
round trip.

`FeeSetting.Max` is a `MaxBound` rather than a `float64`: Fonbnk sends the
string `"Infinity"` for the top band of every fee table, and a plain float fails
to decode any complete fee schedule.

---

## On-ramp and the STK push

The on-ramp direction (KES in, USDC out) is implemented in the client but not
wired. Two things to know before it is:

**Payout goes to the treasury with a per-user `blockchainMemo`**, not to a
borrower's own Stellar account. That removes the requirement that every borrower
hold a USDC trustline, and reuses the memo-matching machinery the MoneyGram
repayment rail already has.

**The intermediate-action attempt cap is destructive.** On a mobile-money
deposit the payer gets a carrier prompt; `TriggerIntermediateAction` sends a
fresh one or submits the OTP that unlocks it. Calling it once
`intermediateActionAttempts` has reached `intermediateActionMaxAttempts` does
**not** fail harmlessly. It moves the order to `deposit_expired` and then
returns `400 The order is expired`. An ordinary retry-until-it-works loop
destroys the order. Gate every call on
`TransferInstructions.CanRetryIntermediateAction`.

---

## Webhooks

Fonbnk POSTs on every status change. Its signature is a **nested plain SHA-256,
not an HMAC**. A third distinct scheme in this codebase:

```
hex( SHA256( rawBody || hex( SHA256(secret) ) ) )
```

Two failure modes worth a test each: the order is body-then-secret-hash (reversed
never matches), and the **raw bytes** must be hashed, parsing the JSON and
re-serialising changes the hash, so the handler must capture the body before any
middleware parses it.

| Requirement | Value |
|---|---|
| Response | 2xx within 20 seconds |
| Redirects | **Not followed.** A 301/302 is a failed delivery, register the final `https` URL. |
| Retries | 10 attempts, exponential backoff from 1s |
| Idempotency | Required, deliveries repeat and can arrive out of order |

The payload is a **summary**: rates and amounts but no fee breakdown, no
transfer instructions, no status history. Match on `_id` or
`merchantOrderParams`, then call `GetOrder` for the rest.

---

## Configuration

| Variable | Notes |
|---|---|
| `FONBNK_CLIENT_ID` / `FONBNK_CLIENT_SECRET` | Per environment. Absent means Fonbnk is not registered at all, boot logs it and carries on. |
| `FONBNK_BASE_URL` | Sandbox or production host |
| `ENABLE_PAYMENT_PROVIDER_RELAY_SWITCH` | Whether Fonbnk ever wins a payout. See [relay](../relay/README.md). |

**Never hard-code a contract address, bank name or carrier list.** Sandbox
values are fixtures and differ from production. Read them from
`GetAvailableCurrencies` or from the quote.

`GET /api/v2/order-limits` answers `200` with every field zero when a corridor
cannot be traded, which is why the client turns that into
`corridor_unavailable` rather than reporting a limit of zero.

---

## Sandbox

`fieldsToCreateOrder` carries two extra fields in sandbox only,
`depositSandboxForcedFlow` and `payoutSandboxForcedFlow`, forcing a deposit
success, invalid, underpayment (50%) or overpayment (200%), and a payout success
or failure. Which values a leg accepts varies, so read the field's own `options`
array. Neither appears in production.

Those six outcomes are the integration test matrix. **Underpayment and
overpayment have no analogue in the YellowCard integration and no defined
handling in our settlement path yet.**

---

## Failure modes & ops playbook

| Symptom | Meaning | Action |
|---|---|---|
| `merchant_not_permitted` | create-users not enabled on the account | Contact Fonbnk support; nothing local will fix it |
| `CRITICAL: USDC sent to fonbnk but the deposit could not be confirmed` | Funds left the treasury, confirm call failed | Usually self-heals. Fonbnk detects the payment. Verify the order reached `payout_successful`. |
| `corridor_unavailable` | No provider can price the pair right now | Transient; re-read available currencies |
| Order stuck at `deposit_awaiting` | Treasury send never landed | Check the on-chain transaction; the order expires on its own |
| `401 Signature is invalid` | The query string signed differs from the one sent | A re-serialised query, build it once with `url.Values.Encode()` |

---

## Related

- [Off-ramp overview](./README.md), shared conventions and provider selection.
- [Payment Relay](../relay/README.md), how Fonbnk is chosen over YellowCard.
- [yellowcard.md](./yellowcard.md): the provider it competes with.
