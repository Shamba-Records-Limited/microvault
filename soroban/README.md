# Microvault Soroban Contracts

![Status](https://img.shields.io/badge/status-testnet%20only-yellow)
![Audit](https://img.shields.io/badge/audit-not%20yet-red)
![Soroban SDK](https://img.shields.io/badge/soroban--sdk-25.3.1-blue)
![License](https://img.shields.io/badge/license-AGPL--3.0-green)

> **Status: testnet only, not audited.** Do not deploy to mainnet without an independent security review.

SEP-0056 tokenized vault for USDC credit delegation on Stellar, with an OpenZeppelin implementation of TimelockController for time-delayed governance.

## Workspace Layout

| Path | Crate | Purpose |
|---|---|---|
| `contracts/vault/` | `microvault-sep56` | SEP-56 tokenized vault: deposits, share token, treasury credit delegation, kinked-rate interest model |
| `contracts/timelock-controller/` | `microvault-timelock-controller` | Time-delayed governance controller; owns the vault |

## Prerequisites

- Recent stable Rust with the `wasm32v1-none` target: `rustup target add wasm32v1-none`
- [Stellar CLI](https://developers.stellar.org/docs/tools/developer-tools/cli/stellar-cli) (a recent release)
- A funded testnet identity for deploys; see [`docs/soroban/operations.md` § Prerequisites](../docs/soroban/operations.md#prerequisites)

## Build

```bash
stellar contract build
```

WASM artifacts land at:

- `target/wasm32v1-none/release/microvault_sep56.wasm`
- `target/wasm32v1-none/release/microvault_timelock_controller.wasm`

To build a single crate, pass `-p microvault-sep56` or `-p microvault-timelock-controller`.

## Test

```bash
cargo test
```

Runs the integration tests in both crates.

## Lint & Format

```bash
cargo fmt --all
cargo clippy --all-targets --all-features -- -D warnings
```

## Deployed Contracts (Testnet)

| Contract | Address |
|---|---|
| Vault | [`CDZVKARLUCAYYIV2TPSR6PLWLETPS4TYE2QXVXSQT27QNFYE3GEE5IS5`](https://stellar.expert/explorer/testnet/contract/CDZVKARLUCAYYIV2TPSR6PLWLETPS4TYE2QXVXSQT27QNFYE3GEE5IS5) |
| TimelockController | [`CAL3RYRW6MJ2BMKP2J7G47BPBFWKZ7K2BMRG2EXZWQI5ZZAHFPM7NO7B`](https://stellar.expert/explorer/testnet/contract/CAL3RYRW6MJ2BMKP2J7G47BPBFWKZ7K2BMRG2EXZWQI5ZZAHFPM7NO7B) |

## Documentation

For integration, deploy, and upgrade workflows, see [`../docs/soroban/`](../docs/soroban/README.md):

- [Vault](../docs/soroban/vault.md) — contract reference (functionality, events, errors, constants)
- [TimelockController](../docs/soroban/timelock-controller.md) — governance reference (operation lifecycle, roles)
- [Operations](../docs/soroban/operations.md) — CLI cookbook (build, deploy, upgrade, schedule to execute, ownership flows)

## SDK Versions

soroban-sdk 25.3.1, stellar-* 0.7.1. Pin exact versions when bumping (`= 0.7.1`, not `^0.7.1`); structural-hash storage keys make pre-1.0 dependency upgrades risky. See [Operations § Critical Notes](../docs/soroban/operations.md#critical-notes) for the upgrade-safety rules.

## Contributing & License

Contributions follow the repo-level [CONTRIBUTING](../README.md#contributing) flow (CLA required). Licensed under AGPL-3.0; see [`LICENSE`](../LICENSE) at the repo root.
