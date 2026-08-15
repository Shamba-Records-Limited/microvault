# YellowCard Off-Ramp

How a USDC loan becomes mobile money in a borrower's wallet through YellowCard
(YC), including the two settlement modes, the direct to fiat pivot, the webhook
state machine, fee capture, and every config override on this path.

This doc favours plain explanation over exhaustive API detail. The code it points
at lives in this repo. The off-ramp defines a set of interfaces here (status
updates, completion recording, notifications); a host loan service supplies the
concrete implementations and owns the loan database.

---

## The short version

1. A borrower asks to cash out. The loan service borrows USDC from the Vault into
   the treasury, then asks the off-ramp to pay out.
2. The YC adapter resolves which mobile-money **channel** and **network** to use,
   builds a payment request, and submits it to YC.
3. It tries **direct settlement** first (send YC the crypto for this payout). If
   anything about direct fails, it **falls back to fiat** (let YC pay from a
   balance we pre-funded, and keep our crypto).
4. From there we wait. YC calls our **webhook** as the payout progresses. Those
   events move the loan's `disbursement_status` and trigger side effects:
   notify the borrower, repay the Vault, record what was actually delivered and
   what fees were charged, or — if direct failed mid-flight — start a refund-and-
   retry cycle.

Two files carry most of the weight:

- [`pkg/mobile/ussd/adapters/offramp_yellowcard.go`](../../pkg/mobile/ussd/adapters/offramp_yellowcard.go) — submitting the payout and
  doing the direct to fiat fallback at submission time.
- [`pkg/webhook/webhook_service.go`](../../pkg/webhook/webhook_service.go) — reacting to YC's webhook events afterwards.

---

## Settlement modes

A payout has to be *funded* somehow. YC supports two ways, and we use both.

### Direct settlement (the default)

We tell YC "pay this person, and here's the crypto to cover it." Concretely
(`tryDirectSettlement`, [`offramp_yellowcard.go`](../../pkg/mobile/ussd/adapters/offramp_yellowcard.go)):

1. Submit the payment to YC with `directSettlement=true`. YC replies with a
   one-time Stellar wallet address to send USDC to.
2. Check that address actually has a USDC trustline (if it doesn't, sending would
   burn the transaction — so we stop here and fall back instead).
3. Send USDC from the treasury to that address on-chain.

The crypto leaves our treasury. Because the money is now with YC, **we do not
repay the Vault on a direct payout** — the loan stays borrowed until the borrower
repays it later.

### Fiat settlement

We tell YC "pay this person out of the balance we already have with you." We
keep our crypto. Concretely (`tryFiatDisbursement`):

1. Check our YC balance is big enough for this payout (if not, stop with an
   `InsufficientBalanceError`).
2. Submit the payment with `forceAccept=true` and no crypto attached.

Here the crypto never left the treasury, so **once the payout completes we repay
the Vault** — we borrowed USDC we didn't end up needing, so it goes back to the
pool. That repay is triggered from the webhook handler (see below).

### Why default to direct?

Direct doesn't require us to keep a large pre-funded float sitting at YC. Fiat is
faster and simpler but ties up capital. So we prefer direct and treat fiat as the
safety net.

---

## The direct to fiat pivot

This is the subtle part. "Direct first, fiat as fallback" happens in **two
different places**, depending on *when* direct fails.

### Pivot at submission time (synchronous)

Inside `Initiate` ([`offramp_yellowcard.go`](../../pkg/mobile/ussd/adapters/offramp_yellowcard.go)), if the direct attempt errors out
*before the crypto has irreversibly moved*, we immediately retry in fiat mode.
The two failure checkpoints are:

- **F1** — the YC API call to create the payment fails. No crypto sent yet.
- **F2** — the on-chain USDC transfer to YC's wallet fails (e.g. no trustline).
  The USDC is still in the treasury.

In both cases the USDC is safely still ours, so the fallback is clean: we re-run
as fiat with a **new sequence id** — the original key plus a `_fiat` suffix — so
YC sees it as a distinct request and we never collide with the abandoned direct
attempt. This all happens in one call; the borrower never notices.

### Pivot after the fact (asynchronous, via the refund poller)

The harder case: the direct payout *was* submitted, the USDC *did* reach YC, and
then YC fails the payout. Now the money is sitting at YC and they owe us a crypto
refund. We can't synchronously retry — we have to wait for the refund first.

This is handled by the **refund poller** ([`pkg/webhook/refund_poller.go`](../../pkg/webhook/refund_poller.go)), a
background loop that runs every 30 seconds:

1. A failed direct payout is marked `refund_pending` by the webhook handler.
2. The poller picks up every `refund_pending` loan and asks YC for its status.
3. When YC reports `refunded` (crypto is back in our treasury), the poller
   **retries the payout in fiat mode** — same `_fiat` sequence id convention.
4. Crucially, it first flips the loan's `settlement_method` to `fiat`
   (`SetSettlementMethod`). Without this flip, when the retried payout later
   completes, the webhook handler would still see `"direct"` and skip the Vault
   repay — leaving borrowed USDC stranded. The flip makes the eventual completion
   repay correctly.
5. If the retry itself fails, or YC reports `refund_failed`, the loan is marked
   terminally failed, ops is alerted, and the Vault is repaid (the crypto is back
   in treasury, so it should go home).

So `settlement_method` on a loan is not fixed at submission — a loan that started
`direct` can end up `fiat` after this pivot, and that field is the source of
truth the rest of the system reads.

---

## Webhook events — what each one does

After submission we're passive: YC tells us what's happening by calling our
webhook, and `ProcessYellowCardEvent` ([`pkg/webhook/webhook_service.go`](../../pkg/webhook/webhook_service.go)) maps
each event to a status change plus side effects.

| YC event / status | What we do |
|---|---|
| `complete` | Mark `complete`; record what was delivered + fees; **fiat only:** repay the Vault; SMS the borrower |
| `failed` (direct) | Mark `refund_pending`; alert ops; the refund poller takes over (await crypto refund, then fiat retry) |
| `failed` (fiat) | Mark `failed`; SMS the borrower; alert ops |
| `pending_liquidity` | YC's balance is low; they auto-retry for ~2h. Alert ops, change nothing |
| `pending_refund` / `refund_processing` | A crypto refund is in flight; mark `refund_pending` |
| `refunded` | Crypto is back; mark `refund_received` (the poller drives the fiat retry) |
| `refund_failed` | Mark `failed`; alert ops — **manual intervention needed** |
| `processing` / `pending` / `created` | Mark `processing` |
| `pending_settlement` | Direct mode only: YC is waiting for our crypto. Mark `direct_submitted` |

A few things worth calling out:

- **We figure out direct-vs-fiat ourselves.** YC's webhook payload does *not* say
  whether a payout was direct or fiat. So on a `failed` event we look the loan up
  and read its `settlement_method` (`IsDirectSettlement`) to decide between the
  refund path and terminal failure. A loan with no settlement method recorded is
  treated as *not* direct — safer to fail it than to wait forever for a refund
  that will never arrive.
- **The webhook is thin.** It carries the payment id, sequence id, status, and
  event name — and nothing else. No amounts, no fees. That's why capturing the
  final numbers needs a separate lookup (next section).

---

## Recording what was actually delivered (and the fees)

When a payout completes we need two things the webhook doesn't give us: the exact
local amount the borrower received, and the fees YC charged. So on a `complete`
event we make one extra call back to YC — `LookupPayment` — to fetch the final
payment details. `recordCompletionFinancials` ([`webhook_service.go`](../../pkg/webhook/webhook_service.go)) packages
them into a `CompletionFinancials` value and hands them to the host loan service
through the `RecordDisbursementCompletion` interface method, which persists them.

What gets stored, all in minor units (cents):

| Field | Meaning |
|---|---|
| `delivered_amount_kes` | What the borrower actually received — YC's `convertedAmount`. Not a computed figure |
| `service_fee_local` / `service_fee_usd` | YC's service fee, charged against our held balance |
| `partner_fee_local` / `partner_fee_usd` | YC's partner fee, audit-only for now |

Two rules behind this:

- **Delivered = `convertedAmount`, full stop.** We don't compute it by subtracting
  fees from some gross amount — the fees come out of *our* held YC balance, not
  out of what the borrower receives, so subtracting would misstate what landed in
  their wallet and corrupt the accounting.
- **Only the service fee feeds repayment.** The service fee is a real cost of
  delivering this loan, so it's added to what the borrower owes (alongside the
  Vault's borrow interest). The partner fee is currently borne by the business
  and kept only for audit — that may change after development.

This capture is **idempotent**: if `delivered_amount_kes` is already set, a
replayed `complete` webhook is ignored, so the numbers can't be double-written.

There is also a phantom field worth knowing about: YC's API has a
`networkFeeAmount` but it always returns `0` for us, so there is no column for it.
A real network/SMS admin fee may be added later, with its own design doc, once
there's an actual source for it.

> **MoneyGram note:** the SEP-24 path maps its `amount_fee` to the same
> `service_fee_local` field, keeping fee accounting uniform across providers.

---

## Config & overrides on this path

Everything tunable on the YC off-ramp, and how each behaves in production.

### Test-only overrides (must be OFF in production)

These exist purely to exercise the flow against YC's sandbox. Both live on
`YellowCardOffRampConfig` ([`offramp_yellowcard.go`](../../pkg/mobile/ussd/adapters/offramp_yellowcard.go)) and both are **gated by the
host that wires up the adapter**: the env vars must only be read when
`SERVER_ENVIRONMENT` is not `production`. Whenever either is active, the adapter
logs a loud `WARN` on startup and on every payout, so it can't slip by unnoticed.

| Override | What it forces | Why it exists |
|---|---|---|
| `TestDestinationPhoneOverride` | Every payout goes to one fixed phone number | Hit YC's sandbox simulation numbers (e.g. a guaranteed-success momo number) instead of real users |
| `TestDestinationAddressOverride` | Direct mode checks *this* address's trustline before submitting | Point it at an address with no USDC trustline to deliberately trigger the direct to fiat failover without leaving an orphaned direct payment at YC |

### Normal configuration

| Setting | Notes |
|---|---|
| YC base URL + API credentials | Sandbox vs production endpoint and keys |
| `BusinessID` / `BusinessName` | Identifies us as the sender on every payout; the name is sanitised to letters and spaces (YC validation rule) |
| Channel/network cache TTL | The adapter caches YC's channel and network lists for 15 minutes to avoid refetching them on every payout |
| Refund poller interval | How often the refund poller checks for completed crypto refunds (default 30s) |

### Defaults & fallbacks baked into the code

- **Settlement method defaults to `direct`** when the request doesn't specify one.
- **Network resolution is forgiving.** If the borrower's stored network code
  doesn't map cleanly to a YC network (e.g. a USSD simulator sends `"SANDBOX"`),
  the adapter falls back to the first active mobile-money network for that
  country rather than failing the payout (`validateNetwork`).
- **Phone numbers are normalised** to international `+254…` form; YC rejects bare
  `254…`.

---

## Failure modes & ops playbook

| Symptom | What it means | Action |
|---|---|---|
| `pending_liquidity` alert | YC's float is low; they auto-retry ~2h | Usually self-heals; top up YC balance if it persists |
| Loan stuck in `refund_pending` | Direct failed; awaiting crypto refund from YC | The poller is working it; check it's running and YC is returning the refund |
| `refund_failed` alert | YC couldn't return our crypto | **Manual** — reconcile with YC directly |
| `vault_repay_status = failed`, repay hash NULL | A payout settled but returning USDC to the Vault failed | The USDC is stuck in treasury; sweep/retry the repay manually |
| Fiat failover failed alert | Direct refunded but the fiat retry couldn't go through (e.g. low YC balance) | Loan is marked failed and Vault repaid; investigate the retry error |

---

## Related

- [Off-ramp overview](./README.md) — the two-settlement-modes / webhook-driven
  framing and shared conventions.
- [Soroban / Vault](../soroban/vault.md) — the borrow and repay calls the
  off-ramp triggers on-chain.
