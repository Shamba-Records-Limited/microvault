# Stellar Go Client

Developer reference for [`pkg/stellar`](../../pkg/stellar), the Go client Microvault
uses to talk to the Stellar network. It covers two layers:

- **Classic operations**, creating accounts, establishing trustlines, and moving
  USDC, all through the [`classic`](../../pkg/stellar/classic/classic.go) package.
- **Soroban contract calls**, borrowing, repaying, reading state, and admin
  actions on the Vault, through the [`soroban`](../../pkg/stellar/soroban/soroban.go)
  package.

This doc is the **"how do I call it from Go"** view. For how the on-chain contract
itself behaves (deposit/withdraw math, event shapes, error codes), read
[docs/soroban](../soroban/README.md). The two overlap on purpose; they answer
different questions.

A quick map of the package lives in the [package README](../../pkg/stellar/README.md).

---

## What this package is

One composite [`stellar.Service`](../../pkg/stellar/stellar_service.go) embeds both
`classic.Service` and `soroban.Service`, so a caller holds a single handle for
on-chain and off-chain work. You construct it with `NewService`:

```go
svc := stellar.NewService(
    rpcClient,           // *rpcclient.Client
    networkPassphrase,   // network the keys and contract belong to
    treasuryPrivateKey,  // signs and pays for classic + treasury operations
    adminPrivateKey,     // signs Soroban admin operations
    contractID,          // the Vault contract
    usdcIssuer,          // issuer of the USDC asset we transact in
)
```

The composite service delegates each method to the right underlying service; there
is no logic in the wrapper itself.

---

## The treasury key and the sponsorship model

This is the load-bearing concept for the whole package, so it is worth reading
carefully.

### Two keys, two jobs

The service is built with two secret keys:

- **Treasury key**, signs and pays the fee for every **classic** operation
  (account creation, trustlines, USDC sends) and every **treasury** Vault
  operation (borrow, repay, accrue). It is parsed once per call with
  `keypair.MustParseFull` inside [`classic.go`](../../pkg/stellar/classic/classic.go).
- **Admin key**, signs **Soroban admin** operations (pause, set limits, set lock
  period). Kept separate so day-to-day treasury activity and privileged governance
  use different credentials.

Both keys are supplied by the host at construction. Whoever holds the treasury key
controls treasury funds and can sponsor accounts, so it is the most sensitive
secret the host manages.

### What sponsorship means

On Stellar, every account must hold a minimum XLM **base reserve** just to exist,
and each extra ledger entry it owns (such as a trustline) raises that reserve.
Sponsorship lets one account pay another account's reserves while the sponsored
account holds no XLM of its own.

Microvault leans on this so that **child accounts hold zero XLM**. They are created
with a starting balance of `"0"`, fully sponsored by the treasury. The pattern
appears wherever a child entry is created, wrap the reserve-raising operations
between a begin and an end marker:

```
BeginSponsoringFutureReserves (sponsor = treasury)
    CreateAccount / ChangeTrust   (the entries the treasury pays reserves for)
EndSponsoringFutureReserves   (source = child)
```

The treasury is the transaction source and pays the fee and the reserves. The child
still has to **sign** the transaction, because the sandwiched operations act on the
child's own account. The child authorises them, the treasury funds them. So a
sponsored transaction carries **two signatures**: treasury and child.

### Why children exist at all

Child accounts are **tracking and auditing markers**. They never hold USDC. When a
loan is funded, USDC is borrowed into the **treasury**, and the child address is
only recorded in the on-chain borrow event for attribution. Payouts then leave the
treasury, never a child. This is why children are created without a USDC
trustline: one they would never use would just burn a sponsored reserve.

---

## Child-account lifecycle

### Create, no trustline

`CreateSponsoredAccount` ([`classic.go`](../../pkg/stellar/classic/classic.go))
builds a sponsored `CreateAccount` (plus optional multisig `SetOptions`) and submits
it. The child comes into existence holding no XLM and **no USDC trustline**.

Multisig is optional: when enabled, the child can be configured so that only the
treasury signer carries weight (child weight `0`), making the treasury the sole
authoriser of anything the child later does.

### Add a trustline, only when needed

If a child ever genuinely needs to hold USDC, give it a trustline with
`EstablishSponsoredTrustline` ([`classic.go`](../../pkg/stellar/classic/classic.go)).
It bundles a sponsored `ChangeTrust` for USDC between the begin/end sponsorship
markers, signed by treasury and child. The treasury sponsors the trustline's
reserve, so the child still needs no XLM.

Because today USDC never lands in a child, this is **not** part of the normal
funding path. It exists for flows that deliberately move USDC through a child.

---

## Moving USDC

### `SendUSDC`, treasury to an external address (the live path)

`SendUSDC` ([`classic.go`](../../pkg/stellar/classic/classic.go)) sends USDC straight
from the treasury wallet to any external Stellar address, with a text memo. This is
the path actually used in production, for example by the YellowCard off-ramp
adapter ([`offramp_yellowcard.go`](../../pkg/mobile/ussd/adapters/offramp_yellowcard.go))
and the MoneyGram poller ([`poller.go`](../../pkg/services/mgpoller/poller.go)).

Before sending, the caller verifies the destination has a USDC trustline (see
below). Amounts are in **stroops** (`10_000_000` stroops = 1 USDC).

### `SponsoredPaymentTransaction`, child to a payee (not currently used)

`SponsoredPaymentTransaction` ([`classic.go`](../../pkg/stellar/classic/classic.go))
moves an asset out of a **child** account, with the treasury sponsoring the fee.
It has **no production caller** today. If it is ever wired up, the child must first
hold the asset, which means a trustline via `EstablishSponsoredTrustline` and a
balance, so it depends on the lifecycle step above.

### Trustline checks

A payment to an account without a trustline for the asset would fail on-chain, so
both send paths check first:

- `CheckUSDCTrustline` ([`classic.go`](../../pkg/stellar/classic/classic.go)) returns
  whether an address holds a USDC trustline. The off-ramp adapter calls it before a
  direct on-chain send to avoid a wasted transaction.
- Internally this uses `hasAssetTrustline`, which loads the account and inspects its
  balances. A missing trustline surfaces as
  [`ErrMissingTrustline`](../../pkg/stellar/types/errors.go).

Every trustline check in the codebase targets an **external destination**, never a
child account.

---

## The Soroban / Vault Go client

The [`soroban`](../../pkg/stellar/soroban/soroban.go) package wraps the Vault
contract. These methods build, sign, simulate, and submit contract invocations; the
**contract semantics** behind each one are documented in
[docs/soroban/vault.md](../soroban/vault.md).

| Method | Kind | Signed by |
|---|---|---|
| `BorrowFromVault` / `RepayToVault` / `AccrueInterest` | Treasury | treasury key |
| `PauseVault` / `UnpauseVault` / `SetMaxDeposit` / `SetMaxWithdraw` / `SetLockPeriod` | Admin | admin key |
| `GetTreasuryAddress`, `GetTotalBorrowed`, `GetAvailableLiquidity`, `GetUtilizationRate`, `GetBorrowAPR`, `IsUserLocked`, `IsPaused`, … | View (read-only) | none |

A note on admin operations: this client can call the Vault's admin functions
directly with the admin key, but in the deployed system those privileged changes
are routed through the TimelockController so they are publicly visible during a
delay window. See [docs/soroban/timelock-controller.md](../soroban/timelock-controller.md)
for that governance flow.

---

## Transaction submission and confirmation

Every state-changing call follows the same shape:

1. **Build** the transaction (operations, time bounds, fee).
2. **Sign** it, treasury and/or admin and/or child, depending on the operation.
3. **Submit** it to the RPC. The submission response carries an immediate status
   (`PENDING`, `ERROR`, `TRY_AGAIN_LATER`, `DUPLICATE`), which only says whether the
   network *accepted* the transaction, not whether it *succeeded*.
4. **Poll** with `PollTransaction` ([`poll.go`](../../pkg/stellar/rpc/poll.go)) until
   the transaction is applied to the ledger and reaches a final status.

`PollTransaction` is configured with `PollConfig` (`MaxAttempts`, `PollInterval`,
`Logger`); `DefaultPollConfig` polls up to 10 times at 1-second intervals. The
distinction that matters: a `PENDING` submission is **not** success, only a polled
`TransactionStatusSuccess` is. The poller returns typed errors for the failure
modes: failed-on-ledger, unknown status, context cancelled, and timeout.

---

## Errors

Two families of error live in [`types/errors.go`](../../pkg/stellar/types/errors.go),
re-exported from the top-level package in
[`stellar_service.go`](../../pkg/stellar/stellar_service.go):

- **Service-level errors**, transaction build/sign/submit failures, validation
  (`ErrInvalidStellarAddress`, `ErrMissingTrustline`), and status outcomes
  (`ErrTransactionRejected`, `ErrTransactionFailedOnLedger`, `ErrTransactionTimeout`,
  …). These are plain sentinel values; compare with `errors.Is`.
- **Contract errors**, `MapContractError` turns a numeric Soroban error code into a
  structured `ContractError{ Code, Name, Message, Source }`. It also classifies the
  error: `IsRetryable`, `IsUserError`, `IsAdminError`, `IsPauseError`, so callers can
  decide whether to retry, surface to a user, or alert. The code-to-meaning table is
  in [docs/soroban/vault.md](../soroban/vault.md).

---

## Configuration

`NewService` ([`stellar_service.go`](../../pkg/stellar/stellar_service.go)) takes
everything the client needs:

| Parameter | Purpose |
|---|---|
| `rpcClient` | Connection to a Stellar RPC endpoint (testnet or mainnet). |
| `networkPassphrase` | Identifies the network; must match the keys and contract. |
| `treasuryPrivateKey` | Treasury signer, classic operations and treasury Vault calls. |
| `adminPrivateKey` | Admin signer, privileged Vault operations. |
| `contractID` | The Vault contract address. |
| `usdcIssuer` | Issuer of the USDC asset the client transacts in. |

The same code runs against testnet and mainnet, only these inputs change. Deployed
testnet contract addresses are listed in [docs/soroban/README.md](../soroban/README.md).

---

## Testing

The [`testing`](../../pkg/stellar/testing/mock_rpc_client.go) package supplies a
`MockRPCClient` with per-method function hooks (`LoadAccountFunc`,
`SendTransactionFunc`, `GetTransactionFunc`, …) plus call tracking, and a set of
deterministic `TestKeys`. The table-driven tests in
[`classic_test.go`](../../pkg/stellar/classic/classic_test.go) and
[`poll_test.go`](../../pkg/stellar/rpc/poll_test.go) wire these hooks to drive
success and failure branches without touching a real network. To keep retry-path
tests fast, set a tiny `PollInterval` (the poll tests use a millisecond).

---

## Related

- [Package README](../../pkg/stellar/README.md): the in-code map of `pkg/stellar`.
- [docs/soroban](../soroban/README.md), on-chain contract behaviour, events, and
  error codes the Vault client calls into.
- [docs/offramp](../offramp/README.md), how the off-ramp adapters use `SendUSDC`
  and the trustline checks to deliver funds.
