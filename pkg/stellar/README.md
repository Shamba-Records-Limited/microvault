# pkg/stellar

Go client for everything Microvault does on the Stellar network — both **classic**
operations (accounts, trustlines, payments) and **Soroban** smart-contract calls
(the Vault). One composite service fronts both.

```
host service
     │
     ▼
stellar.Service ──────────────┬───────────────────────────┐
     │                        │                           │
     ▼                        ▼                           ▼
classic.Service          soroban.Service             rpc.PollTransaction
(accounts, trustlines,   (vault borrow/repay,        (build to sign to
 payments, USDC sends)    views, admin)               submit to poll)
     │                        │
     └──────────┬─────────────┘
                ▼
        Stellar RPC ──▶ Stellar network
```

## Subpackages

| Package | What lives here |
|---|---|
| [`classic`](./classic/classic.go) | Off-chain Stellar ops: sponsored child-account creation, sponsored USDC trustlines, treasury USDC sends, trustline checks. |
| [`soroban`](./soroban/soroban.go) | Go client for the Vault contract: borrow/repay/accrue, read-only views, admin operations. |
| [`rpc`](./rpc/poll.go) | `PollTransaction` — waits for a submitted transaction to be applied to the ledger. |
| [`types`](./types/dto.go) | Request/response DTOs ([`dto.go`](./types/dto.go)), service errors, and contract-error mapping ([`errors.go`](./types/errors.go)). |
| [`testing`](./testing/mock_rpc_client.go) | `MockRPCClient` and deterministic test keys used by the table tests. |

## Building the service

`stellar.Service` embeds both `classic.Service` and `soroban.Service`, so a caller
holds one handle for on-chain and off-chain work. Build it with `NewService`
([`stellar_service.go`](./stellar_service.go)):

```go
svc := stellar.NewService(
    rpcClient,           // *rpcclient.Client
    networkPassphrase,   // e.g. "Test SDF Network ; September 2015"
    treasuryPrivateKey,  // signs and pays for classic + treasury operations
    adminPrivateKey,     // signs Soroban admin operations
    contractID,          // the Vault contract
    usdcIssuer,          // issuer of the USDC asset we transact in
)
```

## How it works

The concepts — the treasury sponsorship model, why child accounts hold zero XLM
and carry no trustline, the two-key split, and the build/sign/submit/poll path —
live in the package docs. Read them with `go doc` or on pkg.go.dev:

```bash
go doc ./pkg/stellar          # composite overview
go doc ./pkg/stellar/classic  # accounts, trustlines, USDC, sponsorship
go doc ./pkg/stellar/soroban  # the Vault contract client
```

## Full reference

See **[docs/stellar/client.md](../../docs/stellar/client.md)** — the complete
Go-client reference: sponsorship model, account lifecycle, moving USDC, the Vault
client, transaction confirmation, errors, configuration, and testing.

For the on-chain contract behaviour (deposit/withdraw, events, error codes) see
[docs/soroban/](../../docs/soroban/README.md).
