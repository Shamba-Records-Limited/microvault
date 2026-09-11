# Microvault M-Pesa (Daraja)

Developer reference for the M-Pesa integration: a client for Safaricom's
Daraja API gateway, the confirm-before-credit staging pipeline every inbound
notification goes through, and the pollers that turn an observation into
money actually credited.

Code: [`pkg/payment/mpesa`](../../pkg/payment/mpesa) for the Daraja client,
[`pkg/controllers/daraja_callback_controller.go`](../../pkg/controllers/daraja_callback_controller.go)
and
[`daraja_hakikisha_controller.go`](../../pkg/controllers/daraja_hakikisha_controller.go)
for the inbound routes, [`pkg/services/mpesapoller`](../../pkg/services/mpesapoller)
for the core-side tickers. The client is a library with no database and no
consumer of its own — the actual collection flow (STK prompts, loan
attribution, repayment settlement) is wired in the sibling credit module,
referenced throughout below.

---

## The big ideas

1. **Daraja signs nothing. A callback is an observation, never evidence.**
   Anyone who learns a callback URL can post a well-formed payment
   notification to it. Nothing here credits a loan on the strength of a
   callback alone — every inbound notification lands in a staging table
   (`mpesa_transactions`) and is confirmed independently before anything
   downstream acts on it. See
   [`pkg/payment/mpesa/doc.go`](../../pkg/payment/mpesa/doc.go) for the full
   list of safety rules a caller of this package must uphold — none of them
   are enforced by the package itself.

2. **A queue timeout is not a failure.** Every Initiator-bearing call
   (Reversal, Transaction Status, Account Balance, B2C/B2B) takes both a
   `ResultURL` and a `QueueTimeOutURL`. A delivery to the timeout URL means
   Daraja did not finish in time — nothing about whether the transaction
   later completed. Treat it as `OutcomeUnknown` and resolve with
   `TransactionStatus`; retrying on a timeout is how money moves twice. See
   [`pkg/payment/mpesa/callback.go`](../../pkg/payment/mpesa/callback.go).

3. **The two async deliveries cannot be told apart by their body.** A
   result and a timeout carry the identical envelope; only the URL that
   received the post distinguishes them. `AsyncResult` and `AsyncTimeout`
   are therefore two distinct routes passing an explicit `CallbackKind`,
   never one handler inferring which happened —
   [`daraja_callback_controller.go`](../../pkg/controllers/daraja_callback_controller.go)'s
   `Register`.

4. **Verification fails closed.** `ValidateMobileNumber` reports a match
   only on Daraja's documented success code; every other response,
   documented or not, is a non-match. A verifier that answers "verified" to
   a code it doesn't understand is worse than no verifier, because it is
   trusted. The same discipline governs the unmapped-result-code fallback
   in `ExpressOutcomeFor` and `AsyncOutcomeFor` — see § Outcome
   classification below.

---

## What's actually wired vs. package capability

The client (`pkg/payment/mpesa`) implements most of Daraja's API surface.
Only a subset has a consumer:

| Capability | Consumer | Status |
|---|---|---|
| M-Pesa Express (STK push) | `MpesaCollectionAdapter.Prompt` (credit) + `MpesaSTKLoanDriver` poller (credit) | Wired end to end — loan repayment |
| C2B v2 (paybill) | `DarajaCallbackController.C2BValidation`/`C2BConfirmation` + `PullSweeper` | Wired end to end — passive loan repayment |
| C2B Hakikisha | `DarajaHakikishaController` | Wired end to end — account-name resolution for Safaricom's confirmation screen |
| Pull Transaction | `PullSweeper` (core) | Wired — reconciliation sweep, and the only route to an unmasked payer MSISDN |
| Account Balance | `BalancePoller` (core) | Wired — ops signal, floor alerts |
| Mobile Number Validation | `MpesaCollectionAdapter.validateNumber` | Wired, advisory-only — see § Mobile Number Validation |
| B2C / B2Pochi | none | Package capability only. Disbursement stays on the OTC desk (`cmd/mpesa-settle`) until a provider and fee comparison settle it |
| B2B (PayBill/BuyGoods/PayToBulk/PayTaxToKRA) | none | Package capability only — the collection→disbursement float bridge described in `b2b.go` is not built |
| Reversal | none | Package capability only. Deliberately not automated — see `reversal.go`'s doc comment on why a reversal needs a human in the loop |
| Transaction Status | none | Package capability only, though it's the documented way to resolve a queue timeout when a caller does add one |
| Dynamic QR | none | Package capability only |
| Query Org Info (B2B Hakikisha) | none | Package capability only — the guard the design intends in front of a future B2B disbursement |

---

## Collection: STK push repayment

The dominant repayment path. `MpesaCollectionAdapter.Prompt`
([`mpesa_collection_adapter.go`](../../../microvault-credit/internal/credit/adapters/mpesa_collection_adapter.go))
pushes a prompt via `Client.Express`, then hands the checkout off to a poller
rather than trusting the callback alone:

```
Prompt() ──Express()──▶ Daraja pushes a prompt to the handset
  │
  ├─ marks the loan RepaymentStatusInitiated,
  │  stores RepaymentMpesaCheckoutID, schedules RepaymentNextPollAt
  │
  ├─ (async) customer pays or cancels
  │    │
  │    ├─ STKCallback lands → staged in mpesa_transactions (NOT credited yet)
  │    └─ or nothing arrives at all (customer just doesn't respond)
  │
  └─ MpesaSTKLoanDriver.Drive() polls ExpressQuery on RepaymentNextPollAt:
       code == 0        → settle(): funds received, confirms the staged
                           callback row if one exists, backfills the receipt
                           later via reconciliation if the callback was lost
       outcome.Retryable → retryOrExpire(): one more interval, up to
                           STKMaxAttempts, then expire
       otherwise         → close(): expired immediately
```

The query, not the callback, is what actually settles the loan — `settle()`
runs whether or not a callback ever arrived, which is why losing a callback
is a receipt-backfill problem, not a lost-payment one.

Sandbox has no STK simulator: a real handset must be charged a real amount.
`MpesaConfig.PromptAmountKES` overrides the quoted payoff with a small fixed
figure for that reason, and `Validate` rejects setting it in production.

**Retry classification matters.** `ExpressOutcomeFor`
([`express.go`](../../pkg/payment/mpesa/express.go)) maps every documented
STK result code to `{Retryable, Operational, Message}`. An **unmapped**
code — sandbox-only codes, or genuinely undocumented ones — falls back to
`{Operational: true, Retryable: true}`, so an unrecognised failure gets the
same bounded retry budget a known-transient one gets, rather than expiring
on the very first poll. See the "yellowcard-offramp-webhook-race-2026-09-10.md"
doc (§3) in the knowledge vault for the incident that shaped this default.

## Collection: C2B paybill

The passive rail: a borrower pays the shortcode directly with the loan's
short reference as the account number. Two Safaricom-pushed callbacks:

- **Validation** (`C2BValidation`) — must answer inside an 8-second budget.
  Checks the reference's *shape* first (`loanref.Validate` against the
  configured prefix, no database round trip for something that plainly
  cannot be ours), then resolves it to a loan. An internal error rejects
  with `C2B00016`; an unresolved reference rejects with
  `C2B00012` (`ValidationInvalidAccountNumber`). **Neither may ever answer
  0**, which is Daraja's signal to accept the payment.
- **Confirmation** (`C2BConfirmation`) — the settled payment, staged into
  `mpesa_transactions` like every other observation. Still not credited
  from here.

`MpesaConfig.ReferencePrefix` must equal `PaymentsConfig.LoanReferencePrefix`
— both load from the same `LOAN_REFERENCE_PREFIX` variable, since the
validator's check-character derivation and the reference generator must
agree on the namespace.

A C2B confirmation's `MSISDN` is masked (`"2547 ***** 126"`,
`mpesa.MaskedMSISDN` — a distinct type so a masked value can never be passed
where a real number belongs). **Pull Transaction is the only route to the
unmasked number**, which is why `PullSweeper` is the compliance path, not
just a reconciliation convenience.

### Pull reconciliation sweep

`PullSweeper` ([`pull.go`](../../pkg/services/mpesapoller/pull.go)) walks
Daraja's Pull API on a wall-clock cadence (`MpesaPullCursorRepository`
tracks the window, not a per-row schedule — there is no queue of due rows to
walk, only a time range). A transaction Pull reports is real by
construction, so every row it writes is `Confirmed: true` regardless of
whether its reference resolves to a loan — an unattributed row is an orphan
for the reversal queue, not an unconfirmed observation. A failed window
leaves the cursor where it was and retries next tick; Daraja's 48-hour
retention gives plenty of room to catch up.

## C2B Hakikisha

Inverts every other Daraja-facing route: Safaricom is the client here, not
the pusher. `DarajaHakikishaController`
([`daraja_hakikisha_controller.go`](../../pkg/controllers/daraja_hakikisha_controller.go))
issues short-lived bearer tokens via `client_credentials` (HTTP Basic
against `HakikishaUsername`/`HakikishaPassword`, signed with its own
`HakikishaSigningKey` — deliberately not `pkg/auth.JWTService`, so the admin
and Hakikisha token families share nothing and one can never verify against
the other), then answers "what name goes with this account number" so
Safaricom can show it on the payer's confirmation screen before they
confirm.

The account name is built from the request's own, already-validated account
number (`"Microvault Loan " + accountNumber`) — **never** from anything
looked up internally. `mpesa.HakikishaResponse`'s doc comment spells out why:
this name is disclosed to any M-Pesa customer who can guess or observe a
reference, so it must identify the obligation, never the borrower.

## Account Balance

`BalancePoller` ([`balance.go`](../../pkg/services/mpesapoller/balance.go))
asks for both shortcodes' balances on a cadence; the figures arrive
asynchronously at `/balance/result`, correlated back to a shortcode via
`OriginatorConversationID` (`MpesaBalanceRepository.ResolveQuery`).
`DarajaCallbackController.recordBalances` persists the parsed figures and
logs a warning when one drops below its configured floor
(`CollectionBalanceFloorKES`/`DisbursementBalanceFloorKES`) — an ops signal,
not a latency-sensitive one.

Every Initiator-bearing call requires a `ResultURL`/`QueueTimeOutURL` pair
(`AsyncURLs.validate`) — `MpesaConfig.DarajaCallbackURL(suffix)` builds one
consistently from `CallbackBaseURL`/`CallbackSlug`, e.g.
`DarajaCallbackURL("balance/result")`. This is also the cheapest end-to-end
check that `InitiatorName`/`SecurityCredential` are correct — if Account
Balance works, Reversal and Transaction Status's shared credential path
works too.

## Mobile Number Validation

Checks an STK payer's MSISDN against the borrower's national ID
(`Client.ValidateMobileNumber`,
[`validation.go`](../../pkg/payment/mpesa/validation.go)). Called **out of
band** in a detached goroutine after the STK push already responded
(`MpesaCollectionAdapter.validateNumber`) — it's a paid, synchronous
third-party round trip, and nothing about whether a payer's number matches
their ID should slow down or fail the push itself. `NumberValidationPolicy`
(`disabled`/`advisory`/`enforcing`) is accepted in config; only `advisory`
(record, block nobody) is actually acted on today — `enforcing`'s blocking
behaviour is not implemented. Results cache by a SHA-256 hash of
`msisdn|idType|idNumber`, never the tuple itself.

## Dormant capabilities

Built, tested, unused — each file documents why in its own doc comment
rather than a stub pretending to be wired:

- **B2C / B2Pochi** ([`b2c.go`](../../pkg/payment/mpesa/b2c.go)) — pays an
  MSISDN from the disbursement shortcode. Nothing calls it; disbursement is
  the OTC desk (`cmd/mpesa-settle`, see § Settlement below).
- **B2B** ([`b2b.go`](../../pkg/payment/mpesa/b2b.go)) —
  `BusinessPayBill`/`BusinessBuyGoods`/`BusinessPayToBulk`/`PayTaxToKRA`.
  `BusinessPayToBulk` is named as the eventual bridge that would let
  collections fund disbursements without a manual sweep
  (`MpesaSettlementProviderSweep`), rejected at boot today as not
  implemented.
- **Reversal** ([`reversal.go`](../../pkg/payment/mpesa/reversal.go)) — the
  most dangerous call in the package; the doc comment lists six
  preconditions a caller must establish before invoking it, none of them
  checkable by the package itself. Deliberately not automated.
- **Transaction Status** ([`status.go`](../../pkg/payment/mpesa/status.go))
  — the documented resolution path for a queue timeout on any endpoint that
  has one; nothing currently calls it because nothing currently needs to.
- **Dynamic QR** ([`qr.go`](../../pkg/payment/mpesa/qr.go)), **Query Org
  Info** ([`orginfo.go`](../../pkg/payment/mpesa/orginfo.go)) — the latter
  is the guard the design recommends in front of any future B2B
  disbursement (confirm the paybill's trading name before paying it; an
  over-payment to the wrong paybill isn't reversible the way an
  over-collection is).

All five share the same `AsyncURLs`-population responsibility Account
Balance had before it was fixed (`AccountBalanceRequest.URLs` was empty for
every call until 2026-09-11 — see the vault doc referenced above). Anyone
wiring up B2C, B2B, Reversal or Transaction Status should build the request
with `MpesaConfig.DarajaCallbackURL(...)` from the start.

## Settlement: OTC desk, not automated

`MpesaConfig.SettlementMode` decides how KES collected on the paybill
becomes USDC. `"otc"` — the only implemented mode — means a desk converts
the float and deposits USDC to treasury by hand;
[`cmd/mpesa-settle`](../../../microvault-credit/cmd/mpesa-settle) then
executes the on-chain `repay_for` leg once that's confirmed out-of-band.
Amount and borrower address come from the loan row, never a command-line
argument, since this command moves real money on-chain. `"provider_sweep"`
(B2B-sweeping the float to an on-ramp's own paybill) is named in config
because the choice is real, but `Validate` rejects it everywhere, not just
in production — selecting an unbuilt settlement mode is wrong regardless of
environment.

## Callback architecture

Every Daraja-facing route hangs off one unguessable, config-driven path
segment:

```
{CallbackBaseURL}/api/v1/callbacks/daraja/{CallbackSlug}/{route}
```

`DarajaCallbackController.Register` mounts `stk/result`, `c2b/validation`,
`c2b/confirmation`, `status/{result,timeout}`, `balance/{result,timeout}`,
`reversal/{result,timeout}`; `DarajaHakikishaController.Register` mounts
`hakikisha/oauth/token` and `hakikisha/resolve` under the same slug. Every
route asserts the path contains none of Daraja's blocked words (`mpesa`,
`safaricom`, `exe`, `exec`, `cmd`, `sql`, `query` — `AssertCallbackURL`).

Two independent layers gate an inbound request, neither alone sufficient:

1. **Source IP allowlist** (`allowedCIDR`) against
   `MpesaConfig.CallbackAllowedCIDRs` (Safaricom's published egress range).
   Log-only when unset in development; a boot-time misconfiguration in
   production (empty list) fails closed with a 403 on every callback rather
   than accepting from anywhere.
2. **The unguessable slug itself** — Daraja signs nothing, so this and the
   IP allowlist are the only things making a forged callback hard to send.

Balance recording is wired additively via
`DarajaCallbackController.EnableBalanceTracking`, called only when a
`BalancePoller` is actually running — without it the controller's behaviour
is unchanged, matching the "absence is honest" convention elsewhere in this
codebase.

## Outcome classification

Two parallel classification schemes, because the two contexts need
different answers:

| | `ExpressOutcome` (`express.go`) | `AsyncOutcome` (`outcomes.go`) |
|---|---|---|
| Covers | M-Pesa Express result codes | Reversal / Transaction Status / Account Balance / B2C / B2B initiator codes |
| Answers | Is this the payer's problem? Can we retry? What do we tell a borrower? | What class of failure (`transient`/`config`/`permission`/`credential`/`operational`)? Retryable? |
| Message audience | Borrower-facing, GSM 03.38 only (may render on USSD/SMS) | Operator-facing |
| Namespace | One flat map, `resultCode → outcome` | Family-scoped (`FamilyReversal`/`FamilyStatus`/`FamilyBalance`) checked before a shared `initiatorOutcomes` set — the same numeric code means different things on different endpoints |
| Unmapped code | `{Operational: true, Retryable: true}` | `{Kind: OutcomeOperational, Retryable: false}` |

Both fallbacks are deliberate, not oversights: Express's says "give an
unknown failure the same retry budget a known-transient one gets" (see §
Collection: STK push above); the async one says "never retry an
unrecognised initiator failure blindly." They differ because a borrower's
STK prompt failing is cheap to retry a few times; an Initiator-bearing call
moving real money is not.

## Configuration reference

`MpesaConfig` ([`pkg/config/config.go`](../../pkg/config/config.go)) fields
worth knowing beyond their doc comments:

| Field | Env var pattern | Notes |
|---|---|---|
| `CollectionShortcode` / `DisbursementShortcode` | — | Split because the two products can sit on separate shortcodes; setting both equal is valid when one shortcode carries both |
| `STKPollInterval` | `MPESA_STK_POLL_INTERVAL` | **Seconds** — parsed as a `time.Duration` via the standard `<n>s`/`<n>m` suffix, not a bare integer of minutes |
| `STKMaxAttempts` | `MPESA_STK_MAX_ATTEMPTS` | Poll rounds, not borrower-visible time; defaults to 3 |
| `PromptAmountKES` | `MPESA_PROMPT_AMOUNT_KES` | Sandbox-only override; a boot error in production |
| `SettlementMode` | `MPESA_SETTLEMENT_MODE` | `"otc"` only; `"provider_sweep"` is named but rejected everywhere |
| `NumberValidationPolicy` | `MPESA_NUMBER_VALIDATION_POLICY` | `enforcing` accepted, not enforced — see § Mobile Number Validation |
| `CallbackAllowedCIDRs` | — | Empty is log-only outside production, a boot-time hard-fail in it |
| `HakikishaSigningKey` | — | Deliberately distinct from the admin JWT key |

---

## Conventions used in this doc

- **Money is stored as whole minor units** (cents for KES), except M-Pesa
  Express itself, which is whole-shilling only (`AmountKES int64`, no
  cents) — Daraja's own constraint, not this codebase's choice.
- **Code references** are clickable links to the file (the function name is
  given alongside, since links resolve to the file, not the line). Links
  into `microvault-credit` point at the sibling credit module, which owns
  the loan database and the actual STK/collection consumer.
