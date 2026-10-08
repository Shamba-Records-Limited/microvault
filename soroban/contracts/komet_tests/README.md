# Komet Tests

Komet-based test contracts used for property-based and formal verification of the Microvault Soroban contracts.

## Crates

| Path | Crate | Purpose |
|---|---|---|
| `test_token/` | `microvault-test-token` | Minimal token contract for komet test scenarios |
| `test_vault/` | `microvault-test-vault` | Vault test harness with komet tests and proof artifacts |
| `test_vault_probe/` | `microvault-test-vault-probe` | Probe contract for vault test scenarios |

## Prerequisites

- Recent stable Rust with the `wasm32v1-none` target: `rustup target add wasm32v1-none`
- [Stellar CLI](https://developers.stellar.org/docs/tools/developer-tools/cli/stellar-cli) (a recent release)

## Build

### Build with Stellar CLI

```bash
# Build all komet test crates
stellar contract build

# Build a single crate
stellar contract build -p microvault-test-token
stellar contract build -p microvault-test-vault
stellar contract build -p microvault-test-vault-probe
```

### Build with Cargo

```bash
cargo build --release --target wasm32v1-none --package microvault-test-token
cargo build --release --target wasm32v1-none --package microvault-test-vault
cargo build --release --target wasm32v1-none --package microvault-test-vault-probe
```

WASM artifacts land at `target/wasm32v1-none/release/<crate_name>.wasm`.

## Run Komet Tests

```bash
# Run all tests in a crate
cargo test --package microvault-test-vault
cargo test --package microvault-test-vault-probe

# Run a specific test
cargo test --package microvault-test-vault test_no_deposit_when_disallowed

# Run with output
cargo test --package microvault-test-vault -- --nocapture

# Run with log output
RUST_LOG=debug cargo test --package microvault-test-vault

# Run all komet tests across crates
cargo test --package microvault-test-vault --package microvault-test-vault-probe
```

## Proof Artifacts

### Generate proofs

```bash
# Run komet tests with proof output
cargo test --package microvault-test-vault -- --proof-output

# Output lands in proof-out/
```

### Inspect proof artifacts

```bash
# View KCFG proof
cat proof-out/test_no_deposit_when_disallowed/kcfg/kcfg.json

# View proof nodes
ls proof-out/test_no_deposit_when_disallowed/kcfg/nodes/
```

### Clean proof artifacts

```bash
rm -rf proof-out/
rm -rf .hypothesis/
```

## Lint & Format

```bash
cargo fmt --all
cargo clippy --all-targets --all-features -- -D warnings
```

## Notes

- `.hypothesis/` — Hypothesis test generation data (gitignored)
- `proof-out/` — KCFG proof output artifacts (gitignored)
- These are test-only crates (`publish = false`) and are not part of the main workspace build
- Intended for verification workflows, not production deployment
