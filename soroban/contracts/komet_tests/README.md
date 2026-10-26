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

`komet_tests/` is its own Cargo workspace, kept out of the contracts workspace so `cargo test` there never needs these wasm-only crates. Its `soroban-sdk` pin must match `soroban/Cargo.toml`. `test_vault` and `test_vault_probe` import the vault wasm from `soroban/target`, so build the contracts first:

```bash
(cd ../.. && stellar contract build)
```

`.cargo/config.toml` links with `--allow-undefined`, because the `kasmer_*` cheatcodes are host imports that only Komet provides.

### Build with Stellar CLI

```bash
# Build all komet test crates (run from komet_tests/)
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

Komet builds the harness and the contracts listed in `kasmer.json` itself, so run it from the harness directory:

```bash
cd test_vault
komet test                                   # fuzz every test_* property (100 examples each)
komet test --id test_no_deposit_when_disallowed
komet test --max-examples 500
komet prove run                              # symbolic proof
```

### Tooling compatibility (Komet 0.1.89)

| Issue | Cause | Workaround |
|---|---|---|
| `KeyError: 'type_'` | stellar-cli 28 renamed the interface JSON key `type_` to `type` | Wrap `stellar` so `contract info interface --output json` renames the key back |
| `ParseError: Invalid section id: 0xc` | The CLI's wasm optimizer emits a DataCount section that Komet's parser rejects | Build with `stellar contract build --optimize=false` |
| Every vault-touching property fails on its first example | Vault built on soroban-sdk 28 / OZ 0.8 imports host functions Komet doesn't implement (`sparse_map_*_linear_memory`, `u256/i256_checked_*`, `u256_rem_euclid`) | None; needs a Komet release with protocol 28 support |

## Proof Artifacts

### Generate proofs

```bash
# Run komet proofs (from test_vault/)
komet prove run

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
