# Microvault Airtel Money

Developer reference for the **Airtel Money** cash-in rail: a borrower repays a
loan from their Airtel Money wallet through a USSD push prompt. It is the
second mobile-money rail beside [M-Pesa](../mpesa/README.md) and mirrors its
shape: a client package, a staging table, tickers, and a credit-side adapter.

**Status: built, inert.** Every path is gated on credentials, and there is no
Airtel developer account yet. With no `AIRTEL_*` variables set, nothing is
constructed, the repay menu does not offer Airtel, and the config validates
in every environment.

Two gates, like the YellowCard/Fonbnk relay:

- **Credentials** (`AIRTEL_CLIENT_ID` + `AIRTEL_CLIENT_SECRET`,
  `AirtelConfig.Enabled()`) construct the client, the collection adapter, the
  prompt loan poller and the summary sweeper.
- **`ENABLE_AIRTEL_MONEY_SWITCH`** (`AirtelConfig.Offered()`, unset is off)
  offers the rail to borrowers in the repay menu. Switching it off hides the
  rail for new repayments only; the pollers and the callback keep settling
  any repayment already in flight.

## What's wired

| Piece | Where | Status |
|---|---|---|
| Client (auth, Collection v2 with message signing, enquiry, summary, refund, KYC, balance) | [`pkg/payment/airtel`](../../pkg/payment/airtel) | Built, tested against `airtelstub` |
| Staging table `airtel_transactions` + `airtel_summary_cursor` | migration `000020` (core) | Built |
| Loan columns `repayment_airtel_*` | migration `000035` (credit) | Built |
| Collection adapter (push only) | `AirtelCollectionAdapter` in the credit module | Wired in `cmd/credit` behind `AirtelConfig.Enabled()` |
| Prompt loan poller (enquiry-driven settlement) | `AirtelPromptLoanDriver` in the credit module | Wired in `cmd/credit` behind `Enabled()` |
| Summary sweeper (missed-callback reconciliation) | [`pkg/services/airtelpoller`](../../pkg/services/airtelpoller) | Wired in `cmd/credit` behind `Enabled()` |
| USSD repay menu option | `HandlerDeps.AirtelPrompter` | Shown only when `Offered()`, and only to borrowers on the Airtel network (see below) |
| Collection callback `POST /api/v1/callbacks/airtel/:slug/collection` | `AirtelCallbackController` | Mounted in `cmd/credit` and `cmd/microvault` whenever `AIRTEL_CALLBACK_SLUG` is set, independent of the switch |

`Enabled()` means `AIRTEL_CLIENT_ID` and `AIRTEL_CLIENT_SECRET` are both set.

## The big ideas

1. **The enquiry is the authority.** Airtel documents its callback as
   carrying intermediate *or* final status, and its `TS`/`TF` codes cover
   less than the enquiry's `TA`/`TIP`/`TE`. A `TF` callback is not proof of
   failure, and a verified callback hash proves Airtel sent it, not that the
   payment settled. Loans settle only on what `CollectionEnquiry` says.
2. **Our transaction id is the key.** The callback carries no loan
   reference, only the transaction id we minted. That id is stored on the loan
   (`repayment_airtel_txn_id`) and keys `airtel_transactions`, so the loan and
   any callback row find each other without Airtel's receipt, which does not
   exist until the payment succeeds.
3. **Ambiguous means enquire, never retry.** In-process codes, HTTP 408/502/504
   and the `ESB000001/4/8/14` family may still succeed. Resending a fresh id
   makes a second payment. The poller enquires instead, starting after
   Airtel's documented three-minute floor.
4. **Push only.** Airtel Collection has no paybill equivalent, so
   `Collect` refuses and every collection is a USSD push.
5. **The network the borrower dials from picks the prompt.** A prompt can only
   reach a wallet on the SIM's own network. Africa's Talking sends a
   `networkCode` (MCC+MNC) with every USSD request: `63902` Safaricom,
   `63903` Airtel, `63907` Telkom, `63999` Equitel. That code is the network
   the SIM is on, so it is right for ported numbers too. On Safaricom the
   menu offers only the M-Pesa prompt, on Airtel only the Airtel push, and on
   any other Kenyan network neither; the paybill is always offered. Without a
   usable code (missing, the `99999` simulator, a foreign network), the
   number's prefix allocation (`phone.KenyaOperatorByPrefix`) is used as a
   hint: both prompts stay, with the guessed network first, because numbers
   have been portable since 2011. The provider is pinned by the menu choice;
   the cash-in registry's generic prompt method still points at M-Pesa.

## Flow

```
USSD repay menu → "Airtel Money"
  → LoanServiceAdapter.PromptRepaymentVia(airtel)
  → AirtelCollectionAdapter: mint txn id, CollectionPayment (USSD push), store id on the loan
  → AirtelPromptLoanDriver: after AIRTEL_ENQUIRY_DELAY, CollectionEnquiry every AIRTEL_POLL_INTERVAL
       success          → funds_received, record airtel_money_id, SMS the borrower
       terminal failure → close the attempt (repayment_status=expired)
       ambiguous        → keep enquiring; after AIRTEL_MAX_ATTEMPTS, close as expired
  (callback, when mounted: upsert into airtel_transactions; never overrides an enquiry-confirmed row)
  → SummarySweeper: walks the Transactions Summary window to catch payments whose callback never came
```

KES collected in the Airtel merchant wallet becomes USDC at the OTC desk, as
with M-Pesa (`AIRTEL_SETTLEMENT_MODE=otc`, the only built mode).

## Callback security

The callback group is mounted at `/callbacks/airtel/:slug` and `requireSlug`
checks the slug in constant time, so the secret stays out of route templates,
logs and spans. A body whose `hash` fails HMAC-SHA256 verification against
`AIRTEL_CALLBACK_HMAC_KEY` is rejected 403; one with no hash is recorded
unverified. The source-IP allowlist (`AIRTEL_CALLBACK_ALLOWED_CIDRS`, via
`middleware.ClientIP`) is log-only when empty. **Airtel does not publish its
egress range**; production is blocked until their support supplies it.

## Configuration

`AirtelConfig` ([`pkg/config/config.go`](../../pkg/config/config.go)):

| Variable | Notes |
|---|---|
| `AIRTEL_CLIENT_ID` / `AIRTEL_CLIENT_SECRET` | Both set = rail constructed |
| `ENABLE_AIRTEL_MONEY_SWITCH` | Offer the rail in the repay menu; unset is off |
| `AIRTEL_ENVIRONMENT` | `staging` or `production` |
| `AIRTEL_COUNTRY` / `AIRTEL_CURRENCY` | Default `KE` / `KES` |
| `AIRTEL_SIGNING_ENABLED` | Collection v2 message signing |
| `AIRTEL_CALLBACK_SLUG` / `AIRTEL_CALLBACK_BASE_URL` | Callback path secret and host |
| `AIRTEL_CALLBACK_HMAC_KEY` | Verifies the callback `hash` |
| `AIRTEL_CALLBACK_ALLOWED_CIDRS` | Airtel's egress range, when known |
| `AIRTEL_ENQUIRY_DELAY` | Wait before the first enquiry; Airtel documents 3 minutes |
| `AIRTEL_POLL_INTERVAL` / `AIRTEL_MAX_ATTEMPTS` | Enquiry cadence, and rounds before the attempt closes as expired |
| `AIRTEL_SUMMARY_SWEEP_INTERVAL` | Summary reconciler cadence |
| `AIRTEL_PROMPT_AMOUNT_KES` | Staging-only fixed push amount; a boot error in production |
| `AIRTEL_SETTLEMENT_MODE` | `otc` only |

`LOAN_REFERENCE_PREFIX` is shared with M-Pesa.

## Open until staging is exercised

Run `go test -tags airtel_sandbox ./pkg/payment/airtel/` against staging
credentials to settle:

- whether `transaction.amount` takes minor units;
- whether Account's `type` is a query parameter;
- whether the gateway is header-casing sensitive (Go canonicalises outgoing
  header names);
- which rendering of the callback body Airtel's HMAC covers.

## Related docs

- [`pkg/payment/airtel/doc.go`](../../pkg/payment/airtel/doc.go), the
  client's safety rules: final vs intermediate callbacks, the receipt that
  only exists on success, `ROUTER115` (wrong PIN) vs `ROUTER116` (our
  encryption).
- [M-Pesa](../mpesa/README.md), the sibling rail this one mirrors.
- [Mobile](../mobile/README.md), the USSD repay menu.
