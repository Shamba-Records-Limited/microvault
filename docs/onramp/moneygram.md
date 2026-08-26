# MoneyGram On-Ramp (Cash-In Repayment)

How a borrower settles a USDC loan with **cash handed over at a MoneyGram
agent**, including the SEP-24 interactive deposit, the poller that drives it,
the treasury-to-vault leg, and every config override on this path.

This is the mirror of [MoneyGram cash pickup](../offramp/moneygram.md), but it
is not that flow reversed, see [§ Why a deposit is not a withdrawal
backwards](#why-a-deposit-is-not-a-withdrawal-backwards).

---

## The short version

1. At the USSD menu the borrower picks a loan to repay. We quote the payoff,
   check it fits MoneyGram's cash-in corridor, and **lock the quote**.
2. We open a SEP-24 **deposit** with MoneyGram and get back a webview URL. That
   URL is SMS'd to the borrower. They open it, do KYC, and pick an agent.
3. MoneyGram issues either a **reference number** or a **transaction page**. We
   SMS whichever exists, because the borrower cannot pay without one.
4. The borrower hands cash to the agent. MoneyGram sends USDC to the treasury
   with our memo attached.
5. The **poller** sees `completed`, tells the borrower their money arrived, then
   calls `repay_for` on the Vault, treasury pays, borrower gets the credit.

Three files carry most of the weight:

- [`loan_service_adapter.go`](../../../microvault-credit/internal/credit/adapters/loan_service_adapter.go)
  (credit module), `InitiateRepayment` and `runRepaymentInitiation`: quoting,
  the corridor gate, and opening the SEP-24 deposit.
- [`pkg/services/mgpoller/deposit.go`](../../pkg/services/mgpoller/deposit.go):
  `DepositDriver`, the whole cash-in state machine.
- [`pkg/payment/stellaranchor/sep24.go`](../../pkg/payment/stellaranchor/sep24.go):
  `InitiateDeposit` and the SEP-24 transaction shape both directions share.

---

## Why a deposit is not a withdrawal backwards

The two directions differ in more than sign.

A **withdrawal** is something *we* started and MoneyGram must finish. It
resolves in minutes, so it is polled hard on a fixed ticker until it settles.

A **deposit** is something the *borrower* must finish, and nothing on our side
compels them to. The window is roughly a day, most of which they spend doing
nothing. A 30-second tick against MoneyGram for that long is tens of thousands
of pointless requests.

So cadence lives in a **`repayment_next_poll_at` column**, not in the ticker.
The runner's interval only decides how often we *ask* which rows are due; the
column decides which rows come back. This is worth internalising before
debugging anything timing-related, turning `REPAYMENT_POLL_INTERVAL` down does
not make a parked deposit poll sooner. See [§ Config](#config--overrides-on-this-path).

Both directions share the generic
[`Runner`](../../pkg/services/mgpoller/runner.go): wake on a cadence, fetch a
batch, drive each record, never let one record's failure abort the batch.

---

## Initiation

`InitiateRepayment` splits into a synchronous preflight and a background worker.

**Synchronous**, because a USSD screen that promises an SMS must not be shown
when the request could never have produced one: the anchor must be wired, and a
loan ID and phone number must both be present. Both checks are local.

**Background**, because everything past that talks to MoneyGram and took over
fifteen seconds against the sandbox, past the point Africa's Talking abandons
the session. `runRepaymentInitiation` runs on its own context with a 2-minute
timeout, and every exit path SMSes the outcome.

The background half:

1. **Quote the payoff** and check it against MoneyGram's cash-in corridor
   (15-950 USD, `MinDepositUSD` / `MaxDepositUSD` in
   [`corridor.go`](../../pkg/payment/moneygram/corridor.go)). The USSD menu
   already hides the rail for an ineligible payoff, but this path is reachable
   without that screen. The two ends carry different error codes because they
   are actioned differently, below the floor the borrower uses mobile money,
   above the ceiling they must split the payment.
2. **Derive the child-account memo** for the SEP-10 session. This scopes the
   custodial treasury to one borrower, exactly as on the withdrawal side.
3. **Post the SEP-24 deposit**, with the treasury as the destination account and
   a request-side `memo` naming the loan.
4. **Lock the quote**, write `repayment_status=initiated`,
   `repayment_payoff_stroops`, `repayment_locked_at`, `repayment_expires_at` and
   `repayment_mg_tx_id` in one update.
5. **SMS the webview link.**

> If step 4 fails after step 3 succeeded, the deposit exists at MoneyGram and we
> have no record of it. The poller will never drive it, and a borrower who pays
> is unreconciled. That branch logs `CRITICAL` for exactly this reason.

### Two memos, two jobs

This trips people up. There are **two different memos** on this path and they
answer different questions.

| Memo | Where | Answers |
|---|---|---|
| SEP-10 child-account memo | The auth JWT | *Which borrower is this session?* |
| SEP-24 request `memo` | The Stellar payment MoneyGram makes | *Which loan is being settled?* |

The **child-account memo** is `stellaranchor.ChildAccountMemo(seed, index)`, a
sha256 derivation namespaced by the key it is seeded with. It is seeded from
the **auth wallet** (`MONEYGRAM_AUTH_SECRET`), *not* the treasury. The poller
seeds it the same way. Seeding the two sides differently puts the deposit in a
memo space the poller never queries, so the borrower could pay and nothing would
ever see it. The two are identical by default because both env vars fall back to
`TREASURY_SECRET_KEY`, which is what made the earlier divergence invisible.

The **deposit memo** is the loan reference (falling back to the loan ID),
truncated to MEMO_TEXT's 28 bytes. Every borrower's deposit lands on the one
treasury address, so without it the payments are told apart only by amount and
timing. It is optional in the spec and MoneyGram may drop it, `deposit_memo` on
the polled transaction reports what was actually attached. **Confirmed honoured
on testnet:** MoneyGram echoes back `deposit_memo` and `deposit_memo_type=text`.

### Why the treasury and not a child account

Child accounts hold no USDC trustline. Adding one per borrower would cost a
sponsored reserve for an account that only ever passes funds through. So the
deposit destination is the treasury, and the memo is what ties an inbound
payment back to one loan.

---

## Telling the borrower how to pay

The borrower cannot hand over cash without something to quote at the counter,
and MoneyGram issues two different artifacts at two different times.

| Artifact | When it exists |
|---|---|
| `external_transaction_id` | SEP-24 defines it as the external transaction that **started the deposit**. On a cash-in that means it does not exist until the borrower has *already paid*. Testnet confirms it empty through `pending_user_transfer_start`. |
| `more_info_url` | The field the spec designates for telling a user how to start a deposit. Populated from the first poll onward. |

So the reference alone could never arrive in time. `sendPayInstructionsOnce`
prefers the reference. A code a borrower can read out beats a link they must
open on a feature phone, and falls back to the transaction page.

Two properties of this send worth knowing:

- **It is checked on every tick**, not inside one status branch. Tying it to a
  nominated status means guessing which one the artifact arrives at.
- **The marker is spent before the send.** A failing SMS provider must not be
  retried every tick for the rest of the window. The consequence is that a
  failure here is *terminal* for the loan: no later tick retries, and the deposit
  sits open until it lapses with the borrower never told how to use it. That is
  why `notifyPayInstructions` raises an ops alert naming the remedy, clear
  `repayment_reference_sent` and the next tick resends.

The more-info link is shortened before it is sent. The notifier tries dub on
MoneyGram's raw URL, then falls back to our own `/r/{code}` redirect using
`RampMoreInfoShortCode`, minted once when the URL is first persisted.

---

## Poller lifecycle, what each status does

| MG status | What we do |
|---|---|
| `incomplete` | Borrower has not finished the webview. **The only status the window expires from.** Send the single pre-expiry reminder if due. Idle backoff. |
| `pending_user_transfer_start` | Committed: agent chosen, walking to the counter. They tend to pay within the hour, so active backoff. |
| `completed` | Cash reached the treasury as USDC. Notify, then settle the vault leg. See below. |
| `refunded` | MoneyGram returned the borrower's cash. Their money is back and the loan is untouched, so this **ends the rail rather than failing a debt**. |
| `too_small` / `too_large` | Our own corridor gate should have prevented these. Alert *and* fail. It means the amount gate and MoneyGram's limits disagree. |
| `expired` / `no_market` / `error` | Fail the rail. No funds moved. |
| `on_hold` | MoneyGram is doing extra checks. Alert ops, change nothing, active backoff. |
| `pending_*` (anchor, external, stellar, trust, user, user_transfer_complete) | In flight on someone else's side. Active backoff. |

Every tick logs the polled transaction unconditionally, before the switch. Every
branch below it can return without logging, which would make a parked
transaction indistinguishable from a stalled poller.

A failed `GetTransaction` still reschedules. Without that, a transient MoneyGram
outage leaves `next_poll_at` in the past and the row spinning every tick.

### The deadline is MoneyGram's, not ours

MoneyGram states `user_action_required_by` on the transaction, and **that is the
one that binds**. It has been observed at roughly 24 hours against a
`REPAYMENT_WINDOW` default of 96. Acting on ours would keep a dead deposit live
for three days and tell the borrower they still had time.

The deadline is applied to this tick's in-memory copy as well as persisted, so
expiry and the reminder use the real value now rather than one poll later.

### The reminder lead self-caps

`DepositReminderBefore` defaults to 24h, sized for a four-day window. Against
MoneyGram's real 24-hour deadline that would fire the reminder at the moment of
initiation, saying nothing the borrower was not just told.

So the lead is capped at a quarter of the window that actually applies, measured
from `started_at`, not from the time remaining, which would make the threshold
chase itself and only ever be met at expiry.

---

## Completion and the vault leg

`handleCompleted` runs in a deliberate order.

**The borrower is told first**, before the vault leg is attempted. From their
side the repayment is done: the cash reached the treasury. The treasury-to-vault
transfer is our leg, and a retry on it is not something they should hear about.

Then two preconditions, each of which withholds the vault leg and alerts rather
than guessing:

- **No borrower address**. We could still call plain `repay`, but that silently
  drops the attribution the whole rail exists to produce.
- **No locked payoff**, nothing to repay.

Then `repay_for(borrower, amount)` on the Vault. The treasury is the payer; the
borrower authorises nothing and the address is carried onto the `Repaid` event
for attribution only.

### `MarkSettled` is the dangerous line

If `repay_for` lands but `MarkSettled` fails, the chain moved and the row did
not. The next tick sees `funds_received` again and **could repay a second time**.
That branch logs `CRITICAL` and alerts ops with an explicit "do not let this loan
be repaid again".

### The vault leg never gives up

The borrower's USDC is already on the treasury, so there is no state in which
abandoning the leg is correct. `DepositVaultMaxAttempts` (default 10) decides
when a *human is told*, not when to stop. Past the ceiling the retry slows to
`DepositVaultRetryBackoff` (default 1h), because by then the cause is unlikely
to clear on its own and hammering a broken RPC every two minutes helps nobody.

Attempts only ever increment, so the alert fires on `==` rather than `>=`.
Alerting on `>=` would page ops on every subsequent tick.

The count is persisted (`repayment_vault_attempts`) so a restart does not reset
the ceiling.

### The fee discrepancy, open question

SEP-24 defines `amount_out` as `amount_in` less `fee.total`, so a fee-bearing
corridor credits the treasury with **less than the borrower was quoted**.
MoneyGram charges 3.00 USD on a 23.40 deposit in the sandbox.

`checkDepositShortfall` reports this and does not act on it. The repay still
uses the quoted payoff, which means the treasury absorbs any difference, a slow
drain rather than a visible failure, hence the ops alert.

Whether `amount_out` is genuinely net of the fee is **unconfirmed**: the only
payload seen so far predates the borrower paying, and reported
`amount_out_asset` as fiat rather than USDC, so it cannot be read as settled.
Changing what is repaid on that basis would be guessing. The first completed
deposit answers it with evidence.

---

## State columns

All on `loans`, in the credit module's
[`loan.go`](../../../microvault-credit/internal/credit/app/models/loan.go).

| Column | Purpose |
|---|---|
| `repayment_status` | The state machine. Two live states, four terminal. |
| `repayment_payoff_stroops` | Quote-locked at initiation. Borrow-index movement since then does not change what the borrower owes. |
| `repayment_locked_at` / `repayment_expires_at` | The quote lock window. |
| `repayment_mg_tx_id` | MoneyGram's transaction ID, and the idempotency key. Unique index. |
| `repayment_next_poll_at` | **The cadence.** Indexed; this is what `GetDueRepayments` selects on. |
| `repayment_reminder_sent_at` | Written before the pre-expiry SMS. |
| `repayment_reference_sent_at` | Written before the pay-instructions SMS. Clear this to force a resend. |
| `repayment_vault_tx_hash` | The `repay_for` transaction. |
| `repayment_vault_attempts` | Failed vault-leg count, durable across restarts. |

| `repayment_status` | Meaning |
|---|---|
| `none` | No repayment in flight. |
| `initiated` | **Live.** Deposit open, quote locked, borrower has not paid the agent. |
| `funds_received` | **Live.** USDC on the treasury, vault leg not yet confirmed. The borrower has been told. |
| `settled` | Terminal. Vault leg confirmed. The only state that flips `loans.status` to `repaid`. |
| `expired` | Terminal. Window elapsed unpaid; the lock is released and the borrower owes what they owed before. |
| `failed` | Terminal. The rail failed **before funds moved**. Never used for a failed vault leg, money on the treasury stays at `funds_received` so reconciliation keeps retrying. |

`repayment_status` and `loans.status` are allowed to disagree for a window:
`funds_received` deliberately does not flip the loan to `repaid`, because the
vault leg may still be retrying.

---

## Config & overrides on this path

Under `MoneyGramConfig` ([`config.go`](../../pkg/config/config.go)), from
`REPAYMENT_*` env vars.

| Setting | Default | Notes |
|---|---|---|
| `REPAYMENT_WINDOW` | 96h | Our window. **Superseded by MoneyGram's `user_action_required_by` whenever it is present.** |
| `REPAYMENT_POLL_INTERVAL` | 60s | How often the runner *asks* for due rows. Not how often any one deposit is polled. |
| `REPAYMENT_ACTIVE_BACKOFF` | 2m | Gap between polls once the borrower has engaged. |
| `REPAYMENT_IDLE_BACKOFF` | 30m | Gap while the borrower has not opened the link. This is most of the window. |
| `REPAYMENT_REMINDER_BEFORE` | 24h | Lead on the single pre-expiry reminder, capped at ¼ of the real window. `0` disables. |
| `REPAYMENT_VAULT_MAX_ATTEMPTS` | 10 | Failed vault legs before ops are alerted. Not a give-up threshold. |
| `REPAYMENT_VAULT_RETRY_BACKOFF` | 1h | Gap between vault-leg retries past the ceiling. |

### Watching a deposit move in development

The three backoff overrides exist for this. Setting `REPAYMENT_POLL_INTERVAL`
alone does nothing for a row parked 30 minutes out. It changes how often the
runner asks, not what it gets back. Set the backoffs:

```
REPAYMENT_ACTIVE_BACKOFF=5
REPAYMENT_IDLE_BACKOFF=10
REPAYMENT_VAULT_RETRY_BACKOFF=15
```

Unset means zero, and the wiring in `cmd/credit` leaves the poller's own
defaults in place rather than config restating them.

---

## Failure modes & ops playbook

| Symptom | What it means | Action |
|---|---|---|
| `Repayment instructions not delivered` alert | The pay-instructions SMS failed and the marker is already spent. The borrower will never be told how to pay. | **Manual**, clear `repayment_reference_sent_at` on the loan; the next tick resends. |
| `Repayment settled on-chain but not recorded` | `repay_for` landed but the row did not update. | **Urgent, manual**, record the settlement by hand. Do not let the loan be repaid again. |
| `Repayment vault leg stuck` | `repay_for` has failed 10 times. Borrower paid, their USDC is on the treasury, loan still open. | Investigate the RPC / vault. The retry continues on its own at 1h. |
| `Repayment deposit short of the quoted payoff` | MoneyGram credited less than quoted; the treasury is absorbing the difference. | See [§ The fee discrepancy](#the-fee-discrepancy--open-question). Collect evidence before changing behaviour. |
| `Repayment missing borrower address` / `missing locked payoff` | Funds received but the vault leg cannot be attributed or sized. | **Manual**, backfill the loan row; the next tick proceeds. |
| `MoneyGram deposit outside anchor limits` | `too_small` / `too_large`. Our corridor gate disagrees with MoneyGram's. | Reconcile `corridor.go` against MoneyGram's published limits. |
| `MoneyGram deposit on hold` | Compliance check. | Coordinate with MoneyGram. Nothing to do locally. |
| `loan has no phone number to notify` | The loan row was loaded without its user association. | Fixed, `GetByID` now preloads `User`. If it recurs, check for a new `Get*` that does not. |

---

## Known gaps

- **Fee presentation and settlement** is undecided pending a completed deposit
  with cash actually paid. See [§ The fee discrepancy](#the-fee-discrepancy--open-question).
- **The inbound USDC payment is not verified on-ledger** before the vault is
  repaid. The matching key is resolved (`deposit_memo` works); the on-ledger
  confirmation still wants a completed deposit to build against. Contrast the
  refund path on the withdrawal side, which already does verify.

---

## Related

- [On-ramp overview](./README.md), shared conventions across cash-in rails.
- [MoneyGram cash pickup](../offramp/moneygram.md): the same anchor, the other
  direction: SEP-24 withdrawal, treasury send, and refund settlement.
- [Soroban / Vault](../soroban/vault.md), `repay_for`, the call this rail
  triggers on-chain.
