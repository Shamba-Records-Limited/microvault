# Vault

SEP-0056 tokenized vault for USDC credit delegation. Depositors transfer USDC and receive share tokens (`mvUSDC`); the share-to-asset ratio appreciates as interest accrues on outstanding borrows. A designated treasury can borrow up to 80 % of the vault's assets on behalf of child accounts. Built on the [OpenZeppelin Stellar Contracts](https://docs.openzeppelin.com/stellar-contracts) library.

Source: [`soroban/contracts/vault/src/lib.rs`](../../soroban/contracts/vault/src/lib.rs).

## Constructor

```rust
pub fn __constructor(
    e: &Env,
    owner: Address,        // typically the TimelockController contract address
    guardian: Address,     // emergency-pause address
    asset: Address,        // underlying token (e.g. USDC)
    treasury: Address,     // credit-delegation wallet
    name: String,          // share token name, e.g. "MicroVault USDC"
    symbol: String,        // share token symbol, e.g. "mvUSDC"
)
```

Initial state set on construction: `MaxDeposit` and `MaxWithdraw` default to 1 000 000 USDC (with 7 decimals), `TotalBorrowed` to 0, `BorrowIndex` to `1.0` (WAD-scaled), `LastAccrualTime` to the current ledger timestamp, lock period to 0 (disabled).

## Functionality

The public surface is grouped below by who is expected to call each function. Auth requirements appear in the **Auth** column: `none` = anyone, `require_auth` = the named caller must sign, `[only_owner]` = owner-only (and therefore routed through the timelock), `[when_not_paused]` = blocked while paused.

### View Functions

| Function | Returns | Auth | Description |
|---|---|---|---|
| `treasury()` | `Result<Address, MicroVaultError>` | none | Current treasury address. Errors with `TreasuryNotSet` if unset. |
| `guardian()` | `Option<Address>` | none | Current guardian address, if any. |
| `get_max_deposit()` | `i128` | none | Per-transaction max deposit, in underlying-asset stroops. |
| `get_max_withdraw()` | `i128` | none | Per-transaction max withdrawal. |
| `total_borrowed()` | `i128` | none | Outstanding principal + accrued interest borrowed by treasury. |
| `available_liquidity()` | `i128` | none | Underlying-asset balance held by the vault, excluding borrows. |
| `total_managed_assets()` | `i128` | none | `available_liquidity + total_borrowed`. **Triggers interest accrual** before reading. |
| `utilization_rate()` | `i128` (WAD) | none | `total_borrowed / total_managed_assets` as 18-decimal fixed-point. Triggers accrual. |
| `get_borrow_index()` | `i128` (WAD) | none | Cumulative compound-interest index. `1e18` at deploy, monotonically increasing. Triggers accrual. |
| `borrow_apr()` | `i128` (WAD) | none | Current borrow APR derived from utilization via the kinked rate model. |
| `get_lock_period()` | `u64` | none | Configured deposit lock duration in **seconds**. `0` = disabled. |
| `get_unlock_time(user)` | `u64` | none | Ledger timestamp at which `user`'s shares become withdrawable. |
| `is_locked(user)` | `bool` | none | `true` if `user`'s shares are still locked. |
| `remaining_lock_time(user)` | `u64` | none | Seconds until `user`'s shares unlock; `0` if already unlocked. |

The vault also exposes the standard FungibleVault preview/convert helpers: `convert_to_shares`, `convert_to_assets`, `query_asset`, `total_assets`, `max_deposit`, `max_mint`, `max_withdraw`, `max_redeem`, `preview_deposit`, `preview_mint`, `preview_withdraw`, `preview_redeem`. Each respects the pause flag (returns `0` while paused) and the configured limits.

### Depositor Functions

All four are gated by `[when_not_paused]`. `withdraw` and `redeem` additionally check the per-user lock and the per-transaction max-withdraw cap.

| Function | Returns | Description |
|---|---|---|
| `deposit(assets, receiver, from, operator)` | `i128` (shares minted) | Pull `assets` of underlying from `from`, mint shares to `receiver`. Updates `receiver`'s lock with a weighted-average unlock time. |
| `mint(shares, receiver, from, operator)` | `i128` (assets used) | Mint exactly `shares` to `receiver`, pulling whatever assets are required. Updates lock. |
| `withdraw(assets, receiver, owner, operator)` | `i128` (shares burned) | Burn shares from `owner` to release exactly `assets` of underlying to `receiver`. |
| `redeem(shares, receiver, owner, operator)` | `i128` (assets returned) | Burn `shares` from `owner`, send the equivalent assets to `receiver`. |

### Treasury Functions

The treasury identity is stored on-chain; `treasury_caller` must equal that stored value and must sign (`require_auth`).

| Function | Auth | Description |
|---|---|---|
| `borrow(treasury_caller, recipient, amount)` | `require_auth(treasury_caller)`, `[when_not_paused]` | Transfer `amount` of underlying from the vault to the **treasury wallet** (the treasury then forwards to `recipient` off-chain or in a follow-up call). Accrues interest, then enforces the 80 % utilization cap and the available-liquidity check. |
| `repay(treasury_caller, amount)` | `require_auth(treasury_caller)`, `[when_not_paused]` | Pull `amount` of underlying from the treasury back into the vault. Accrues interest first. Errors with `RepayExceedsDebt` if `amount > total_borrowed`. |
| `accrue()` | none | Force interest accrual immediately. Useful for indexers and for keepers that want to crystallize interest before a snapshot. |
| `sweep_foreign_asset(current_treasury, token_to_recover, recipient, amount)` | `require_auth(current_treasury)` | Recover tokens accidentally sent to the vault. **Cannot sweep the underlying asset**, guarded by an explicit address check. |

### Admin Functions (Owner-Gated)

The owner is the TimelockController contract, so every call below is invoked indirectly via `execute_op` (see [Operations](./operations.md)). Direct calls signed by an EOA will fail the `[only_owner]` check.

| Function | Description |
|---|---|
| `set_max_deposit(new_limit)` | Update the per-transaction deposit cap. |
| `set_max_withdraw(new_limit)` | Update the per-transaction withdrawal cap. |
| `set_lock_period(new_period)` | Update the deposit lock duration in **seconds**. `0` disables locking. Existing user locks are not retroactively shortened or extended; only future deposits use the new value. |
| `set_guardian(new_guardian)` | Replace the guardian address. |
| `set_treasury(new_treasury)` | Replace the treasury address. Single call: there is no separate propose/execute/cancel because the timelock already provides that. |
| `upgrade(new_wasm_hash)` | Swap the contract WASM. State is preserved; see the storage-layout warning in [Critical notes](#critical-notes). |
| `unpause(caller)` | Resume operations after a pause. Only owner can unpause, even though guardian can pause. |

### Guardian Function

| Function | Auth | Description |
|---|---|---|
| `pause(caller)` | `require_auth(caller)` and `caller` must equal the owner **or** the guardian | Halt all `[when_not_paused]` operations immediately, with no timelock delay. Designed for rapid emergency response. |

## Events

Every event is published with `.publish(e)` and can be subscribed to with `stellar events --id $VAULT_ID …`.

| Event | Fields | Emitted when |
|---|---|---|
| `TreasuryUpdated` | `old_treasury`, `new_treasury` | `set_treasury` succeeds. |
| `GuardianUpdated` | `old_guardian`, `new_guardian` | `set_guardian` succeeds. |
| `MaxDepositUpdated` | `old_limit`, `new_limit` | `set_max_deposit` succeeds. |
| `MaxWithdrawUpdated` | `old_limit`, `new_limit` | `set_max_withdraw` succeeds. |
| `LockPeriodUpdated` | `old_period`, `new_period` | `set_lock_period` succeeds. |
| `UserLockUpdated` | `user`, `unlock_time` | A deposit or mint refreshes `user`'s unlock timestamp. |
| `VaultPaused` | `by` | `pause` succeeds. |
| `VaultUnpaused` | `by` | `unpause` succeeds. |
| `Borrowed` | `treasury`, `recipient`, `amount`, `total_borrowed` | `borrow` succeeds. |
| `Repaid` | `treasury`, `amount`, `total_borrowed` | `repay` succeeds. |
| `InterestAccrued` | `interest_amount`, `new_total_borrowed`, `utilization_rate` | Compound interest is added to `total_borrowed` (only when `interest_amount > 0`). |
| `ForeignAssetSwept` | `token`, `recipient`, `amount` | `sweep_foreign_asset` succeeds. |

The vault also emits the standard `Transfer` / `Mint` / `Burn` events from the FungibleToken trait on every share-balance change.

## Error Codes

Errors are returned as `MicroVaultError` (`#[contracterror]`, `#[repr(u32)]`). Decode them from the contract's `Error` field in the transaction result.

| Code | Name | Meaning |
|---|---|---|
| 1 | `Unauthorized` | Caller is not the configured treasury. |
| 2 | `CannotSweepUnderlyingAsset` | `sweep_foreign_asset` was called with the underlying asset. |
| 3 | `InvalidAmount` | Amount is `<= 0`, or total managed assets are 0 during a borrow. |
| 4 | `ExceedsMaxDeposit` | Deposit exceeds `MaxDeposit`. |
| 5 | `ExceedsMaxWithdraw` | Withdrawal exceeds `MaxWithdraw`. |
| 6 | `TreasuryNotSet` | Treasury storage entry is missing (should never happen post-construction). |
| 9 | `ExceedsUtilizationCap` | Borrow would push utilization above 80 %. |
| 10 | `InsufficientLiquidity` | Borrow exceeds `available_liquidity()`. |
| 11 | `RepayExceedsDebt` | Repayment exceeds outstanding `total_borrowed`. |
| 12 | `SharesLocked` | Withdraw or redeem attempted while user's shares are locked. |

Codes 7 (`TimelockNotExpired`) and 8 (`NoPendingUpdate`) existed in earlier versions of the manual treasury timelock and were removed when ownership migrated to the standalone TimelockController.

## Constants

| Constant | Value | Notes |
|---|---|---|
| `DECIMALS_OFFSET` | `6` | Share-token decimals = asset decimals + 6. With USDC's 7 decimals, shares have 13. Defends against the first-depositor inflation attack. |
| `DEFAULT_MAX_DEPOSIT` | `10_000_000_000_000` | 1 000 000 USDC at 7 decimals. Adjustable via `set_max_deposit`. |
| `DEFAULT_MAX_WITHDRAW` | `10_000_000_000_000` | 1 000 000 USDC at 7 decimals. Adjustable via `set_max_withdraw`. |
| `UTILIZATION_CAP` | `0.8e18` (WAD) | Hard cap at 80 % utilization on `borrow`. |
| `OPTIMAL_UTILIZATION` | `0.8e18` (WAD) | Kink point in the interest rate model. |
| `BASE_RATE` | `0.02e18` (WAD) | 2 % APR floor. |
| `SLOPE1` | `0.075e18` (WAD) | Per-100 %-utilization slope below the kink. |
| `SLOPE2` | `5.0e18` (WAD) | Per-100 %-utilization slope above the kink (steep penalty curve). |
| `SECONDS_PER_YEAR` | `31_536_000` | Used to convert APR to a per-second rate for compounding. |

The interest rate at utilization `u` (WAD-scaled) is:

```
u <= 0.8:  rate = BASE_RATE + u * SLOPE1
u >  0.8:  rate = BASE_RATE + 0.8 * SLOPE1 + (u - 0.8) * SLOPE2
```

At the kink (`u = 0.8`) the rate is `2 % + 0.8 * 7.5 % = 8 %` APR. Above 0.8 it climbs steeply: every 1 % of additional utilization adds 5 % APR.

## CLI Operations

All examples assume the env vars in [Operations § Prerequisites](./operations.md#prerequisites) are exported.

### Read vault state

```bash
stellar contract invoke --id $VAULT_ID --source deployer --network-passphrase "$NETWORK" -- treasury
stellar contract invoke --id $VAULT_ID --source deployer --network-passphrase "$NETWORK" -- total_borrowed
stellar contract invoke --id $VAULT_ID --source deployer --network-passphrase "$NETWORK" -- available_liquidity
stellar contract invoke --id $VAULT_ID --source deployer --network-passphrase "$NETWORK" -- utilization_rate
stellar contract invoke --id $VAULT_ID --source deployer --network-passphrase "$NETWORK" -- borrow_apr
stellar contract invoke --id $VAULT_ID --source deployer --network-passphrase "$NETWORK" -- get_borrow_index
```

### Approve and deposit USDC

```bash
# Approve the vault to pull 1 000 USDC from the depositor (USDC is 7 decimals).
stellar contract invoke \
  --id $USDC_ID \
  --source depositor \
  --network-passphrase "$NETWORK" \
  -- \
  approve \
  --from $DEPOSITOR \
  --spender $VAULT_ID \
  --amount 10000000000 \
  --expiration_ledger 3000000

# Deposit 1 000 USDC, receive shares.
stellar contract invoke \
  --id $VAULT_ID \
  --source depositor \
  --network-passphrase "$NETWORK" \
  -- \
  deposit \
  --assets 10000000000 \
  --receiver $DEPOSITOR \
  --from $DEPOSITOR \
  --operator $DEPOSITOR
```

### Withdraw / redeem

```bash
# Withdraw exactly 500 USDC by burning the corresponding shares.
stellar contract invoke \
  --id $VAULT_ID \
  --source depositor \
  --network-passphrase "$NETWORK" \
  -- \
  withdraw \
  --assets 5000000000 \
  --receiver $DEPOSITOR \
  --owner $DEPOSITOR \
  --operator $DEPOSITOR

# Redeem an exact share amount for the equivalent assets.
stellar contract invoke \
  --id $VAULT_ID \
  --source depositor \
  --network-passphrase "$NETWORK" \
  -- \
  redeem \
  --shares <SHARE_AMOUNT> \
  --receiver $DEPOSITOR \
  --owner $DEPOSITOR \
  --operator $DEPOSITOR
```

### Treasury borrow / repay

```bash
# Treasury borrows 100 USDC for a child account.
stellar contract invoke \
  --id $VAULT_ID \
  --source treasury \
  --network-passphrase "$NETWORK" \
  -- \
  borrow \
  --treasury_caller $TREASURY \
  --recipient <CHILD_ACCOUNT> \
  --amount 1000000000

# Treasury repays 100 USDC.
stellar contract invoke \
  --id $VAULT_ID \
  --source treasury \
  --network-passphrase "$NETWORK" \
  -- \
  repay \
  --treasury_caller $TREASURY \
  --amount 1000000000
```

### Guardian emergency pause

```bash
stellar contract invoke \
  --id $VAULT_ID \
  --source guardian \
  --network-passphrase "$NETWORK" \
  -- \
  pause \
  --caller $GUARDIAN
```

`unpause` is owner-only and therefore must go through the TimelockController. See [Operations § Common admin operations](./operations.md#common-admin-operations).

### Force interest accrual

```bash
stellar contract invoke --id $VAULT_ID --source deployer --network-passphrase "$NETWORK" -- accrue
```

## Critical Notes

- **Owner is the TimelockController.** Every `[only_owner]` function (`set_max_deposit`, `set_max_withdraw`, `set_lock_period`, `set_guardian`, `set_treasury`, `upgrade`, `unpause`) must be invoked via `schedule_op` → `execute_op` on the timelock. A direct call signed by a deployer key will trap on the owner check.
- **Lock period is in seconds, not ledgers.** `set_lock_period(604_800)` = 7 days. Do not confuse this with the TimelockController's `delay`, which is measured in ledger sequence counts.
- **Decimals offset is 6.** Share-token amounts are scaled by `1e13` (USDC's 7 + offset 6). Trust the `preview_deposit`/`preview_redeem` helpers for conversion. Do not multiply manually.
- **Utilization cap is hard-enforced at 80 %.** A borrow that would push utilization above the cap reverts with `ExceedsUtilizationCap`. The rate curve above 80 % is intentionally steep (slope-2 = 500 %) to discourage probing the cap.
- **`pause` is callable by owner OR guardian, but `unpause` is owner-only.** A compromised guardian can DoS the vault by pausing, but cannot unpause to re-enable theft. Resuming requires a timelocked governance round-trip.
- **`sweep_foreign_asset` cannot drain the underlying asset.** The function compares `token_to_recover` against `Vault::query_asset(e)` and rejects the call. Foreign tokens (e.g. an SEP-41 sent by mistake) can be recovered by the treasury.
- **Storage layout is fragile across upgrades.** Renaming `DataKey` variants, reordering them, or changing `#[contracttype]` field types will strand existing state on `upgrade`. Treat any change to `DataKey`, `MicroVaultError`, or any `#[contractevent]` struct as breaking. See [Operations § Upgrade flows](./operations.md#upgrade-flows) for the full upgrade procedure.
- **Interest is only crystallized on touch.** `total_borrowed` only increases on read paths that call `accrue_interest` (`total_managed_assets`, `utilization_rate`, `get_borrow_index`, `borrow_apr`, `borrow`, `repay`). Off-chain indexers that snapshot `total_borrowed()` directly between transactions will see a stale value: call `accrue` first, or read `total_managed_assets` instead.
- **`borrow` transfers to the treasury wallet, not directly to `recipient`.** The `recipient` field is recorded in the `Borrowed` event for off-chain accounting; the actual token transfer goes from the vault to the treasury, which is responsible for the second-leg transfer to the offramp provider wallet.
