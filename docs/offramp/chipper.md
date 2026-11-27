# Chipper (Mobile Money, Stellar-Funded)

How a loan is disbursed through Chipper, how its outcome is settled, and what
keeps the Chipper balance honest.

Chipper competes with YellowCard on the mobile-money rail. Which of them handles
a loan is decided by the [relay](../relay/README.md) **before the borrow**, and
the loan is pinned to the winner. Chipper is optional: without
`CHIPPER_API_KEY` nothing below is wired.

Code: [`pkg/payment/chipper`](../../pkg/payment/chipper) (client),
[`offramp_chipper.go`](../../pkg/mobile/ussd/adapters/offramp_chipper.go) (the
`offramp.Provider` adapter),
[`pkg/webhook/chipper.go`](../../pkg/webhook/chipper.go) (settlement),
[`pkg/services/chippermon`](../../pkg/services/chippermon) (float alert and fee
reconciliation).

---

## The API

| Environment | Base URL | Key prefix |
|---|---|---|
| Sandbox | `https://sandbox-api.platform.chipper.ai` | `sk_test_` |
| Production | `https://api.platform.chipper.ai` | `sk_live_` |

`Authorization: Bearer sk_…` on every call, with `chipper-version: 2026-02-20`
pinned. There is no request signing. The client refuses a key used against the
other stack's URL, and any non-https base URL except loopback.

Amounts come back as decimal strings and are sent as JSON numbers; the client
uses exact decimals throughout and rejects a payout carrying more decimals than
the currency allows (KES 2, USDC 6, UGX/TZS/RWF 0) before calling Chipper.

**Production payouts answer `403 forbidden` until Chipper enables them for the
organization.** The same code covers a missing scope or a blocked IP.

---

## The disbursement flow

Direct settlement, the default. It mirrors YellowCard: the vault lends only the
borrower's request, and Chipper's fee comes out of our Chipper balance, not the
payout.

1. **Resolve the payout method.** `CHIPPER_METHOD_CODES` maps the borrower's
   mobile money network code (`offramp.Request.NetworkCode`, e.g. `MPESA`) to a
   Chipper method (`ke_mpesa`). The method must be listed in capabilities and
   not in `outage`.
2. **Skip if already paid.** A payout that already exists for the reference is
   returned as is; nothing is sent twice.
3. **Deposit.** The treasury sends the net payout (principal less origination
   fee) to the pinned Stellar deposit address with the pinned **memo ID**.
4. **Wait for the credit.** Every Chipper crypto deposit records its Stellar
   transaction hash (`details.hash`), so the adapter matches *our* deposit
   exactly, polling every 3 s for up to 90 s. Concurrent loans cannot claim each
   other's deposits.
5. **Pay out** from the USDC balance: `from.amount` is the deposited USDC,
   `to` is the borrower's number in digits. Chipper debits amount + fee.

### The rule that shapes the flow

**Once the treasury send succeeds, `Initiate` never returns an error.** The loan
service reads an error as "the USDC never left the treasury" and repays the
vault immediately; after the send that would be false. So a deposit that is not
credited in time, or a payout Chipper rejects, returns `Status: payout_pending`
with `OffRampPayload.PayoutPending` and a reason, and raises an alert.
`ReleasePayout` finishes it later, from a webhook or the reconciler, paying the
deposit's own on-chain amount.

### Deposit target pinning

`CHIPPER_DEPOSIT_ADDRESS` and `CHIPPER_DEPOSIT_MEMO` are copied from the
dashboard. The adapter compares them with the address the API returns (Chipper
keeps one per currency and chain per organization) before any deposit. A
mismatch stops the credit service at boot; Chipper being unreachable at boot
only warns, and the check repeats before the first deposit. A wrong address
would send treasury funds somewhere unrecoverable.

### Fallbacks

| Failure | Where the USDC is | What happens |
|---|---|---|
| Treasury send fails | Treasury | Payout from the Chipper balance if it holds amount × 1.02, marked `fiat` so the loan service repays the vault; otherwise an error |
| Deposit not credited in 90 s | On its way to Chipper | `payout_pending`, alert, released when the deposit is credited |
| Payout rejected after the credit | Chipper balance | `payout_pending` with Chipper's error code, alert, released later |
| Payout create times out or 5xx | Unknown | Read back by reference before deciding it failed |

Fiat mode (`chipper.Options{SettlementMethod: fiat}`) skips the deposit and pays
from the balance directly.

---

## Settlement

`POST /api/v1/webhooks/chipper`, Standard Webhooks: HMAC-SHA256 over
`{webhook-id}.{webhook-timestamp}.{raw body}` with the base64 bytes after
`whsec_`, five-minute timestamp tolerance, any of several `v1,` signatures may
match. Register it in the dashboard for `payout.completed`, `payout.failed`,
`account.credited` and `collection.completed`.

| Event | Action |
|---|---|
| `payout.completed` | Loan complete; delivered amount and Chipper's fee recorded, the fee as the service fee the borrower repays; borrower notified. No vault repay: the treasury deposited the USDC |
| `payout.failed`, direct, first failure | Repeated once from the Chipper balance under `<reference>_fiat` (the loan lookup resolves the suffix) to the same number and amount; ops alerted |
| `payout.failed` on the repeat, or on a balance-funded payout | Loan off-ramp failed, vault repaid from treasury, borrower notified, ops alerted. On direct, the deposit stays on the Chipper balance as float |
| `collection.completed` / `account.credited` for a crypto deposit | Releases the pending payout whose deposit hash matches |

Terminal outcomes skip a loan that is already settled, so a repeated delivery
never texts the borrower twice. Chipper retries for about a day and disables an
endpoint after 10 consecutive failures.

**The reconciler** (`webhook.ChipperReconciler`, every 30 s) is the backstop: it
releases pending payouts and re-reads payouts with no outcome after 10 minutes,
applying them through the same handlers.

---

## Fees and rates

Chipper publishes no fee schedule; fees are set per organization and rail. Seen
on the sandbox: **1.5% on deposits and 1.5% on payouts**, plus a 1.5–2.5% FX
spread already inside the rate.

- `GET /v1/rates/USDC/KES` is all-in and is the rate payouts execute at, so it
  is the source of truth for loans Chipper disburses. YellowCard is only the
  fallback.
- `CHIPPER_FEE_SCHEDULE` estimates both fees for **routing only**. The fee
  recorded on a loan always comes from the payout.
- The float covers both fees: depositing X credits about 0.985X, a payout of X
  debits about 1.015X, so each loan draws about 3% of its USDC from the float.

The relay source prices Chipper as
`(amount − deposit fee − payout fee) × rate / amount`, the convention every
source shares. It refuses to start if a mapped method has no fee rule.

---

## Monitoring

| Worker | Interval | Alerts |
|---|---|---|
| Float monitor | 5 min | `Chipper float low` when the USDC balance drops below `CHIPPER_FLOAT_ALERT_USDC`, every 6 h while low; `Chipper float recovered` once back above. Off when unset |
| Fee reconciler | `CHIPPER_FEE_RECONCILE_INTERVAL` (default 24 h) | `Chipper fee mismatch` listing every settled payout whose recorded fee differs from Chipper's `fee_debit` ledger entries by more than a cent |

Deposit fees are not reconciled: they are never recorded on a loan.

---

## Configuration

| Variable | Notes |
|---|---|
| `CHIPPER_API_KEY` | Enables Chipper. Once set, the variables below are validated as a set |
| `CHIPPER_BASE_URL` | Defaults to sandbox; required in production |
| `CHIPPER_WEBHOOK_SECRET` | `whsec_…` of the registered endpoint |
| `CHIPPER_DEPOSIT_ADDRESS` / `CHIPPER_DEPOSIT_MEMO` | Dashboard deposit target, per environment. The memo must be numeric |
| `CHIPPER_METHOD_CODES` | e.g. `MPESA=ke_mpesa` |
| `CHIPPER_FEE_SCHEDULE` | e.g. `{"deposit":{"pct":"1.5"},"payouts":{"ke_mpesa":{"pct":"1.5"}}}`; `flat`, `min` and `max` are optional, in USDC |
| `CHIPPER_FLOAT_ALERT_USDC` | Float alert threshold; empty is off |
| `CHIPPER_FEE_RECONCILE_INTERVAL` | Seconds |
| `CHIPPER_TEST_DESTINATION_PHONE_OVERRIDE` | Sandbox only; ignored in production |
| `ENABLE_PAYMENT_PROVIDER_RELAY_SWITCH` | Whether Chipper ever wins a loan. See [relay](../relay/README.md) |

---

## Sandbox

Read-only checks against the real sandbox, run by hand:

```sh
CHIPPER_SANDBOX_KEY=sk_test_… go test -tags chipper_sandbox -run Sandbox -v ./pkg/payment/chipper/...
```

They confirm `ke_mpesa` and Stellar USDC deposits are offered and print the
rate, balances and recent fee ledger entries. The sandbox runs on the real
Stellar testnet with Circle USDC.

---

## Failure modes & ops playbook

| Symptom | Meaning | Action |
|---|---|---|
| Boot stops: deposit target does not match | Pinned address or memo differs from the API | Copy both from the dashboard for this environment |
| `Chipper deposit not credited` | USDC left the treasury; no credited deposit within 90 s | Usually resolves when Chipper credits it; the payout is released automatically. Check the Stellar transaction and the memo |
| `Chipper payout not created` | USDC is on the Chipper balance; the payout was rejected | Check the reason in the alert. `insufficient_funds` means the float is empty: top it up and the reconciler releases it |
| `Chipper float low` | Payout fees will soon fail | Top up the Chipper USDC balance from the treasury |
| `Chipper Disbursement Failed` | A payout failed twice; the vault was repaid | The deposit is float on the Chipper balance; check the destination number |
| `Chipper fee mismatch` | A recorded service fee differs from Chipper's ledger | The borrower repays the recorded figure; correct the loan if Chipper's is right |
| `403 forbidden` in production | Payouts not enabled, missing scope or blocked IP | Ask Chipper support; check key scopes and the IP allowlist |

---

## Related

- [Off-ramp overview](./README.md), shared conventions and provider selection.
- [Payment Relay](../relay/README.md), route-first quoting and the Chipper rate
  source.
- [yellowcard.md](./yellowcard.md): the provider it competes with.
