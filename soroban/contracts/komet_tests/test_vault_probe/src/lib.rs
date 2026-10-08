#![no_std]
//! Komet property tests for the Microvault vault, testnet-branch additions:
//!   A. allow/disallow depositor gates deposits and share transfers
//!   B. exit deadline freezes a disallowed depositor's shares

use soroban_sdk::{
    contract, contractimpl, symbol_short, Address, Bytes, Env, IntoVal, String, Symbol,
};

mod komet;

mod vault {
    soroban_sdk::contractimport!(
        file = "../../target/wasm32v1-none/release/microvault_sep56.wasm"
    );
}
mod token {
    soroban_sdk::contractimport!(
        file = "../../target/wasm32v1-none/release/microvault_test_token.wasm"
    );
}

const VAULT_KEY: Symbol = symbol_short!("vault");
const TOKEN_KEY: Symbol = symbol_short!("token");
const OWNER_KEY: Symbol = symbol_short!("owner");
const ROLE_KEY: Symbol = symbol_short!("role");

// The post-revocation grace (default 30 days) is never hard-coded below:
// properties read it with the `withdraw_grace_period()` view.

/// Fixed deposit used by the time-focused properties (well under MaxDeposit).
const A: i128 = 1_000_000;

// ───────────────────────────────────────────────────────────────────────
// Helpers (free functions — not exported, Komet only runs init/test_*)
// ───────────────────────────────────────────────────────────────────────

/// 32-byte label for `kasmer_address_from_bytes` (prefix must be ≤ 32 bytes).
fn label(env: &Env, prefix: &[u8]) -> Bytes {
    let mut b = [b'_'; 32];
    b[..prefix.len()].copy_from_slice(prefix);
    Bytes::from_array(env, &b)
}

fn account(env: &Env, prefix: &[u8]) -> Address {
    komet::address_from_bytes(env, &label(env, prefix), false)
}

/// Fuzzed depositor identity. The leading tag byte guarantees two addresses
/// built with different tags can never collide, whatever the seeds.
fn user_addr(env: &Env, tag: u8, seed: u64) -> Address {
    let mut b = [b'_'; 32];
    b[0] = tag;
    b[1..9].copy_from_slice(&seed.to_be_bytes());
    komet::address_from_bytes(env, &Bytes::from_array(env, &b), false)
}

/// Fuzzed deposit amount: always positive, always under MaxDeposit.
fn amt(raw: i128) -> i128 {
    raw.rem_euclid(100_000_000_000).max(1)
}

/// Fuzzed timestamp in a sane, overflow-free range.
fn fuzz_time(raw: u64) -> u64 {
    (raw % 4_000_000_000).max(1_600_000_000)
}

fn vault_client(env: &Env) -> vault::Client {
    let addr: Address = env.storage().instance().get(&VAULT_KEY).unwrap();
    vault::Client::new(env, &addr)
}

fn token_client(env: &Env) -> token::Client {
    let addr: Address = env.storage().instance().get(&TOKEN_KEY).unwrap();
    token::Client::new(env, &addr)
}

fn role(env: &Env) -> Address {
    env.storage().instance().get(&ROLE_KEY).unwrap()
}

/// Owner sets the compliance role and switches enforcement on.
fn enable_allowlist(env: &Env) {
    let v = vault_client(env);
    v.set_compliance_role(&role(env));
    v.set_allowlist_enforced(&true);
}

#[contract]
pub struct TestVaultContract;

#[contractimpl]
impl TestVaultContract {
    /// Called by Komet once, with the wasm hashes listed in kasmer.json,
    /// in order: (vault_hash, token_hash).
    pub fn init(env: Env, vault_hash: Bytes, token_hash: Bytes) {
        // 1. Underlying test token (no constructor → plain create).
        let token = komet::create_contract(&env, &label(&env, b"token"), &token_hash);

        // 2. Actor accounts (mirrors the native tests: owner, guardian, ...).
        let owner = account(&env, b"owner");
        let guardian = account(&env, b"guardian");
        let treasury = account(&env, b"treasury");
        let compliance = account(&env, b"complnce");

        // 3. The vault, whose 6-argument __constructor must run before any
        //    property can touch it.
        //
        //    Komet's semantics models exactly one contract-creation path: the
        //    `kasmer_create_contract` cheat, whose internal `deployContract`
        //    step carries no constructor arguments. The standard host
        //    functions (`create_contract`, `create_contract_with_constructor`,
        //    i.e. `env.deployer().deploy_v2(..)`) are not implemented by the
        //    soroban-semantics and wedge the configuration. So: create the
        //    instance with the cheat, then run the `__constructor` export as an
        //    ordinary cross-contract call — the deploy-then-initialize pattern
        //    Komet's own test_fxdao integration contract uses.
        let vault_addr = komet::create_contract(&env, &label(&env, b"vault"), &vault_hash);
        let ctor_args = soroban_sdk::vec![
            &env,
            owner.into_val(&env),
            guardian.into_val(&env),
            token.into_val(&env),
            treasury.into_val(&env),
            String::from_str(&env, "MicroVault USDC Shares").into_val(&env),
            String::from_str(&env, "mvUSDC").into_val(&env),
        ];
        env.invoke_contract::<()>(&vault_addr, &Symbol::new(&env, "__constructor"), ctor_args);

        env.storage().instance().set(&VAULT_KEY, &vault_addr);
        env.storage().instance().set(&TOKEN_KEY, &token);
        env.storage().instance().set(&OWNER_KEY, &owner);
        env.storage().instance().set(&ROLE_KEY, &compliance);
    }

    // ───────────────────────────────────────────────────────────────────
    // AUDIT PROBES — each one states a *desired* invariant for an OPEN
    // finding in audit.md. They are EXPECTED TO FAIL on this revision; a red
    // result here is the finding being confirmed, not a broken harness.
    // ───────────────────────────────────────────────────────────────────

    /// audit.md §1 residual: `max_redeem` does not clamp to the per-transaction
    /// withdrawal cap that `redeem` enforces, so it advertises more than it can
    /// honour. ERC-4626 requires `redeem(max_redeem(owner))` not to revert and
    /// the advertised limit to respect the same cap as `max_withdraw`.
    pub fn test_max_redeem_is_usable(env: Env, seed: u64, raw_cap: i128) -> bool {
        let v = vault_client(&env);
        let t = token_client(&env);
        let user = user_addr(&env, b'a', seed);
        enable_allowlist(&env);
        v.allow_depositor(&role(&env), &user);
        t.mint(&user, &(A * 10));
        let shares = v.deposit(&A, &user, &user, &user);
        if shares <= 0 {
            return false;
        }

        let value = v.preview_redeem(&shares); // asset value of the whole position
        if value < 2 {
            return true; // no meaningful cap to set below the position
        }
        let cap = raw_cap.rem_euclid(value - 1) + 1; // 1 ..= value-1, always < value
        v.set_max_withdraw(&cap);

        let m = v.max_redeem(&user);
        let w = v.max_withdraw(&user);
        if w <= 0 {
            return false; // vacuity guard: the capped view must still offer something
        }

        // The advertised share limit must be redeemable right now, and worth no
        // more than the cap the same transaction will be checked against.
        v.try_redeem(&m, &user, &user, &user).is_ok() && v.preview_redeem(&m) <= cap
    }

    /// audit.md §3: the limit setters accept non-positive values. A cap is an
    /// amount, so the views must never report a negative one; either the setter
    /// rejects it or the view is clamped.
    pub fn test_neg_limits_never_exposed(env: Env, seed: u64) -> bool {
        let v = vault_client(&env);
        let t = token_client(&env);
        let user = user_addr(&env, b'a', seed);
        enable_allowlist(&env);
        v.allow_depositor(&role(&env), &user);
        t.mint(&user, &(A * 10));
        if v.deposit(&A, &user, &user, &user) <= 0 {
            return false;
        }

        let r_deposit = v.try_set_max_deposit(&(-1i128));
        let deposit_ok = r_deposit.is_err() || v.get_max_deposit() >= 0;

        let r_withdraw = v.try_set_max_withdraw(&(-1i128));
        let withdraw_ok = r_withdraw.is_err() || v.max_withdraw(&user) >= 0;

        deposit_ok && withdraw_ok
    }
}
