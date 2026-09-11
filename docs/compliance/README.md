# Microvault Compliance

Developer reference for KYB/AML compliance on the vault's institutional
deposit path: screening a depositor's Stellar address via Elliptic, gating
the vault contract's on-chain allowlist on the result, and the admin surface
compliance officers work the queue from.

Code: [`pkg/compliance`](../../pkg/compliance) for the provider-neutral
screening contract, [`pkg/compliance/elliptic`](../../pkg/compliance/elliptic)
for the Elliptic client, [`pkg/services/compliance`](../../pkg/services/compliance)
for the lifecycle service and the two on-chain tickers,
[`pkg/repository/counterparty_repository.go`](../../pkg/repository/counterparty_repository.go)
for persistence, and the vault contract's own allowlist functions in
[`soroban/contracts/vault/src/lib.rs`](../../soroban/contracts/vault/src/lib.rs).
The admin UI lives in the sibling credit module, referenced throughout below.

---

## The big ideas

1. **Screening decides; the contract enforces; a watcher checks the
   enforcer.** Three independent layers, deliberately not one:
   `pkg/compliance.Screener` decides whether an address is trustworthy,
   the vault contract's `AllowList` gate (`require_allowed`, called from
   `deposit`/`mint`/`transfer`/`transfer_from`) is what actually blocks
   money, and `pkg/services/vaultwatch.Watcher` is a canary that flags a
   mismatch between the two rather than correcting one. A bug in any single
   layer does not silently become the system's behaviour.

2. **Intent and observed state are recorded separately.** An admin action
   (approve, revoke) writes what should happen; a *separate* worker
   (`OnchainWriter`) is what actually submits the Soroban transaction and
   confirms it landed. `CounterpartyAddress.OnchainState` — `absent` →
   `pending` → `approved`/`revoked` — is the intent/observed split made
   durable. This is why `RevokeAddress` (the repository) never sets
   `onchain_state` to `revoked` itself; see its doc comment.

3. **The compliance signing key stays out of the web process.** `AllowList`
   membership is a fast path with no timelock (matching the guardian's
   `pause()` precedent — a sanctions hit or a revocation cannot wait days),
   which means whoever holds `ComplianceRoleSecretKey` can move an address
   on and off the allowlist immediately. That key is held only by the
   credit backend's ticker (`OnchainWriter`, started from
   `cmd/credit/main.go`), never by `cmd/admin`. The admin writes intent to
   the database; it never signs a transaction.

4. **A screening record never changes in place.** `address_screenings` is
   append-only — there is no update path anywhere in this codebase for it.
   "What did we know on the day we approved this" has to be answerable
   years later, and a mutable record can't answer it. The admin's screening
   detail view renders from each row's raw `Raw`/`RawPayload`, not from
   parsed columns, for the same reason: it shows the screening as it was,
   not as today's parser would interpret it.

5. **A null risk score is not a clean score of zero.**
   `compliance.Screening.RiskScore` is `*float64` specifically so "Elliptic
   returned no score" and "Elliptic returned 0.0" stay distinguishable —
   collapsing them would be the single most dangerous silent default
   available in this integration. `deriveVerdict` and `applyVerdict` both
   treat "no score, not sanctioned" as `VerdictReview`, never `VerdictApproved`.

---

## Screening: `pkg/compliance` + `pkg/compliance/elliptic`

`compliance.Screener` is the provider-neutral interface
(`ScreenAddress(ctx, ScreenRequest) (*Screening, error)`) — one provider
today, so the package stays a plain interface rather than a registry.
`elliptic.Client` implements it against Elliptic's AML API
(`POST /v2/wallet/synchronous`), screening exactly one asset/blockchain
pair: `blockchain: "stellar"`, `asset: "USDC"` — the vault holds Stellar-
issued USDC, never native XLM, and both strings were confirmed directly
against Elliptic's coverage table.

**Signing.** Every request is HMAC-SHA256 signed over
`"{timestamp_ms}{METHOD}{lowercased_path}{payload}"`, keyed on the
base64-decoded API secret (`sign.go`). Two easy-to-miss details: the path is
lowercased *for signing only* (sent as-is on the wire — a Stellar address is
uppercase base32 and must never appear in a path or query, only ever in the
body), and an empty body signs as the two characters `"{}"`, never `""`.

**Verdict policy** (`verdict.go`), applied in this order:

1. `process_status` anything but `"complete"` → `VerdictPending` regardless
   of what the score says.
2. Sanctions membership (`entity_details.sanctions` on any clustered
   entity) → `VerdictRejected`, absolute, never reachable via a threshold.
3. No risk score on a complete analysis → `VerdictReview` (see big idea 5).
4. Score against `Thresholds.Reject`/`Thresholds.Review` — both configured
   at runtime, not constants, per the source design. **Currently wired as
   `elliptic.Thresholds{}`** in `cmd/credit/main.go` (zero-valued), which
   means every scored, unsanctioned screening routes to `VerdictReview`
   today — there is no admin control for these yet.
5. Elliptic's 404 "not in blockchain" response (`ErrNotInBlockchain`) is
   its own case, handled in `ScreenAddress` before the policy above ever
   runs: `VerdictUnscreenable`, not a pass or a fail — see § Lifecycle
   below on what that becomes.

**Webhook verification** (`webhook.go`) implements the Standard Webhooks
spec from scratch (Elliptic ships verifier classes for JavaScript, Python
and PHP only) — HMAC-SHA256 over `"{id}.{timestamp}.{body}"`, a 5-minute
freshness window, and tolerance for multiple space-separated `v1,<sig>`
candidates during secret rotation. **`VerifyWebhook` is built and tested but
has no caller** — nothing in either repo registers a webhook route for it
yet; Elliptic-initiated rescreening notifications are not currently
consumed. `RescreenSweep` (below) is what stands in for that today.

## Lifecycle: `pkg/services/compliance.Service`

The orchestrator (`lifecycle.go`) that actually calls `Screener` and applies
a verdict to the three-table schema below. Core-owned end to end — unlike
`pkg/services/mgpoller`, nothing here reaches outside core, so there is no
adapter split to a credit-side implementation.

```
SubmitAddress(counterpartyID, address)
  │  validates the address is a well-formed Stellar G... key locally first
  │  ("a typo should not cost an API call") — strkey.IsValidEd25519PublicKey
  │  inserts CounterpartyAddress{status: pending, onchain_state: absent}
  └─ if the counterparty's KYB is already approved → ScreenAndRecord() immediately
     (otherwise the address just waits for ApproveKYB)

ApproveKYB(counterpartyID, actor)
  │  sets kyb_status = approved, kyb_approved_by/at
  └─ screens every address already on file for this counterparty

ScreenAndRecord(addressID)   ◀── also the admin's "rescreen now" action;
  │                              idempotent, so a manual rescreen is simply
  │                              another call to this with no separate path
  ├─ calls Screener.ScreenAddress with the counterparty's
  │  EllipticCustomerReference
  └─ applyVerdict() maps the verdict to a new address status + expiry:
       approved      → status approved, onchain_state absent→pending
                        (queues the on-chain writer)
       rejected       → status rejected (final until a human overrides)
       review         → status review (into the admin queue)
       unscreenable   → status approved, onchain_state absent→pending,
                        but PROVISIONAL — no on-chain history to judge.
                        Visible only via this screening's own Verdict
                        column, since address status alone reads identically
                        to a real approval. A mandatory rescreen on first
                        observed deposit is designed but not wired — an
                        operator has to trigger it manually today.
       pending        → address status untouched (nil); the screening row
                        is still recorded (append-only, even a pending
                        result is worth keeping)
```

A rescreen of an address that's already `pending`/`approved`/`revoked`
on-chain never re-touches `onchain_state` — re-confirming an already-clean
verdict isn't a reason to re-queue a write, and an address a human
explicitly revoked doesn't get silently re-allowed by an automated
rescreen. Only a transition *from* `OnchainStateAbsent` queues a write.

## Persistence: three tables, one repository

`CounterpartyRepository` spans all three — one bounded context, mirroring
how `UserRepository` spans everything user-related. Migration:
[`000018_compliance_screening.up.sql`](../../platform/database/migrations/000018_compliance_screening.up.sql).

| Table | Model | Role |
|---|---|---|
| `counterparties` | `models.Counterparty` | The KYB entity — legal name, jurisdiction, `KYBStatus`, and `EllipticCustomerReference` (set once, from the counterparty's own ID, never changed) |
| `counterparty_addresses` | `models.CounterpartyAddress` | The allowlist's source of truth — `Status` (the verdict states, plus `expired`), `OnchainState` (the intent/observed split), approval/revocation actor + reason |
| `address_screenings` | `models.AddressScreening` | Append-only screening history — `RiskScore *float64`, `Sanctioned`, `Verdict`, and `RawPayload` (the full provider response) |

**Actor columns are text, not a user-UUID FK.** `KYBApprovedBy`,
`ApprovedBy`, `RevokedBy` all store the acting admin's Stellar public key
directly — these are new tables, so there was no legacy FK to work around
(contrast `internal/admin/handlers/limits.go`'s `created_by`, which predates
this convention and stays a UUID FK). `middleware.GetAdminClaims(c).AdminPublicKey`
— plumbing that existed before this module but had no caller — is what the
admin write handlers (below) actually populate these with.

**`RecordScreening`'s nil-means-untouched pointers.** `newAddressStatus` and
`newOnchainState` are both `*T`; `nil` means "leave this column alone." This
is what makes `applyVerdict`'s `VerdictPending` branch safe — a rescreen
that hasn't resolved yet can never silently downgrade an address that was
already approved, because it passes `nil` rather than some default value.

## On-chain enforcement: the vault contract's `AllowList`

Built on OpenZeppelin's `stellar_tokens::fungible::allowlist::AllowList`,
called via its plain associated functions (not the `FungibleAllowList`
trait, which requires `ContractType = AllowList` — conflicting with the
vault's existing `ContractType = Vault`).

```rust
is_allowed(address) -> bool          // AllowList::allowed — membership, independent of enforcement
allow_depositor(caller, address)     // compliance role only, no timelock
disallow_depositor(caller, address)  // compliance role only, no timelock
set_compliance_role(new_role)        // owner only, timelocked — rotating the key is rare & deliberate
set_allowlist_enforced(enforced)     // owner only, timelocked
allowlist_enforced() -> bool         // defaults to false
```

**Enforcement defaults off.** `allowlist_enforced()` defaults to `false` so
an upgrade from a pre-allowlist WASM doesn't lock out existing depositors
before they've been backfilled onto the list. Deposits and share transfers
are gated only once an operator explicitly flips it on, *after* the
backfill completes — see `set_allowlist_enforced`'s doc comment.

**Both sides of a transfer are checked.** `deposit`/`mint` require both
`from` and `receiver` allowed; `transfer`/`transfer_from` require both
`from` and `to`. Constraining one side only would let unscreened money buy
shares for a screened party, or the reverse.

**Two distinct error codes mean "not allowlisted":**

| Code | Name | Path |
|---|---|---|
| 14 | `AddressNotAllowed` | `deposit`/`mint`'s own `require_allowed` check |
| 113 | `FungibleTokenError::UserNotAllowed` | `AllowList::transfer`/`transfer_from`, OpenZeppelin's own panic |

Both mean the same thing to a caller; the difference is which layer raised
it, deposit-side custom logic vs. the OZ allowlist's own transfer gate.

`pkg/stellar/soroban/compliance.go` is the Go side:
`Service.AllowDepositor`/`DisallowDepositor` sign with the compliance role
key specifically (`requireComplianceRole` — fails loudly rather than
falling back to the admin key, which the contract would reject anyway since
`require_compliance_role` checks the caller against the stored
`compliance_role`, not the owner). `Service.WithComplianceRole(privateKey)`
is a fluent setter that mutates the service struct in place — **not safe to
call concurrently**; call it once at startup before any goroutine touches
the service, which is exactly how `cmd/credit/main.go` uses it.

## The two on-chain tickers

Both live in the credit backend (`cmd/credit/main.go`), never `cmd/admin`,
for the signing-key-isolation reason above. Same
tick-immediately-then-on-interval shape as every other ticker in this
codebase (`pkg/services/mpesapoller`, `pkg/services/vaultwatch`).

### `OnchainWriter` — the fast path

```
processAllows()   addresses WHERE status=approved AND onchain_state=pending
                   → AllowDepositor() → SetOnchainState(approved)
processRevokes()  addresses WHERE revoked_at IS NOT NULL AND onchain_state != revoked
                   → DisallowDepositor() → SetOnchainState(revoked)
```

A failed submission leaves the row exactly where it was, retried next tick.
`AllowList::allow_user` is idempotent on the contract side, so a database
write failing *after* a successful on-chain call is safe to retry too — the
next tick's `allow_depositor` is a harmless no-op, not a double-effect. This
"database says pending, contract says approved" drift is precisely the
mismatch `vaultwatch.Watcher` (below) exists to catch as a second,
independent layer.

`OnchainWriterInterval` defaults to 1 minute — ticks far more often than
the rescreen sweep, because this is the fast path (a revocation cannot wait).

### `RescreenSweep` — the sweep neither of Elliptic's own mechanisms cover

```
tick()
  ├─ MarkExpiredAddresses()  bulk: status=approved AND expires_at < now → expired
  └─ for each address now expired: Service.ScreenAndRecord(id)
```

Neither of Elliptic's own rescreening mechanisms (three rescreens over a
window, or retries on a failed call) amounts to standing monitoring — an
allowlist gating real money needs its own scheduled walk of every address
whose screening has gone stale. Reuses `Service.ScreenAndRecord` one
address at a time rather than a batch endpoint (the batch wallet-screening
endpoint was out of scope for the Elliptic client build; at institutional
depositor cardinality, the sync endpoint in a loop costs nothing extra
worth a second client code path). `RescreenSweepInterval` defaults to 1
hour. A rescreen that fails leaves the address visibly `expired` rather
than quietly still showing a stale "approved" badge.

## Canary: `pkg/services/vaultwatch.Watcher`

Walks the vault contract's Soroban events (deposit, mint, transfer) on a
cadence and, for every participant address, checks `IsAllowed` against the
chain directly. **It is a canary, not a second enforcer** — once the
contract's own gating is live, an unallowlisted address participating in an
event should be *impossible*; a mismatch here means the gate itself broke
(a bug, a bad upgrade, a misconfiguration), which is why a mismatch is
logged at `Error` level rather than silently corrected. Same
window-retry-on-failure cursor pattern as the M-Pesa Pull sweep.

## Admin UI

`microvault-credit/internal/admin` (Fiber + templ + HTMX), extending the
existing shape rather than a new bring-up. Three screens:

- **Counterparties** (`handlers/counterparties.go`) — list with a
  `kyb_status` filter, create form (pre-generates the row's UUID so
  `EllipticCustomerReference` — `"kyb-" + id`, per the "our KYB entity ID
  becomes the customer reference" design — can be set on the same insert;
  the column is `NOT NULL UNIQUE`, so writing it empty and backfilling
  later would collide the moment a second counterparty was created first),
  detail view, `ApproveKYB`/`RejectKYB`/`SubmitAddress` actions.
- **Screening queue** (`handlers/screening.go`) — everything `review`,
  `pending`, or `expired`. `Rescreen` is **the first `hx-post` handler in
  this codebase** — `htmx.min.js` was already loaded globally but unused
  until this; it returns just the updated `<tr>` HTML
  (`views.ScreeningRow`), swapped in place via `hx-target`/`hx-swap`,
  rather than a full-page reload blocking on a synchronous Elliptic call
  that can take seconds. A failed rescreen still renders the row's current
  (unchanged) state rather than an error page.
- **Screening detail** (`Screening.Show`) — one address's full
  `address_screenings` history, rendered from `RawPayload` — see big idea 4.

Every write handler reads the actor from
`middleware.GetAdminClaims(c).AdminPublicKey`. Write handlers deliberately
never fold `err.Error()` or an attacker-influenced route param into a
redirect URL — `Counterparties.redisplayDetail` redisplays the page inline
with an error instead; `Screening.Approve`/`Reject` stay plain redirects
but with only a static flash message, never dynamic text, in the URL.

## Configuration

`ComplianceConfig` ([`pkg/config/config.go`](../../pkg/config/config.go)):

| Field | Notes |
|---|---|
| `EllipticAPIKey` / `EllipticAPISecret` | Required for the client to function at all |
| `EllipticBaseURL` | Overrides the client default; tests point it at a stub |
| `ScreeningValidity` | How long an approval stays current; defaults to 90 days |
| `ComplianceRoleSecretKey` | Signs `allow_depositor`/`disallow_depositor` — deliberately distinct from the admin/treasury keys, held only by the credit backend's `OnchainWriter` |
| `OnchainWriterInterval` | Defaults to 1 minute (fast path) |
| `RescreenSweepInterval` | Defaults to 1 hour |

---

## What's built vs. what the source design describes but isn't wired

| Capability | Status |
|---|---|
| Vault contract allowlist (enforcement + admin/timelocked controls) | Built, tested |
| Elliptic wallet screening client + verdict policy | Built, tested |
| KYB/screening lifecycle service | Built, tested |
| Three-table persistence, append-only screening history | Built, tested |
| On-chain writer (allow/disallow tickers) | Built, tested |
| Rescreen sweep | Built, tested |
| Detect-and-quarantine watcher | Built, tested |
| Admin UI (counterparties, screening queue, screening detail) | Built, tested |
| Runtime-configurable score thresholds | **Not built** — `Thresholds{}` is hardcoded zero-valued in `cmd/credit/main.go`, so every scored screening lands in `VerdictReview` |
| Elliptic webhook consumption | **Not wired** — `VerifyWebhook` exists and is tested; no route calls it |
| Provisional (unscreenable) address rescreen-on-first-deposit trigger | **Not wired** — the vault watcher noticing a first deposit from a provisional address is a designed trigger, not built; today an operator must rescreen manually |
| Transaction screening (beyond wallet/address screening) | **Not built** — the request shape was unconfirmed against Elliptic's API at design time, and guessing at an external contract was judged worse than not building it |

---

## Conventions used in this doc

- **Code references** are clickable links to the file (the function name is
  given alongside, since links resolve to the file, not the line). Links
  into `microvault-credit` point at the sibling credit module, which owns
  the admin process and the on-chain-writer/rescreen-sweep tickers.
- **"The source design"** refers to the Elliptic compliance integration doc
  kept in the knowledge vault (out of repo) — the phased design this module
  implements. It is not linked directly since it lives outside this
  repository.
