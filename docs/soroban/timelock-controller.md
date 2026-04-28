# TimelockController

OpenZeppelin-style governance contract that enforces a time delay between proposing and executing privileged operations. Designed to be set as the [Vault](./vault.md)'s owner so every admin call is publicly visible during the delay window before it takes effect. Built on the [`stellar-governance::timelock`](https://docs.openzeppelin.com/stellar-contracts) primitives with role-based access control and a `CustomAccountInterface` for self-administration.

Source: [`soroban/contracts/timelock-controller/src/lib.rs`](../../soroban/contracts/timelock-controller/src/lib.rs).

## Operation Lifecycle

Every governance action goes through these states:

```
        schedule_op             delay elapsed              execute_op
Unset ──────────────▶ Waiting ─────────────────▶ Ready ─────────────▶ Done
                         │
                         │ cancel_op (canceler role)
                         ▼
                       Unset
```

| State | Meaning |
|---|---|
| `Unset` | No record. Either never scheduled, or already executed and cleared. |
| `Waiting` | Scheduled, but the delay has not elapsed. |
| `Ready` | Delay elapsed; eligible for `execute_op`. |
| `Done` | Already executed. The slot is consumed and reusing the same `(target, function, args, predecessor, salt)` will trap. |

Operations are identified by a 32-byte ID computed as `keccak256(target ‖ function ‖ args ‖ predecessor ‖ salt)`. You can compute it off-chain with `hash_operation` before calling `schedule_op`.

## Roles

Roles are stored as `Symbol` constants and managed by the `stellar-access::access_control` module. Spelling matters: use the exact symbols below in CLI calls and trait checks.

| Role | Symbol | Granted to | Permissions |
|---|---|---|---|
| Proposer | `"proposer"` | Addresses passed in `proposers` at construction | Call `schedule_op` to queue new operations. |
| Canceler | `"canceler"` (one `l`) | Auto-granted to every proposer at construction | Call `cancel_op` on operations in the `Waiting` state. |
| Executor | `"executor"` | Addresses passed in `executors` at construction | Call `execute_op` on `Ready` operations. **If `executors` is empty, anyone can execute.** |
| Admin | — | `admin` argument at construction (defaults to the contract itself) | Call `update_delay` and `upgrade`. When the contract is its own admin, these calls must themselves be timelocked. |

The on-chain symbol is `canceler` with a single `l` to match the OpenZeppelin Stellar Contracts naming. CLI calls and role checks must use that spelling exactly.

## Constructor

```rust
pub fn __constructor(
    e: &Env,
    min_delay: u32,                  // ledger sequence counts, NOT seconds
    proposers: Vec<Address>,
    executors: Vec<Address>,
    admin: Option<Address>,          // None → self-administered (contract is its own admin)
)
```

`min_delay` is the floor enforced on every `schedule_op` call: any per-operation `delay` smaller than this value is rejected. **The unit is ledger sequence counts.** Stellar produces roughly one ledger every 5 seconds, so 48 hours ≈ 34 560 ledgers. This semantic changed in soroban-sdk v25; earlier versions used seconds. Do not copy values from older docs without converting.

If `admin` is `None`, the contract sets itself as admin. From that point on, changing the delay or upgrading the WASM requires scheduling an operation against this contract and waiting for the delay. There is no instant escape hatch.

## Functionality

### Scheduling

| Function | Auth | Description |
|---|---|---|
| `schedule_op(target, function, args, predecessor, salt, delay, proposer) -> BytesN<32>` | `[only_role(proposer)]`, `proposer` must sign | Queue an operation. Returns the operation ID. |

`predecessor` is another operation ID that must be in the `Done` state before this one can execute. Pass `BytesN<32>` of all-zero bytes (`0x00…00`) when there is no dependency. This is the conventional "no predecessor" sentinel.

`salt` must be unique for the `(target, function, args, predecessor)` tuple. Reusing a salt for an identical tuple computes the same operation ID, which collides with the existing `Done` slot and traps. Generate one with `openssl rand -hex 32` or any 32-byte cryptographically secure pseudorandom number generator (CSPRNG).

`delay` must be `>= min_delay`. The op enters `Waiting` and transitions to `Ready` after `delay` ledgers from the schedule ledger.

### Execution

| Function | Auth | Description |
|---|---|---|
| `execute_op(target, function, args, predecessor, salt, executor: Option<Address>) -> Val` | If executors are configured: `executor` must hold the `"executor"` role and sign. If no executors: anyone. | Invoke `target.function(args)` via the timelock. Verifies the op is in `Ready` and the predecessor is `Done`. |

`execute_op` is **atomic** with the call to the target: the target function runs in the same transaction, returning its `Val`. Soroban auth flows through the `CustomAccountInterface.__check_auth` defined on the controller, which:

1. Verifies every auth context targets this controller.
2. If executors are configured, requires the `executor` argument to hold the `"executor"` role and sign.
3. Reconstructs the `Operation` and marks it as executed via `set_execute_operation`, which validates the delay.

### Cancellation

| Function | Auth | Description |
|---|---|---|
| `cancel_op(operation_id, canceller)` | `[only_role(canceller, "canceler")]`, `canceller` must sign | Move an operation back to `Unset`. Only callable while the op is `Waiting` (and `Ready`, per the underlying `stellar-governance` primitive); cancelling a `Done` op traps. |

### Admin

Both functions are `[only_admin]`. Under self-administration (the default), the admin is the contract itself, so calling these directly will trap; you must `schedule_op` against this contract with `function = update_delay` (or `upgrade`) and route through the normal flow.

| Function | Description |
|---|---|
| `update_delay(new_delay)` | Update `min_delay`. New value applies to subsequent schedules; in-flight operations keep their original delay. |
| `upgrade(new_wasm_hash)` | Swap the controller's WASM. Storage layout warnings apply identically to the vault. See [Operations § Critical notes](./operations.md#critical-notes). |

### Views

All read-only and free of role checks.

| Function | Returns | Description |
|---|---|---|
| `get_min_delay()` | `u32` | Current minimum delay (in ledger counts). |
| `hash_operation(target, function, args, predecessor, salt)` | `BytesN<32>` | Compute the operation ID for the given parameters. |
| `get_operation_state(operation_id)` | `OperationState` | `Unset`, `Waiting`, `Ready`, or `Done`. |
| `is_operation_pending(operation_id)` | `bool` | `true` if `Waiting` or `Ready`. |
| `is_operation_ready(operation_id)` | `bool` | `true` if delay has passed and op can be executed. |
| `is_operation_done(operation_id)` | `bool` | `true` if already executed. |

The contract also exposes the standard `AccessControl` trait surface (role granting, role revoking, role-member queries) inherited from `stellar-access`.

## Errors

The contract does not define its own error enum. Errors propagate from `stellar-governance::timelock::TimelockError` (insufficient delay, op not pending, predecessor not done, etc.) and `stellar-access::access_control` (missing role, missing admin). See the OpenZeppelin docs for the full list. The `Unauthorized` variant of `TimelockError` is what `__check_auth` raises when an auth context targets the wrong contract.

## CLI Operations

The schedule → wait → execute pattern below is the canonical way to perform any governance action. The same shape applies whether you are bumping a deposit cap, replacing the treasury, unpausing the vault, or upgrading either contract's WASM. See [Operations](./operations.md) for end-to-end deploy and upgrade flows.

All examples assume the env vars in [Operations § Prerequisites](./operations.md#prerequisites) are exported.

### Schedule a vault admin call

Schedule a `set_max_deposit(500_000_000_000_000)` (50 M USDC) call against the vault, with the minimum delay:

```bash
stellar contract invoke \
  --id $TIMELOCK_ID \
  --source deployer \
  --network-passphrase "$NETWORK" \
  -- \
  schedule_op \
  --target $VAULT_ID \
  --function set_max_deposit \
  --args '[{"i128":"500000000000000"}]' \
  --predecessor 0000000000000000000000000000000000000000000000000000000000000000 \
  --salt $(openssl rand -hex 32) \
  --delay 120 \ # 10 mins/600 secs
  --proposer $DEPLOYER
# → returns the 32-byte operation_id, e.g.
# 8c2f4c4e1aab3f0c2d3e7c5d6f9b1a2e7c8d4b3f5e1a0d9c8b7a6e5d4c3b2a1f
```

Save the operation id and the salt; you will need both to execute later.

### Wait for the delay

Poll `is_operation_ready` until it returns `true`:

```bash
stellar contract invoke \
  --id $TIMELOCK_ID \
  --source deployer \
  --network-passphrase "$NETWORK" \
  -- \
  is_operation_ready \
  --operation_id 8c2f4c4e1aab3f0c2d3e7c5d6f9b1a2e7c8d4b3f5e1a0d9c8b7a6e5d4c3b2a1f
```

### Execute the scheduled call

Pass the **same** `target`, `function`, `args`, `predecessor`, and `salt` as you did to `schedule_op`. The `executor` argument is a JSON-encoded string, which requires the double-quote shell wrap shown below.

```bash
stellar contract invoke \
  --id $TIMELOCK_ID \
  --source deployer \
  --network-passphrase "$NETWORK" \
  -- \
  execute_op \
  --target $VAULT_ID \
  --function set_max_deposit \
  --args '[{"i128":"500000000000000"}]' \
  --predecessor 0000000000000000000000000000000000000000000000000000000000000000 \
  --salt <SAME_SALT_AS_SCHEDULE> \
  --executor '"'$DEPLOYER'"'
```

### Cancel a scheduled operation

```bash
stellar contract invoke \
  --id $TIMELOCK_ID \
  --source deployer \
  --network-passphrase "$NETWORK" \
  -- \
  cancel_op \
  --operation_id 8c2f4c4e1aab3f0c2d3e7c5d6f9b1a2e7c8d4b3f5e1a0d9c8b7a6e5d4c3b2a1f \
  --canceller $DEPLOYER
```

### Inspect operation state

```bash
stellar contract invoke --id $TIMELOCK_ID --source deployer --network-passphrase "$NETWORK" -- \
  get_operation_state --operation_id <OP_ID>

stellar contract invoke --id $TIMELOCK_ID --source deployer --network-passphrase "$NETWORK" -- \
  is_operation_pending --operation_id <OP_ID>

stellar contract invoke --id $TIMELOCK_ID --source deployer --network-passphrase "$NETWORK" -- \
  get_min_delay
```

## Critical Notes

- **Delay is in ledger sequence counts, not seconds.** `min_delay` and per-op `delay` are u32 ledger counts. ~5 s/ledger means 1 hour ≈ 720, 24 h ≈ 17 280, 48 h ≈ 34 560. Misreading this as seconds is a real footgun introduced by the soroban-sdk v25 migration.
- **Salts must be unique per operation.** The operation ID is `keccak256(target ‖ function ‖ args ‖ predecessor ‖ salt)`. Reusing a salt with the same other inputs collides with the previous `Done` slot and the next `schedule_op` traps. Always generate fresh randomness; `openssl rand -hex 32` is the recommended source.
- **Predecessor `0x00…00` (32 zero bytes) means "no dependency".** Chain operations by passing a prior op ID as `predecessor` to enforce execution ordering. `execute_op` requires the predecessor to be `Done`.
- **`--executor` is a JSON-encoded string.** Wrap with double quotes inside single quotes in shell: `--executor '"GCUU…"'`. Without the inner double quotes the CLI rejects the value as a non-string.
- **The role symbol is `canceler` (one `l`).** The English word has two; the on-chain symbol has one. Mismatching this in role-grant or role-check calls produces silent permission failures.
- **No executors configured = open execution.** If you deploy with `executors: []`, anyone can call `execute_op` on a `Ready` operation. This is sometimes intentional (e.g. for fully decentralized execution after the delay), but combined with proposers-only-as-cancelers it means a single proposer compromise plus the configured delay is enough for an attacker to drain owner-gated functions. Default to a curated executor set for production.
- **Self-administration leaves no instant escape hatch.** When `admin = None`, `update_delay` and `upgrade` on the controller itself can only run via a full timelocked round-trip. Plan deploys carefully: you cannot shorten the delay or upgrade in an emergency without first scheduling and waiting.
- **Same `(target, function, args, predecessor, salt)` is single-shot.** After `execute_op` marks the slot `Done`, the same tuple cannot be scheduled again. This is by design — it is what makes the operation ID a stable handle — but it means rerunning an operation requires a fresh salt.
- **`__check_auth` only validates contexts that target this controller.** Any auth context routed elsewhere panics with `TimelockError::Unauthorized`. This is what prevents a scheduled op against the vault from also being usable as authorization for a different contract in the same transaction.
