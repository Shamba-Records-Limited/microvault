# Operations

End-to-end CLI cookbook for building, deploying, and operating the Microvault contracts. Every flow uses the [Stellar CLI](https://developers.stellar.org/docs/tools/developer-tools/cli/stellar-cli); see the upstream docs for installation and identity management.

For the contract APIs themselves, see [Vault](./vault.md) and [TimelockController](./timelock-controller.md).

## Prerequisites

Install the Stellar CLI:

```bash
cargo install --locked stellar-cli --features opt
```

Export the env vars referenced throughout this doc. Replace placeholders with values from your deployment:

```bash
# Network
export NETWORK="Test SDF Network ; September 2015"
export RPC_URL="https://soroban-testnet.stellar.org"

# Contract addresses (filled in after deploy)
export VAULT_ID="C…"
export TIMELOCK_ID="C…"
export USDC_ID="CBIELTK6YBZJU5UP2WWQEUCYKLPU6AUNZ2BQ4WWFEIE3USCIHMXQDAMA"  # testnet USDC

# Operator addresses (filled in once your identities exist)
export GUARDIAN="G…"        # emergency-pause address
export TREASURY="G…"        # credit-delegation wallet
```

Now create a funded deployer identity (the bootstrap owner / proposer / executor) and capture its public key:

```bash
stellar keys generate deployer --network-passphrase "$NETWORK" --rpc-url "$RPC_URL" --fund
export DEPLOYER=$(stellar keys address deployer)
```

> **`--source` takes an identity name, not an address.** Pass `--source deployer` (the name from `stellar keys generate`), not `--source $DEPLOYER` (the public key). The CLI cannot sign with a bare `G…` address and will fail with `Address cannot be used to sign G…`. The `$DEPLOYER` / `$TREASURY` / `$GUARDIAN` env vars are only used as **argument values** (e.g. `--proposer $DEPLOYER`, `--from $DEPOSITOR`), where addresses are required.

## Build

The contracts target `wasm32v1-none` (soroban-sdk v25). The simplest invocation lets the Stellar CLI handle the target:

```bash
cd soroban
stellar contract build
```

This produces optimized WASMs at:

- `target/wasm32v1-none/release/microvault_sep56.wasm`
- `target/wasm32v1-none/release/microvault_timelock_controller.wasm`

To build only one crate, pass `-p microvault-sep56` or `-p microvault-timelock-controller`.

## Deploy from Scratch

The deploy sequence transfers Vault ownership to the TimelockController so that all subsequent admin calls are timelocked. The deployer is the bootstrap owner / proposer / executor; harden the role assignments before going to production.

### 1. Deploy the Vault

```bash
stellar contract deploy \
  --wasm soroban/target/wasm32v1-none/release/microvault_sep56.wasm \
  --source deployer \
  --network-passphrase "$NETWORK" \
  --rpc-url "$RPC_URL" \
  -- \
  --owner "$DEPLOYER" \
  --guardian "$GUARDIAN" \
  --asset "$USDC_ID" \
  --treasury "$TREASURY" \
  --name "MicroVault USDC" \
  --symbol "mvUSDC"
# to returns the vault contract id
export VAULT_ID="<RETURNED_CONTRACT_ID>"
```

### 2. Deploy the TimelockController

`min_delay` is in **ledger sequence counts** (~5 s/ledger). 48 hours ≈ 34 560 ledgers; for testing, 120 (~10 minutes) is convenient.

```bash
stellar contract deploy \
  --wasm soroban/target/wasm32v1-none/release/microvault_timelock_controller.wasm \
  --source deployer \
  --network-passphrase "$NETWORK" \
  --rpc-url "$RPC_URL" \
  -- \
  --min_delay 120 \
  --proposers '["'$DEPLOYER'"]' \
  --executors '["'$DEPLOYER'"]' \
  --admin '"'$DEPLOYER'"'
# to returns the timelock contract id
export TIMELOCK_ID="<RETURNED_CONTRACT_ID>"
```

For self-administration (the controller is its own admin), pass `--admin null` instead of an address.

### 3. Transfer Vault ownership to the TimelockController

Ownership transfer follows the OpenZeppelin `Ownable` two-step pattern. Each leg has different auth requirements, which dictate whether it goes through the timelock:

| Call | Who calls | Why direct vs timelocked |
|---|---|---|
| `transfer_ownership(new_owner, live_until_ledger)` | **Deployer** (current owner), direct | Requires the **current** owner's auth. The deployer is still the owner at this point, so the call is signed directly by the deployer; no `schedule_op` involved. |
| `accept_ownership()` | **TimelockController** (pending owner), via `schedule_op` to `execute_op` | Requires the **pending** owner's auth. The pending owner is the controller, and a contract can only call other contracts via `execute_op`, so this leg must be timelocked. |

`live_until_ledger` is the ledger number until which `accept_ownership` can be called. Set it far enough in the future to comfortably outlast the timelock's `delay`. A value of `0` cancels a pending transfer.

```bash
# Step 3a: Deployer (current owner) initiates the transfer directly.
# live_until_ledger should be current_ledger + a healthy buffer (here ≈ 100k ledgers ≈ 6 days).
LIVE_UNTIL=$(($(stellar ledger latest --rpc-url "$RPC_URL" --network-passphrase "$NETWORK" --output json | grep -oE '"sequence":[0-9]+' | grep -oE '[0-9]+') + 100000))

stellar contract invoke \
  --id $VAULT_ID \
  --source deployer \
  --network-passphrase "$NETWORK" \
  --rpc-url "$RPC_URL" \
  -- \
  transfer_ownership \
  --new_owner "$TIMELOCK_ID" \
  --live_until_ledger "$LIVE_UNTIL"

# Step 3b: Schedule accept_ownership through the timelock.
# accept_ownership() takes no arguments, so --args is an empty list.
SALT=$(openssl rand -hex 32)
stellar contract invoke \
  --id $TIMELOCK_ID \
  --source deployer \
  --network-passphrase "$NETWORK" \
  --rpc-url "$RPC_URL" \
  -- \
  schedule_op \
  --target $VAULT_ID \
  --function accept_ownership \
  --args '[]' \
  --predecessor 0000000000000000000000000000000000000000000000000000000000000000 \
  --salt $SALT \
  --delay 120 \
  --proposer $DEPLOYER

# Step 3c: Wait for the delay, then execute accept_ownership through the timelock.
stellar contract invoke \
  --id $TIMELOCK_ID \
  --source deployer \
  --network-passphrase "$NETWORK" \
  --rpc-url "$RPC_URL" \
  -- \
  execute_op \
  --target $VAULT_ID \
  --function accept_ownership \
  --args '[]' \
  --predecessor 0000000000000000000000000000000000000000000000000000000000000000 \
  --salt $SALT \
  --executor '"'$DEPLOYER'"'
```

Verify the handover landed:

```bash
stellar contract invoke --id $VAULT_ID --source deployer --network-passphrase "$NETWORK" --rpc-url "$RPC_URL" -- get_owner
# to expected: "$TIMELOCK_ID"
```

After step 3c the vault's owner is the controller, and every `[only_owner]` call must flow through `schedule_op` to `execute_op`.

> **Why not schedule `transfer_ownership` through the timelock too?** `transfer_ownership` is gated by the **current** owner's auth (`enforce_owner_auth`). Routing it via `execute_op` would make the timelock the auth source, but the timelock is not yet the owner during bootstrap, so the call would fail. Only the second leg (`accept_ownership`) needs to be timelocked, because that one requires the *pending* owner's auth, and the pending owner *is* the timelock.

## Rotate Vault ownership (timelock to new owner)

If you later need to move ownership away from the controller, say to a multisig or a new governance contract, the auth roles invert and the directness of each leg flips with them:

| Call | Who calls | Why direct vs timelocked |
|---|---|---|
| `transfer_ownership(new_owner, live_until_ledger)` | **TimelockController** (current owner), via `schedule_op` to `execute_op` | Requires the **current** owner's auth. The current owner is now the timelock, so the call must go through `schedule_op` to wait to `execute_op`. |
| `accept_ownership()` | **New owner** (pending), direct (or via that owner's own governance) | Requires the **pending** owner's auth. If the new owner is an EOA or multisig signer, they call `accept_ownership` directly with their own key. |

```bash
# Set the destination and a generous live_until_ledger.
export NEW_OWNER="G…"   # e.g. a multisig account or a new governance contract
LIVE_UNTIL=$(($(stellar ledger latest --rpc-url "$RPC_URL" --network-passphrase "$NETWORK" --output json | grep -oE '"sequence":[0-9]+' | grep -oE '[0-9]+') + 100000))

# Step R1: Schedule transfer_ownership through the timelock.
SALT=$(openssl rand -hex 32)
stellar contract invoke \
  --id $TIMELOCK_ID \
  --source deployer \
  --network-passphrase "$NETWORK" \
  --rpc-url "$RPC_URL" \
  -- \
  schedule_op \
  --target $VAULT_ID \
  --function transfer_ownership \
  --args '[{"address":"'$NEW_OWNER'"},{"u32":'$LIVE_UNTIL'}]' \
  --predecessor 0000000000000000000000000000000000000000000000000000000000000000 \
  --salt $SALT \
  --delay 120 \
  --proposer $DEPLOYER

# Step R2: Wait for the delay, then execute. After this, pending_owner = $NEW_OWNER.
stellar contract invoke \
  --id $TIMELOCK_ID \
  --source deployer \
  --network-passphrase "$NETWORK" \
  --rpc-url "$RPC_URL" \
  -- \
  execute_op \
  --target $VAULT_ID \
  --function transfer_ownership \
  --args '[{"address":"'$NEW_OWNER'"},{"u32":'$LIVE_UNTIL'}]' \
  --predecessor 0000000000000000000000000000000000000000000000000000000000000000 \
  --salt $SALT \
  --executor '"'$DEPLOYER'"'

# Step R3: New owner accepts directly with their own identity (no timelock needed).
# Replace `new_owner_identity` with whatever stellar keys identity holds the new owner key.
stellar contract invoke \
  --id $VAULT_ID \
  --source new_owner_identity \
  --network-passphrase "$NETWORK" \
  --rpc-url "$RPC_URL" \
  -- \
  accept_ownership

# Verify.
stellar contract invoke --id $VAULT_ID --source deployer --network-passphrase "$NETWORK" --rpc-url "$RPC_URL" -- get_owner
# to expected: "$NEW_OWNER"
```

If the new owner is itself a contract (e.g. another timelock or a multisig contract), step R3 is replaced by that contract's own mechanism for issuing a call to `accept_ownership()`. For a fresh timelock, that means another `schedule_op` to `execute_op` round, mirroring step 3b/3c of the bootstrap.

To **abort** a pending transfer at any point before acceptance, the current owner calls `transfer_ownership(<anything>, 0)`. `live_until_ledger = 0` cancels the pending transfer per the OpenZeppelin spec. During bootstrap the deployer can do this directly; after rotation it must go through the timelock.

## Schedule to Execute Pattern

This is the canonical shape for every governance action against the vault. Substitute the function name and `--args` JSON for the specific operation; the scaffolding stays identical.

```bash
# 1. Generate a fresh salt and schedule the op.
SALT=$(openssl rand -hex 32)
stellar contract invoke \
  --id $TIMELOCK_ID \
  --source deployer \
  --network-passphrase "$NETWORK" \
  -- \
  schedule_op \
  --target $VAULT_ID \
  --function <FUNCTION_NAME> \
  --args '[<JSON_ENCODED_ARGS>]' \
  --predecessor 0000000000000000000000000000000000000000000000000000000000000000 \
  --salt $SALT \
  --delay 120 \
  --proposer $DEPLOYER
# to returns the operation_id

# 2. Wait for `delay` ledgers.

# 3. Execute with the SAME target/function/args/predecessor/salt.
stellar contract invoke \
  --id $TIMELOCK_ID \
  --source deployer \
  --network-passphrase "$NETWORK" \
  -- \
  execute_op \
  --target $VAULT_ID \
  --function <FUNCTION_NAME> \
  --args '[<JSON_ENCODED_ARGS>]' \
  --predecessor 0000000000000000000000000000000000000000000000000000000000000000 \
  --salt $SALT \
  --executor '"'$DEPLOYER'"'
```

Argument JSON encoding for the most common types:

| Rust type | JSON form | Example |
|---|---|---|
| `Address` | `{"address":"…"}` | `{"address":"GCUU…"}` |
| `BytesN<32>` | `{"bytes":"<hex>"}` | `{"bytes":"a93efbf0…"}` |
| `i128` | `{"i128":"…"}` | `{"i128":"50000000000000"}` |
| `u32` / `u64` | `{"u32":N}` / `{"u64":"N"}` | `{"u64":"604800"}` |
| `Symbol` | `{"symbol":"…"}` | `{"symbol":"executor"}` |
| `String` | `"…"` | `"MicroVault USDC"` |
| `Vec<T>` | `[…]` | `[{"address":"GCUU…"}]` |
| empty args | `[]` | `[]` |

Mismatched arg encoding fails at simulation time, not at execute, so the schedule call will surface the error before you commit to the delay.

### Shell quoting for `--args` (read this if you see "key must be a string")

`--args` takes a JSON value. Because bash performs **quote removal** on every argument before the CLI sees it, the form you write at the shell determines what bytes actually reach `stellar`. The single most common mistake is wrapping the JSON in double quotes:

```bash
# ❌ BROKEN: bash strips the inner " characters during quote removal.
--args "[{"i128":"10000000000"}]"
# What stellar receives: [{i128:10000000000}]
# Error: "Invalid JSON in argument 'args': key must be a string"
```

Bash sees `"[{"`, `i128`, `":"`, `10000000000`, `"}]"` as five adjacent words (three quoted + two unquoted) that concatenate to `[{i128:10000000000}]`. Every inner `"` is interpreted as a *closing* quote, never as a literal character.

Three forms that work:

```bash
# ✅ Single quotes outside. Single quotes are literal, every character
#    inside is preserved verbatim. Use this when no variables are
#    interpolated.
--args '[{"i128":"10000000000"}]'

# ✅ Double quotes outside with every inner " escaped. Necessary when
#    you must interpolate a shell variable inside the JSON.
--args "[{\"i128\":\"$AMOUNT\"}]"

# ✅ Concatenate single-quoted literal segments and unquoted variables.
#    Bash glues adjacent words with no separator. Used elsewhere in this
#    doc for the rotation flow.
--args '[{"address":"'$NEW_OWNER'"},{"i128":"'$AMOUNT'"}]'
```

For complex args you can also use `--args-file-path <file.json>` and skip shell quoting altogether.

The same rule applies to `--executor`, which expects a JSON-encoded string. `--executor '"'$DEPLOYER'"'` is form 3: a literal `"`, then `$DEPLOYER`, then another literal `"`. The result handed to the CLI is `"GCUU…"` (with quotes), which is valid JSON. A bare `--executor $DEPLOYER` passes `GCUU…` (no quotes) and is rejected.

## Upgrade Flows

Every contract upgrade is a two-call dance: `stellar contract upload` to put the new WASM on the ledger and get a hash, then a timelocked `upgrade` call.

### Vault upgrade

```bash
# 1. Upload the new vault WASM and capture the returned hash.
NEW_VAULT_WASM_HASH=$(stellar contract upload \
  --wasm soroban/target/wasm32v1-none/release/microvault_sep56.wasm \
  --source-account deployer \
  --network-passphrase "$NETWORK")

# 2. Schedule the upgrade through the timelock.
SALT=$(openssl rand -hex 32)
stellar contract invoke \
  --id $TIMELOCK_ID \
  --source deployer \
  --network-passphrase "$NETWORK" \
  -- \
  schedule_op \
  --target $VAULT_ID \
  --function upgrade \
  --args '[{"bytes":"'$NEW_VAULT_WASM_HASH'"}]' \
  --predecessor 0000000000000000000000000000000000000000000000000000000000000000 \
  --salt $SALT \
  --delay 120 \
  --proposer $DEPLOYER

# 3. After the delay, execute.
stellar contract invoke \
  --id $TIMELOCK_ID \
  --source deployer \
  --network-passphrase "$NETWORK" \
  -- \
  execute_op \
  --target $VAULT_ID \
  --function upgrade \
  --args '[{"bytes":"'$NEW_VAULT_WASM_HASH'"}]' \
  --predecessor 0000000000000000000000000000000000000000000000000000000000000000 \
  --salt $SALT \
  --executor '"'$DEPLOYER'"'
```

### TimelockController self-upgrade

The controller's own `upgrade` is `[only_admin]`. When the controller is its own admin, a self-targeted operation **cannot** go through `execute_op`: `execute_op` makes a cross-contract call into `target`, and when `target` is the controller itself that is a nested call back into the running contract. Soroban rejects it as re-entry (`Error(Context, InvalidAction)`, "Contract re-entry is not allowed").

Instead, **schedule the op against the controller, wait the delay, then call `upgrade` _directly_** (not `execute_op`). Because `upgrade` is `[only_admin]` and the admin is the contract itself, the direct call triggers the controller's `__check_auth`, which reconstructs the operation from the `OperationMeta` you supply, verifies the delay has passed via `set_execute_operation`, and marks it `Done`, all in a single (root) invocation, so there is no re-entry.

```bash
NEW_TIMELOCK_WASM_HASH=$(stellar contract upload \
  --wasm soroban/target/wasm32v1-none/release/microvault_timelock_controller.wasm \
  --source-account deployer \
  --network-passphrase "$NETWORK")

SALT=$(openssl rand -hex 32)
stellar contract invoke \
  --id $TIMELOCK_ID \
  --source deployer \
  --network-passphrase "$NETWORK" \
  -- \
  schedule_op \
  --target $TIMELOCK_ID \
  --function upgrade \
  --args '[{"bytes":"'$NEW_TIMELOCK_WASM_HASH'"}]' \
  --predecessor 0000000000000000000000000000000000000000000000000000000000000000 \
  --salt $SALT \
  --delay 34560 \
  --proposer $DEPLOYER

# After delay: call `upgrade` DIRECTLY (NOT execute_op).
stellar contract invoke \
  --id $TIMELOCK_ID \
  --source deployer \
  --network-passphrase "$NETWORK" \
  -- \
  upgrade \
  --new_wasm_hash $NEW_TIMELOCK_WASM_HASH
```

The direct `upgrade` call requires the controller's `__check_auth` authorization, whose signature is the `Vec<OperationMeta>` `[{ predecessor, salt, executor }]`, **not** an EOA key signature. The `stellar` CLI cannot synthesize a custom-account signature, so this leg's `SorobanAuthorizationEntry` (an `Address` credential for `$TIMELOCK_ID` carrying that `OperationMeta` ScVal) must be built with the SDK (JS/Rust) and attached to the transaction. If executors are configured, `__check_auth` *also* calls `executor.require_auth_for_args(...)`; that inner leg is a normal EOA signature the CLI signs with `--source deployer`. The `predecessor`, `salt`, and `args` in the `OperationMeta` must exactly match the `schedule_op` call, or the reconstructed operation hash won't match the `Ready` slot.

If the controller has an external admin instead of self-administration, the admin calls `upgrade` directly with its own key, no `execute_op`, no `__check_auth`, no re-entry concern; the `[only_admin]` check is the only gate.

## Common Admin Operations

Each row below shows just the `--target`, `--function`, and `--args` you slot into the [Schedule to Execute Pattern](#schedule--execute-pattern). Everything else (predecessor, salt, delay, proposer, executor) is unchanged.

| Operation | Target | Function | Args |
|---|---|---|---|
| Replace the treasury | `$VAULT_ID` | `set_treasury` | `[{"address":"<NEW_TREASURY>"}]` |
| Bump the per-tx deposit cap | `$VAULT_ID` | `set_max_deposit` | `[{"i128":"<NEW_LIMIT>"}]` |
| Bump the per-tx withdraw cap | `$VAULT_ID` | `set_max_withdraw` | `[{"i128":"<NEW_LIMIT>"}]` |
| Replace the guardian | `$VAULT_ID` | `set_guardian` | `[{"address":"<NEW_GUARDIAN>"}]` |
| Set the deposit lock period (seconds) | `$VAULT_ID` | `set_lock_period` | `[{"u64":"<SECONDS>"}]` |
| Resume after a pause | `$VAULT_ID` | `unpause` | `[{"address":"<TIMELOCK_ID>"}]` |
| Update the timelock's min_delay | `$TIMELOCK_ID` | `update_delay` | `[{"u32":<NEW_DELAY>}]` |

For the guardian's emergency `pause`, no scheduling is involved; the guardian calls the vault directly. See [Vault § Guardian emergency pause](./vault.md#guardian-emergency-pause).

## Cancellation

Any address with the `"canceler"` role (auto-granted to every proposer at construction) can move a `Waiting` or `Ready` operation back to `Unset`:

```bash
stellar contract invoke \
  --id $TIMELOCK_ID \
  --source deployer \
  --network-passphrase "$NETWORK" \
  -- \
  cancel_op \
  --operation_id <HEX_OPERATION_ID> \
  --canceller $DEPLOYER
```

Cancelling a `Done` operation traps. Once an op has been executed, the slot is consumed permanently; re-doing the same change requires a fresh salt and a new schedule.

## Verification

After every governance action, verify on-chain:

```bash
# Confirm the operation is Done.
stellar contract invoke --id $TIMELOCK_ID --source deployer --network-passphrase "$NETWORK" -- \
  is_operation_done --operation_id <OP_ID>

# Read the affected vault state to confirm the change took effect.
stellar contract invoke --id $VAULT_ID --source deployer --network-passphrase "$NETWORK" -- get_max_deposit
stellar contract invoke --id $VAULT_ID --source deployer --network-passphrase "$NETWORK" -- treasury
stellar contract invoke --id $VAULT_ID --source deployer --network-passphrase "$NETWORK" -- guardian
```

For deeper inspection, stream events:

```bash
stellar events \
  --network-passphrase "$NETWORK" \
  --id $VAULT_ID \
  --start-ledger <RECENT_LEDGER> \
  --output pretty
```

Watch for `TreasuryUpdated`, `MaxDepositUpdated`, `GuardianUpdated`, `VaultUnpaused`, etc., depending on the operation.

## Critical Notes

- **Delays are ledger sequence counts, not seconds.** Stellar produces roughly one ledger every 5 seconds. `min_delay = 34560` ≈ 48 hours; `min_delay = 120` ≈ 10 minutes. The Vault's `set_lock_period` is a separate axis and is in **seconds**. Do not confuse the two.
- **Always upload before scheduling an upgrade.** `schedule_op` for `upgrade` takes a 32-byte WASM hash, not a path. `stellar contract upload` puts the WASM on the ledger and prints the hash; without it there is nothing to point the schedule at.
- **Argument encoding is checked at simulation.** A typo in `--args` (wrong type tag, malformed JSON) surfaces during `schedule_op`, not at `execute_op`. This is good: you find out before paying the delay.
- **`--executor` requires double-quote shell wrapping.** The CLI treats it as a JSON-encoded string: `--executor '"GCUU…"'`. A bare `--executor $DEPLOYER` is rejected.
- **Salts are single-use per `(target, function, args, predecessor)` tuple.** After `execute_op` marks the slot `Done`, scheduling the same tuple again with the same salt traps. Generate a fresh salt every time with `openssl rand -hex 32`.
- **Predecessor `0x00…00` (32 zero bytes) means "no dependency".** Chain operations by passing a prior op id as `predecessor` if you want the timelock to enforce ordering across multiple queued ops.
- **The Vault's `unpause` arg expects the caller (owner = timelock).** Pass `[{"address":"<TIMELOCK_ID>"}]`, not the deployer's address. The vault checks `caller.require_auth()` and the auth comes from the controller via `__check_auth` during `execute_op`.
- **Storage-breaking changes silently strand state on upgrade.** Soroban derives ledger keys from the structural hash of `#[contracttype]` types. Renaming a `DataKey` variant, reordering enum variants, changing a struct field type, or switching storage tier (instance/persistent/temporary) all change the key, and the new code can no longer read pre-upgrade state. Do not bump multiple `stellar-*` crates in a single PR. Pin exact versions (`= 0.7.1`, not `^0.7.1`) in `Cargo.toml`. Run a smoke test that invokes every metadata view and balance read after every upgrade; if any call traps, roll back immediately.
- **Self-administered timelock has no escape hatch.** When the controller is its own admin, lowering `min_delay` or upgrading the controller itself requires a full timelocked round-trip. Plan delay values for the worst case you can tolerate during an incident.
- **Cancellation cannot rescue a `Done` op.** `cancel_op` works on `Waiting` and `Ready` states; once executed, the change is on-chain. Mistakes are reversed by scheduling a corrective op, not by reverting the previous one.
