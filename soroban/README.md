# MicroVault Soroban Contracts

SEP-56 Tokenized Vault implementation for USDC credit delegation on Stellar/Soroban.

## Quick Start

### Prerequisites

```bash
# Install Stellar CLI
cargo install --locked stellar-cli --features opt

# Generate deployer identity (testnet)
stellar keys generate deployer --network-passphrase "Test SDF Network ; September 2015" --fund
```

### Build

```bash
stellar contract build
# Output: target/wasm32-unknown-unknown/release/microvault_erc4626.wasm
```

### Deploy

```bash
stellar contract deploy \
    --wasm target/wasm32-unknown-unknown/release/microvault_erc4626.wasm \
    --source deployer \
    --network-passphrase "Test SDF Network ; September 2015" \
    -- \
    --owner $(stellar keys address deployer) \
    --asset $USDC_ID \
    --treasury $TREASURY \
    --name "MicroVault USDC" \
    --symbol "mvUSDC"
```

Save the returned contract ID:
```bash
export VAULT_ID="<CONTRACT_ID>"
```

### Update Deployed Contract

When you make changes to the contract code, you can upgrade in-place without losing state:

```bash
# 1. Build new WASM
stellar contract build

# 2. Install new WASM to the network (returns WASM hash)
stellar contract upload \
    --wasm target/wasm32-unknown-unknown/release/microvault_erc4626.wasm \
    --source-account deployer \
    --network-passphrase "Test SDF Network ; September 2015"
# Example output:
# 7f9e8d7c6b5a4e3f2d1c0b9a8e7d6c5b4a3f2e1d0c9b8a7e6d5c4b3a2f1e0d9c

# 3. Upgrade contract to new WASM (owner only)
stellar contract invoke \
    --id $VAULT_ID \
    --source deployer \
    --network-passphrase "Test SDF Network ; September 2015" \
    -- \
    upgrade \
    --new_wasm_hash 7f9e8d7c6b5a4e3f2d1c0b9a8e7d6c5b4a3f2e1d0c9b8a7e6d5c4b3a2f1e0d9c
```

**Upgrade notes:**
- Only the contract owner can call `upgrade`
- All storage data (deposits, borrows, treasury, etc.) is preserved
- New code takes effect immediately
- The WASM hash is a 32-byte hex string returned by `stellar contract upload`

## Environment Setup

```bash
export VAULT_ID="CAJFESYGJ2QVJT6HRBUDYIIK6WD4Z3D6HD5VFOXUZY34AJCAZKDDYQ46"
export USDC_ID="CBIELTK6YBZJU5UP2WWQEUCYKLPU6AUNZ2BQ4WWFEIE3USCIHMXQDAMA"
export TREASURY="GDLNA5YVLSV2JME7JNE4GF2LVLLOR6KFSKZT3WV76DVN4POEBTU33WSS"
export OWNER="GCUUFSK3P7YMFMYVA3L7LVQ7TCWJK7WUOFMXLX2HYSRCFB37ZGZD4CCX"
```

## CLI Commands

### View Functions

```bash
# Vault state
stellar contract invoke --source-account $OWNER --id $VAULT_ID --network-passphrase "Test SDF Network ; September 2015" -- treasury
stellar contract invoke --source-account $OWNER --id $VAULT_ID --network-passphrase "Test SDF Network ; September 2015" -- total_borrowed
stellar contract invoke --source-account $OWNER --id $VAULT_ID --network-passphrase "Test SDF Network ; September 2015" -- available_liquidity
stellar contract invoke --source-account $OWNER --id $VAULT_ID --network-passphrase "Test SDF Network ; September 2015" -- utilization_rate
stellar contract invoke --source-account $OWNER --id $VAULT_ID --network-passphrase "Test SDF Network ; September 2015" -- borrow_apr

# Lock status
stellar contract invoke --source-account $OWNER --id $VAULT_ID --network-passphrase "Test SDF Network ; September 2015" -- get_lock_period
stellar contract invoke --source-account $OWNER --id $VAULT_ID --network-passphrase "Test SDF Network ; September 2015" -- is_locked --user <ADDRESS>
stellar contract invoke --source-account $OWNER --id $VAULT_ID --network-passphrase "Test SDF Network ; September 2015" -- remaining_lock_time --user <ADDRESS>
```

### Depositor Operations

```bash
# Approve USDC spend (1000 USDC = 10000000000 with 7 decimals)
stellar contract invoke --id $USDC_ID --source-account depositor --network-passphrase "Test SDF Network ; September 2015" -- \
    approve --from $DEPOSITOR --spender $VAULT_ID --amount 10000000000 --expiration_ledger 3000000

# Deposit
stellar contract invoke --id $VAULT_ID --source-account depositor --network-passphrase "Test SDF Network ; September 2015" -- \
    deposit --assets 10000000000 --receiver $DEPOSITOR --from $DEPOSITOR --operator $DEPOSITOR

# Redeem
stellar contract invoke --id $VAULT_ID --source-account depositor --network-passphrase "Test SDF Network ; September 2015" -- \
    redeem --shares <AMOUNT> --receiver $DEPOSITOR --owner $DEPOSITOR --operator $DEPOSITOR
```

### Treasury Operations

```bash
# Borrow (sends to child account)
stellar contract invoke --id $VAULT_ID --source-account treasury --network-passphrase "Test SDF Network ; September 2015" -- \
    borrow --treasury_caller $TREASURY --recipient <CHILD_ACCOUNT> --amount 1000000000

# Repay
stellar contract invoke --id $VAULT_ID --source-account treasury --network-passphrase "Test SDF Network ; September 2015" -- \
    repay --treasury_caller $TREASURY --amount 1000000000

# Total borrowed
stellar contract invoke --id $VAULT_ID --source-account treasury --network-passphrase "Test SDF Network ; September 2015" -- \
    total_borrowed
```

### Admin Operations (Owner Only)

```bash
# Pause/Unpause
stellar contract invoke --id $VAULT_ID --source-account deployer --network-passphrase "Test SDF Network ; September 2015" -- pause --caller $OWNER
stellar contract invoke --id $VAULT_ID --source-account deployer --network-passphrase "Test SDF Network ; September 2015" -- unpause --caller $OWNER

# Set limits
stellar contract invoke --id $VAULT_ID --source-account deployer --network-passphrase "Test SDF Network ; September 2015" -- \
    set_max_deposit --new_limit 50000000000000

stellar contract invoke --id $VAULT_ID --source-account deployer --network-passphrase "Test SDF Network ; September 2015" -- \
    set_max_withdraw --new_limit 50000000000000

# Set lock period (in seconds, 0 = disabled)
stellar contract invoke --id $VAULT_ID --source-account deployer --network-passphrase "Test SDF Network ; September 2015" -- \
    set_lock_period --new_period 604800  # 7 days
```

## Contract Features

| Feature | Description |
|---------|-------------|
| Credit Delegation | Treasury borrows up to 80% utilization |
| Interest Model | Kinked rate: 2% base, steep above 80% |
| Lock Period | Configurable deposit lock (default: disabled) |
| Pause | Emergency stop for all operations |
| Timelock | 2-day delay for treasury changes |
