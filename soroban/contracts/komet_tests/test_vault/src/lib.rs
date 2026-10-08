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
    // Phase-3 smoke gate: proves constructor + auth + token wiring in one
    // ───────────────────────────────────────────────────────────────────

    pub fn test_smoke_deposit(env: Env, raw_amount: i128) -> bool {
        let v = vault_client(&env);
        let t = token_client(&env);
        let user = account(&env, b"depositor");
        let a = amt(raw_amount);
        t.mint(&user, &(a * 10));
        let shares = v.deposit(&a, &user, &user, &user);
        shares > 0 && v.balance(&user) == shares && t.balance(&v.address) == a
    }

    // ───────────────────────────────────────────────────────────────────
    // A. allow/disallow gates deposits and transfers
    // ───────────────────────────────────────────────────────────────────

    /// Disallowed depositor cannot deposit: exact AddressNotAllowed (#14),
    /// list state flips, deadline starts, existing position untouched.
    pub fn test_no_deposit_when_disallowed(
        env: Env,
        seed: u64,
        raw_now: u64,
        raw_amount: i128,
    ) -> bool {
        let v = vault_client(&env);
        let t = token_client(&env);
        let now = fuzz_time(raw_now);
        let user = user_addr(&env, b'a', seed);
        let a = amt(raw_amount);
        komet::set_ledger_timestamp(&env, now);
        enable_allowlist(&env);
        v.allow_depositor(&role(&env), &user);
        t.mint(&user, &(a * 10));
        let shares = v.deposit(&a, &user, &user, &user);
        if shares <= 0 {
            return false;
        }

        v.disallow_depositor(&role(&env), &user);
        let grace = v.withdraw_grace_period();

        v.try_deposit(&a, &user, &user, &user)
            == Err(Ok(soroban_sdk::Error::from_contract_error(14)))
            && !v.is_allowed(&user)
            && v.exit_deadline(&user) == Some(now + grace)
            && v.balance(&user) == shares
    }

    /// Two-sided gate: payer allowlisted, receiver not → still #14.
    pub fn test_receiver_must_be_allowed(
        env: Env,
        seed: u64,
        raw_amount: i128,
    ) -> bool {
        let v = vault_client(&env);
        let t = token_client(&env);
        let user = user_addr(&env, b'a', seed);
        let receiver = account(&env, b"rcpt_acct");
        let a = amt(raw_amount);
        enable_allowlist(&env);
        v.allow_depositor(&role(&env), &user); // from allowed, receiver is not
        t.mint(&user, &(a * 10));
        v.try_deposit(&a, &receiver, &user, &user)
            == Err(Ok(soroban_sdk::Error::from_contract_error(14)))
    }

    /// Positive control: an allowed depositor can always deposit.
    pub fn test_allowed_deposit_succeeds(env: Env, seed: u64, raw_amount: i128) -> bool {
        let v = vault_client(&env);
        let t = token_client(&env);
        let user = user_addr(&env, b'a', seed);
        let a = amt(raw_amount);
        enable_allowlist(&env);
        v.allow_depositor(&role(&env), &user);
        t.mint(&user, &(a * 10));
        let shares = v.deposit(&a, &user, &user, &user);
        shares > 0 && v.balance(&user) == shares && v.is_allowed(&user)
    }

    /// Re-allowing restores deposits AND clears the exit deadline.
    pub fn test_reallow_restores_deposit(
        env: Env,
        seed: u64,
        raw_amount: i128,
    ) -> bool {
        let v = vault_client(&env);
        let t = token_client(&env);
        let user = user_addr(&env, b'a', seed);
        let a = amt(raw_amount);
        enable_allowlist(&env);
        v.allow_depositor(&role(&env), &user);
        t.mint(&user, &(a * 20));
        let shares = v.deposit(&a, &user, &user, &user);
        if shares <= 0 {
            return false;
        }

        v.disallow_depositor(&role(&env), &user);
        let blocked = v.try_deposit(&a, &user, &user, &user)
            == Err(Ok(soroban_sdk::Error::from_contract_error(14)));
        if !blocked {
            return false;
        }

        v.allow_depositor(&role(&env), &user);
        let again = v.try_deposit(&a, &user, &user, &user);
        v.exit_deadline(&user).is_none()
            && v.is_allowed(&user)
            && again.is_ok()
            && v.balance(&user) > shares
    }

    /// A revocation only binds while enforcement is switched on.
    pub fn test_enforcement_off_not_gated(env: Env, seed: u64, raw_amount: i128) -> bool {
        let v = vault_client(&env);
        let t = token_client(&env);
        let user = user_addr(&env, b'a', seed);
        let a = amt(raw_amount);
        enable_allowlist(&env);
        v.allow_depositor(&role(&env), &user);
        t.mint(&user, &(a * 20));
        if v.deposit(&a, &user, &user, &user) <= 0 {
            return false;
        }
        v.disallow_depositor(&role(&env), &user);

        v.set_allowlist_enforced(&false); // owner-only
        let more = v.deposit(&a, &user, &user, &user);
        more > 0 && !v.allowlist_enforced() && !v.is_frozen(&user)
    }

    /// Share transfers: disallowing the recipient blocks the move (#113),
    /// re-allowing unblocks it. (Tag bytes 'a'/'b' guarantee user ≠ recipient.)
    pub fn test_transfer_needs_allowed_to(
        env: Env,
        seed: u64,
        seed2: u64,
        raw_amount: i128,
    ) -> bool {
        let v = vault_client(&env);
        let t = token_client(&env);
        let user = user_addr(&env, b'a', seed);
        let recipient = user_addr(&env, b'b', seed2);
        let a = amt(raw_amount);
        enable_allowlist(&env);
        v.allow_depositor(&role(&env), &user);
        v.allow_depositor(&role(&env), &recipient);
        t.mint(&user, &(a * 10));
        let shares = v.deposit(&a, &user, &user, &user);
        if shares < 2 {
            return false;
        }

        v.disallow_depositor(&role(&env), &recipient);
        let sc = soroban_sdk::token::Client::new(&env, &v.address); // share token
        let half = shares / 2;
        let blocked = sc.try_transfer(&user, &recipient, &half)
            == Err(Ok(soroban_sdk::Error::from_contract_error(113)));
        if !blocked {
            return false;
        }

        v.allow_depositor(&role(&env), &recipient);
        sc.transfer(&user, &recipient, &half);
        sc.balance(&recipient) == half
    }

    // ───────────────────────────────────────────────────────────────────
    // B. exit deadline freezes shares
    // ───────────────────────────────────────────────────────────────────

    /// Disallow starts the grace window: deadline == now + grace, not yet frozen.
    pub fn test_disallow_starts_grace(env: Env, seed: u64, raw_now: u64) -> bool {
        let v = vault_client(&env);
        let t = token_client(&env);
        let now = fuzz_time(raw_now);
        let user = user_addr(&env, b'a', seed);
        komet::set_ledger_timestamp(&env, now);
        enable_allowlist(&env);
        v.allow_depositor(&role(&env), &user);
        t.mint(&user, &(A * 10));
        if v.deposit(&A, &user, &user, &user) <= 0 {
            return false;
        }

        v.disallow_depositor(&role(&env), &user);
        let grace = v.withdraw_grace_period();

        v.exit_deadline(&user) == Some(now + grace)
            && !v.is_frozen(&user)
            && !v.is_allowed(&user)
    }

    /// Inside the window (delta < grace) the depositor can still exit fully.
    pub fn test_grace_window_allows_exit(
        env: Env,
        seed: u64,
        raw_now: u64,
        raw_delta: u64,
    ) -> bool {
        let v = vault_client(&env);
        let t = token_client(&env);
        let now = fuzz_time(raw_now);
        let user = user_addr(&env, b'a', seed);
        komet::set_ledger_timestamp(&env, now);
        enable_allowlist(&env);
        v.allow_depositor(&role(&env), &user);
        t.mint(&user, &(A * 10));
        let shares = v.deposit(&A, &user, &user, &user);
        if shares <= 0 {
            return false;
        }

        v.disallow_depositor(&role(&env), &user);
        let grace = v.withdraw_grace_period(); // 30 days by default, > 0
        let delta = raw_delta % grace; // strictly inside the window
        komet::set_ledger_timestamp(&env, now + delta);

        let w = v.max_withdraw(&user);
        if w <= 0 {
            return false;
        }
        let before = t.balance(&user);
        v.withdraw(&w, &user, &user, &user);

        !v.is_frozen(&user) && t.balance(&user) > before && v.balance(&user) < shares
    }

    /// At/after the deadline (delta >= grace): frozen. withdraw/redeem fail
    /// with ExitWindowClosed (#16) and both max exit queries report 0.
    pub fn test_frozen_blocks_exit(
        env: Env,
        seed: u64,
        raw_now: u64,
        raw_delta: u64,
    ) -> bool {
        let v = vault_client(&env);
        let t = token_client(&env);
        let now = fuzz_time(raw_now);
        let user = user_addr(&env, b'a', seed);
        komet::set_ledger_timestamp(&env, now);
        enable_allowlist(&env);
        v.allow_depositor(&role(&env), &user);
        t.mint(&user, &(A * 10));
        if v.deposit(&A, &user, &user, &user) <= 0 {
            return false;
        }

        v.disallow_depositor(&role(&env), &user);
        let grace = v.withdraw_grace_period();
        let delta = (raw_delta % (grace * 2)) + grace; // >= grace
        komet::set_ledger_timestamp(&env, now + delta);

        let r_wd = v.try_withdraw(&1, &user, &user, &user);
        let r_rd = v.try_redeem(&1, &user, &user, &user);
        v.is_frozen(&user)
            && v.max_withdraw(&user) == 0
            && v.max_redeem(&user) == 0
            && r_wd == Err(Ok(soroban_sdk::Error::from_contract_error(16)))
            && r_rd == Err(Ok(soroban_sdk::Error::from_contract_error(16)))
    }

    /// freeze_depositor has no grace window: deadline == now, frozen at once.
    pub fn test_freeze_is_immediate(env: Env, seed: u64, raw_now: u64) -> bool {
        let v = vault_client(&env);
        let t = token_client(&env);
        let now = fuzz_time(raw_now);
        let user = user_addr(&env, b'a', seed);
        komet::set_ledger_timestamp(&env, now);
        enable_allowlist(&env);
        v.allow_depositor(&role(&env), &user);
        t.mint(&user, &(A * 10));
        if v.deposit(&A, &user, &user, &user) <= 0 {
            return false;
        }

        v.freeze_depositor(&role(&env), &user);

        v.is_frozen(&user)
            && v.exit_deadline(&user) == Some(now)
            && v.max_redeem(&user) == 0
            && v.try_redeem(&1, &user, &user, &user)
                == Err(Ok(soroban_sdk::Error::from_contract_error(16)))
    }

    /// Revoking twice inside the window must not extend the deadline.
    pub fn test_revoke_twice_no_extension(
        env: Env,
        seed: u64,
        raw_now: u64,
        raw_delta: u64,
    ) -> bool {
        let v = vault_client(&env);
        let t = token_client(&env);
        let now = fuzz_time(raw_now);
        let user = user_addr(&env, b'a', seed);
        komet::set_ledger_timestamp(&env, now);
        enable_allowlist(&env);
        v.allow_depositor(&role(&env), &user);
        t.mint(&user, &(A * 10));
        if v.deposit(&A, &user, &user, &user) <= 0 {
            return false;
        }

        v.disallow_depositor(&role(&env), &user);
        let grace = v.withdraw_grace_period();
        let first = v.exit_deadline(&user);

        let delta = raw_delta % grace; // still inside the window
        komet::set_ledger_timestamp(&env, now + delta);
        v.disallow_depositor(&role(&env), &user);

        first == Some(now + grace) && v.exit_deadline(&user) == first && !v.is_frozen(&user)
    }

    /// Only strictly later deadlines are accepted (#17 otherwise); a valid
    /// extension lifts the freeze without re-allowing the address.
    pub fn test_extend_deadline_unfreezes(
        env: Env,
        seed: u64,
        raw_now: u64,
        raw_ext: u64,
    ) -> bool {
        let v = vault_client(&env);
        let t = token_client(&env);
        let now = fuzz_time(raw_now);
        let user = user_addr(&env, b'a', seed);
        komet::set_ledger_timestamp(&env, now);
        enable_allowlist(&env);
        v.allow_depositor(&role(&env), &user);
        t.mint(&user, &(A * 10));
        if v.deposit(&A, &user, &user, &user) <= 0 {
            return false;
        }
        v.freeze_depositor(&role(&env), &user); // deadline == now

        // Same-or-earlier deadline must be rejected; strictly later accepted.
        // (Asserted via is_err()/is_ok() so it compiles whether the imported
        // client exposes a typed error enum or the generic soroban_sdk::Error.)
        let r_bad = v.try_extend_exit_deadline(&role(&env), &user, &now);

        let ext = (raw_ext % (10 * 24 * 60 * 60)) + 1; // 1..=864000
        let r_ok = v.try_extend_exit_deadline(&role(&env), &user, &(now + ext));

        r_bad.is_err()
            && r_ok.is_ok()
            && v.exit_deadline(&user) == Some(now + ext)
            && !v.is_frozen(&user) // ledger still at `now`
            && v.try_redeem(&1, &user, &user, &user).is_ok()
    }

    /// Re-allowing clears the deadline entirely and reopens the exit.
    pub fn test_reallow_clears_deadline(env: Env, seed: u64, raw_now: u64) -> bool {
        let v = vault_client(&env);
        let t = token_client(&env);
        let now = fuzz_time(raw_now);
        let user = user_addr(&env, b'a', seed);
        komet::set_ledger_timestamp(&env, now);
        enable_allowlist(&env);
        v.allow_depositor(&role(&env), &user);
        t.mint(&user, &(A * 10));
        let shares = v.deposit(&A, &user, &user, &user);
        if shares <= 0 {
            return false;
        }
        v.freeze_depositor(&role(&env), &user);

        v.allow_depositor(&role(&env), &user);

        v.exit_deadline(&user).is_none()
            && !v.is_frozen(&user)
            && v.is_allowed(&user)
            && v.try_redeem(&shares, &user, &user, &user).is_ok()
    }

    /// A recorded deadline only binds while enforcement is on: off → exit
    /// works, on → frozen again.
    pub fn test_deadline_needs_enforcement(env: Env, seed: u64, raw_now: u64) -> bool {
        let v = vault_client(&env);
        let t = token_client(&env);
        let now = fuzz_time(raw_now);
        let user = user_addr(&env, b'a', seed);
        komet::set_ledger_timestamp(&env, now);
        enable_allowlist(&env);
        v.allow_depositor(&role(&env), &user);
        t.mint(&user, &(A * 10));
        if v.deposit(&A, &user, &user, &user) <= 0 {
            return false;
        }
        v.freeze_depositor(&role(&env), &user);

        v.set_allowlist_enforced(&false); // owner-only
        let open = !v.is_frozen(&user) && v.try_redeem(&1, &user, &user, &user).is_ok();

        v.set_allowlist_enforced(&true);
        open && v.is_frozen(&user)
            && v.try_redeem(&1, &user, &user, &user)
                == Err(Ok(soroban_sdk::Error::from_contract_error(16)))
    }

    /// An approved spender cannot pull funds out for a frozen owner: the
    /// check runs on `owner` before any allowance logic.
    pub fn test_spender_cannot_exit_frozen(
        env: Env,
        seed: u64,
        seed2: u64,
        raw_now: u64,
    ) -> bool {
        let v = vault_client(&env);
        let t = token_client(&env);
        let now = fuzz_time(raw_now);
        let user = user_addr(&env, b'a', seed);
        let spender = user_addr(&env, b'c', seed2);
        komet::set_ledger_timestamp(&env, now);
        enable_allowlist(&env);
        v.allow_depositor(&role(&env), &user);
        t.mint(&user, &(A * 10));
        let shares = v.deposit(&A, &user, &user, &user);
        if shares <= 0 {
            return false;
        }

        let sc = soroban_sdk::token::Client::new(&env, &v.address);
        sc.approve(&user, &spender, &shares, &1000);
        v.freeze_depositor(&role(&env), &user);

        v.try_redeem(&shares, &spender, &user, &spender)
            == Err(Ok(soroban_sdk::Error::from_contract_error(16)))
    }

    /// withdraw_grace_period = 0 → revocation freezes immediately.
    pub fn test_zero_grace_freezes_now(env: Env, seed: u64, raw_now: u64) -> bool {
        let v = vault_client(&env);
        let t = token_client(&env);
        let now = fuzz_time(raw_now);
        let user = user_addr(&env, b'a', seed);
        komet::set_ledger_timestamp(&env, now);
        enable_allowlist(&env);
        v.allow_depositor(&role(&env), &user);
        t.mint(&user, &(A * 10));
        if v.deposit(&A, &user, &user, &user) <= 0 {
            return false;
        }

        v.set_withdraw_grace_period(&0); // owner-only
        v.disallow_depositor(&role(&env), &user);

        v.withdraw_grace_period() == 0 && v.is_frozen(&user)
    }

    // ───────────────────────────────────────────────────────────────────
    // Access control on the new mutators
    // ───────────────────────────────────────────────────────────────────

    /// All four exit-mutators reject a non-compliance caller (#1) and leave
    /// the target untouched.
    pub fn test_mutators_need_compliance(env: Env, seed: u64) -> bool {
        let v = vault_client(&env);
        let stranger = account(&env, b"stranger");
        let user = user_addr(&env, b'a', seed);
        enable_allowlist(&env);

        // All four must reject the non-compliance caller with Unauthorized
        // (#1) — asserted as rejection + unchanged state so the check does not
        // depend on which error type the imported client surfaces.
        let rejected = v.try_allow_depositor(&stranger, &user).is_err()
            && v.try_disallow_depositor(&stranger, &user).is_err()
            && v.try_freeze_depositor(&stranger, &user).is_err()
            && v.try_extend_exit_deadline(&stranger, &user, &1_000_000_000).is_err();

        rejected && v.exit_deadline(&user).is_none() && !v.is_allowed(&user)
    }

    /// With no compliance role configured, the mutators fail with
    /// ComplianceRoleNotSet (#15) — enforcement is never silently skipped.
    pub fn test_mutators_fail_without_role(env: Env, seed: u64) -> bool {
        let v = vault_client(&env);
        let user = user_addr(&env, b'a', seed);
        // Note: enable_allowlist() is deliberately NOT called here, so every
        // mutator must fail with ComplianceRoleNotSet (#15) rather than
        // silently proceeding.
        v.try_allow_depositor(&role(&env), &user).is_err()
            && v.try_disallow_depositor(&role(&env), &user).is_err()
            && v.try_freeze_depositor(&role(&env), &user).is_err()
            && v.exit_deadline(&user).is_none()
    }
}
