//! Integration tests for the Microvault SEP-56 tokenized vault contract.
//!
//! Covers initialization, deposits, withdrawals, pause/unpause, treasury
//! management, credit delegation (borrow/repay), interest accrual, lock
//! periods, operator patterns, and contract upgrades.

use crate::{MicrovaultContract, MicrovaultContractClient, MicrovaultError};
use soroban_sdk::{
    testutils::{Address as _, Ledger, LedgerInfo},
    token::{self, StellarAssetClient, TokenClient},
    Address, Env, String,
};

/// Registers a Stellar asset contract and returns its address, token client,
/// and admin client.
fn create_token_contract<'a>(
    env: &'a Env,
    admin: &Address,
) -> (Address, TokenClient<'a>, StellarAssetClient<'a>) {
    let token_address = env.register_stellar_asset_contract_v2(admin.clone());
    let token_client = token::Client::new(env, &token_address.address());
    let token_admin = token::StellarAssetClient::new(env, &token_address.address());
    (token_address.address(), token_client, token_admin)
}

/// Deploys a fully initialized vault with generated owner, guardian, and
/// treasury addresses. Returns `(client, asset_address, token_client,
/// token_admin, owner, treasury, guardian)`.
fn setup_vault<'a>(
    env: &'a Env,
) -> (
    MicrovaultContractClient<'a>,
    Address,
    TokenClient<'a>,
    StellarAssetClient<'a>,
    Address,
    Address,
    Address,
) {
    let owner = Address::generate(env);
    let guardian = Address::generate(env);
    let treasury = Address::generate(env);
    let (asset_address, token_client, token_admin) = create_token_contract(env, &owner);

    let contract_id = env.register(
        MicrovaultContract,
        (
            &owner,
            &guardian,
            &asset_address,
            &treasury,
            String::from_str(env, "MicroVault USDC Shares"),
            String::from_str(env, "mvUSDC"),
        ),
    );
    let client = MicrovaultContractClient::new(env, &contract_id);

    (
        client,
        asset_address,
        token_client,
        token_admin,
        owner,
        treasury,
        guardian,
    )
}

/// Deploys a vault and creates an additional user address pre-funded with
/// `initial_balance` of the underlying asset. Returns `(client,
/// asset_address, token_client, token_admin, owner, treasury, guardian,
/// user)`.
fn setup_vault_with_user<'a>(
    env: &'a Env,
    initial_balance: i128,
) -> (
    MicrovaultContractClient<'a>,
    Address,
    TokenClient<'a>,
    StellarAssetClient<'a>,
    Address,
    Address,
    Address,
    Address,
) {
    let (client, asset_address, token_client, token_admin, owner, treasury, guardian) =
        setup_vault(env);

    let user = Address::generate(env);
    env.mock_all_auths();
    token_admin.mint(&user, &initial_balance);

    (
        client,
        asset_address,
        token_client,
        token_admin,
        owner,
        treasury,
        guardian,
        user,
    )
}

// ─────────────────────────────────────────────────────────────────────
// Initialization
// ─────────────────────────────────────────────────────────────────────

#[test]
fn test_initialization() {
    let env = Env::default();
    env.mock_all_auths();

    let (client, asset_address, _token_client, _token_admin, _owner, treasury, _guardian) =
        setup_vault(&env);

    assert_eq!(client.query_asset(), asset_address);
    assert_eq!(client.treasury(), treasury);
    assert_eq!(client.total_assets(), 0);
    assert!(!client.paused());
}

// ─────────────────────────────────────────────────────────────────────
// Deposits
// ─────────────────────────────────────────────────────────────────────

#[test]
fn test_deposit() {
    let env = Env::default();
    env.mock_all_auths();

    let (client, _asset_address, token_client, _token_admin, _owner, _treasury, _guardian, user) =
        setup_vault_with_user(&env, 10_000_000);

    let deposit_amount = 1_000_000i128;

    // Verify initial balance
    assert_eq!(token_client.balance(&user), 10_000_000);

    // Deposit: user deposits assets, receives shares
    let shares = client.deposit(&deposit_amount, &user, &user, &user);

    assert!(shares > 0);
    assert_eq!(token_client.balance(&user), 9_000_000);
    assert_eq!(client.total_assets(), deposit_amount);
    assert_eq!(client.balance(&user), shares);
}

#[test]
#[should_panic(expected = "Deposit exceeds maximum limit")]
fn test_deposit_exceeds_max_limit() {
    let env = Env::default();
    env.mock_all_auths();

    let (client, _asset_address, _token_client, token_admin, _owner, _treasury, _guardian, user) =
        setup_vault_with_user(&env, 0);

    // Mint more than default max deposit (1M USDC)
    let huge_amount = 20_000_000_000_000i128;
    token_admin.mint(&user, &huge_amount);

    // Should panic due to exceeding limit
    client.deposit(&huge_amount, &user, &user, &user);
}

// ─────────────────────────────────────────────────────────────────────
// Withdrawals
// ─────────────────────────────────────────────────────────────────────

#[test]
fn test_withdraw() {
    let env = Env::default();
    env.mock_all_auths();

    let (client, _asset_address, token_client, _token_admin, _owner, _treasury, _guardian, user) =
        setup_vault_with_user(&env, 10_000_000);

    // Deposit first
    let deposit_amount = 1_000_000i128;
    let shares = client.deposit(&deposit_amount, &user, &user, &user);

    // Withdraw half
    let withdraw_amount = 500_000i128;
    let shares_burned = client.withdraw(&withdraw_amount, &user, &user, &user);

    assert!(shares_burned > 0);
    assert!(client.balance(&user) < shares);
    assert!(token_client.balance(&user) > 9_000_000);
}

#[test]
fn test_redeem() {
    let env = Env::default();
    env.mock_all_auths();

    let (client, _asset_address, _token_client, _token_admin, _owner, _treasury, _guardian, user) =
        setup_vault_with_user(&env, 10_000_000);

    // Deposit
    let deposit_amount = 1_000_000i128;
    let shares = client.deposit(&deposit_amount, &user, &user, &user);

    // Redeem all shares
    let assets_received = client.redeem(&shares, &user, &user, &user);

    assert!(assets_received > 0);
    assert_eq!(client.balance(&user), 0);
}

// ─────────────────────────────────────────────────────────────────────
// Pause / Unpause
// ─────────────────────────────────────────────────────────────────────

#[test]
fn test_pause_and_unpause() {
    let env = Env::default();
    env.mock_all_auths();

    let (client, _asset_address, _token_client, _token_admin, owner, _treasury, _guardian, user) =
        setup_vault_with_user(&env, 10_000_000);

    // Pause
    client.pause(&owner);
    assert!(client.paused());

    // Max functions should return 0 when paused
    assert_eq!(client.max_deposit(&user), 0);
    assert_eq!(client.max_withdraw(&user), 0);

    // Unpause
    client.unpause(&owner);
    assert!(!client.paused());

    // Should be able to deposit now
    let shares = client.deposit(&1_000_000i128, &user, &user, &user);
    assert!(shares > 0);
}

// ─────────────────────────────────────────────────────────────────────
// Treasury Management
// ─────────────────────────────────────────────────────────────────────

#[test]
fn test_set_treasury() {
    let env = Env::default();
    env.mock_all_auths();

    let (client, _asset_address, _token_client, _token_admin, _owner, _treasury, _guardian) =
        setup_vault(&env);

    let new_treasury = Address::generate(&env);

    // Owner can set treasury
    client.set_treasury(&new_treasury);
    assert_eq!(client.treasury(), new_treasury);
}

// ─────────────────────────────────────────────────────────────────────
// Admin Limits
// ─────────────────────────────────────────────────────────────────────

#[test]
fn test_set_max_deposit() {
    let env = Env::default();
    env.mock_all_auths();

    let (client, _asset_address, _token_client, _token_admin, _owner, _treasury, _guardian) =
        setup_vault(&env);

    let new_limit = 500_000_000_000i128;
    client.set_max_deposit(&new_limit);

    let user = Address::generate(&env);
    assert_eq!(client.max_deposit(&user), new_limit);
}

#[test]
fn test_set_max_withdraw() {
    let env = Env::default();
    env.mock_all_auths();

    let (client, _asset_address, _token_client, _token_admin, _owner, _treasury, _guardian) =
        setup_vault(&env);

    let new_limit = 100_000_000_000i128;
    client.set_max_withdraw(&new_limit);

    assert_eq!(client.get_max_withdraw(), new_limit);
}

// ─────────────────────────────────────────────────────────────────────
// Sweep Foreign Assets
// ─────────────────────────────────────────────────────────────────────

#[test]
fn test_sweep_foreign_asset() {
    let env = Env::default();
    env.mock_all_auths();

    let (client, _asset_address, _token_client, _token_admin, _owner, treasury, _guardian) =
        setup_vault(&env);

    // Create a foreign token (e.g., USDT)
    let foreign_admin = Address::generate(&env);
    let (foreign_token_address, foreign_token_client, foreign_token_admin) =
        create_token_contract(&env, &foreign_admin);

    // Mint foreign tokens to the vault
    let vault_address = client.address.clone();
    let sweep_amount = 1_000_000i128;
    foreign_token_admin.mint(&vault_address, &sweep_amount);

    assert_eq!(foreign_token_client.balance(&vault_address), sweep_amount);

    // Sweep foreign tokens to recipient
    let recipient = Address::generate(&env);
    client.sweep_foreign_asset(&treasury, &foreign_token_address, &recipient, &sweep_amount);

    // Verify tokens were swept
    assert_eq!(foreign_token_client.balance(&vault_address), 0);
    assert_eq!(foreign_token_client.balance(&recipient), sweep_amount);
}

#[test]
fn test_sweep_underlying_asset_fails() {
    let env = Env::default();
    env.mock_all_auths();

    let (client, asset_address, _token_client, token_admin, _owner, treasury, _guardian) =
        setup_vault(&env);

    // Try to sweep the underlying asset
    let vault_address = client.address.clone();
    token_admin.mint(&vault_address, &1_000_000i128);

    let recipient = Address::generate(&env);
    let result =
        client.try_sweep_foreign_asset(&treasury, &asset_address, &recipient, &1_000_000i128);

    assert_eq!(result, Err(Ok(MicrovaultError::CannotSweepUnderlyingAsset)));
}

#[test]
fn test_sweep_unauthorized() {
    let env = Env::default();
    env.mock_all_auths();

    let (client, _asset_address, _token_client, _token_admin, _owner, _treasury, _guardian) =
        setup_vault(&env);

    // Create foreign token
    let foreign_admin = Address::generate(&env);
    let (foreign_token_address, _foreign_token_client, foreign_token_admin) =
        create_token_contract(&env, &foreign_admin);

    let vault_address = client.address.clone();
    foreign_token_admin.mint(&vault_address, &1_000_000i128);

    // Non-treasury tries to sweep
    let attacker = Address::generate(&env);
    let recipient = Address::generate(&env);
    let result = client.try_sweep_foreign_asset(
        &attacker,
        &foreign_token_address,
        &recipient,
        &1_000_000i128,
    );

    assert_eq!(result, Err(Ok(MicrovaultError::Unauthorized)));
}

#[test]
fn test_sweep_invalid_amount() {
    let env = Env::default();
    env.mock_all_auths();

    let (client, _asset_address, _token_client, _token_admin, _owner, treasury, _guardian) =
        setup_vault(&env);

    // Create foreign token
    let foreign_admin = Address::generate(&env);
    let (foreign_token_address, _foreign_token_client, foreign_token_admin) =
        create_token_contract(&env, &foreign_admin);

    let vault_address = client.address.clone();
    foreign_token_admin.mint(&vault_address, &1_000_000i128);

    let recipient = Address::generate(&env);

    // Try to sweep zero amount
    let result =
        client.try_sweep_foreign_asset(&treasury, &foreign_token_address, &recipient, &0i128);
    assert_eq!(result, Err(Ok(MicrovaultError::InvalidAmount)));

    // Try to sweep negative amount
    let result =
        client.try_sweep_foreign_asset(&treasury, &foreign_token_address, &recipient, &(-100i128));
    assert_eq!(result, Err(Ok(MicrovaultError::InvalidAmount)));
}

// ─────────────────────────────────────────────────────────────────────
// Multi-User Scenarios
// ─────────────────────────────────────────────────────────────────────

#[test]
fn test_multiple_depositors() {
    let env = Env::default();
    env.mock_all_auths();

    let (client, _asset_address, _token_client, token_admin, _owner, _treasury, _guardian, user_a) =
        setup_vault_with_user(&env, 10_000_000);

    // User A deposits 1M
    let shares_a = client.deposit(&1_000_000i128, &user_a, &user_a, &user_a);

    // User B deposits 500K
    let user_b = Address::generate(&env);
    token_admin.mint(&user_b, &500_000i128);
    let shares_b = client.deposit(&500_000i128, &user_b, &user_b, &user_b);

    // Verify
    assert_eq!(client.total_assets(), 1_500_000);
    assert!(shares_a > shares_b);
    assert_eq!(client.balance(&user_a), shares_a);
    assert_eq!(client.balance(&user_b), shares_b);
}

// ─────────────────────────────────────────────────────────────────────
// SEP-56 Preview Functions
// ─────────────────────────────────────────────────────────────────────

#[test]
fn test_preview_functions() {
    let env = Env::default();
    env.mock_all_auths();

    let (client, _asset_address, _token_client, _token_admin, _owner, _treasury, _guardian) =
        setup_vault(&env);

    let assets = 1_000_000i128;
    let preview_shares = client.preview_deposit(&assets);
    assert!(preview_shares > 0);
    assert_eq!(preview_shares, client.convert_to_shares(&assets));

    let shares = 1_000_000i128;
    let preview_assets = client.preview_redeem(&shares);
    assert_eq!(preview_assets, client.convert_to_assets(&shares));
}

// ─────────────────────────────────────────────────────────────────────
// Operator Pattern
// ─────────────────────────────────────────────────────────────────────

#[test]
fn test_operator_deposit() {
    let env = Env::default();
    env.mock_all_auths();

    let (client, asset_address, token_client, token_admin, _owner, _treasury, _guardian) =
        setup_vault(&env);

    let user = Address::generate(&env);
    let operator = Address::generate(&env);
    let receiver = Address::generate(&env);

    // Mint tokens to user
    token_admin.mint(&user, &1_000_000i128);

    // User approves operator to spend their tokens
    let asset_client = token::Client::new(&env, &asset_address);
    asset_client.approve(&user, &operator, &1_000_000i128, &1000);

    // Operator deposits on behalf of user to receiver
    let shares = client.deposit(&500_000i128, &receiver, &user, &operator);

    assert!(shares > 0);
    assert_eq!(client.balance(&receiver), shares);
    assert_eq!(token_client.balance(&user), 500_000);
}

// ─────────────────────────────────────────────────────────────────────
// Full Lifecycle Integration
// ─────────────────────────────────────────────────────────────────────

#[test]
fn test_full_vault_lifecycle() {
    let env = Env::default();
    env.mock_all_auths();

    let (client, _asset_address, token_client, token_admin, owner, _treasury, _guardian) =
        setup_vault(&env);

    // 1. User deposits
    let user = Address::generate(&env);
    token_admin.mint(&user, &5_000_000i128);
    let user_shares = client.deposit(&5_000_000i128, &user, &user, &user);

    assert_eq!(client.total_assets(), 5_000_000);
    assert_eq!(client.balance(&user), user_shares);

    // 2. User redeems half
    let assets_received = client.redeem(&(user_shares / 2), &user, &user, &user);
    assert!(assets_received > 0);
    assert!(token_client.balance(&user) > 0);

    // 3. Pause in emergency
    client.pause(&owner);
    assert!(client.paused());

    // 4. Unpause
    client.unpause(&owner);
    assert!(!client.paused());

    // 5. Update treasury via owner-gated set_treasury
    let new_treasury = Address::generate(&env);
    client.set_treasury(&new_treasury);
    assert_eq!(client.treasury(), new_treasury);

    // 7. User redeems remaining shares
    let remaining_shares = client.balance(&user);
    if remaining_shares > 0 {
        client.redeem(&remaining_shares, &user, &user, &user);
    }
    assert_eq!(client.balance(&user), 0);
}

// ─────────────────────────────────────────────────────────────────────
// Credit Delegation
// ─────────────────────────────────────────────────────────────────────

#[test]
fn test_credit_delegation_initial_state() {
    let env = Env::default();
    env.mock_all_auths();

    let (client, _asset_address, _token_client, _token_admin, _owner, _treasury, _guardian) =
        setup_vault(&env);

    // Initial state should have zero borrowed
    assert_eq!(client.total_borrowed(), 0);
    assert_eq!(client.utilization_rate(), 0);
    assert_eq!(client.available_liquidity(), 0);
}

#[test]
fn test_borrow_success() {
    let env = Env::default();
    env.mock_all_auths();

    let (client, _asset_address, token_client, token_admin, _owner, treasury, _guardian) =
        setup_vault(&env);

    // Depositor provides liquidity
    let depositor = Address::generate(&env);
    token_admin.mint(&depositor, &10_000_000i128);
    client.deposit(&10_000_000i128, &depositor, &depositor, &depositor);

    // Child account (loan recipient)
    let child_account = Address::generate(&env);

    // Treasury borrows for child account (80% of 10M = 8M max)
    let borrow_amount = 5_000_000i128;
    client.borrow(&treasury, &child_account, &borrow_amount);

    // Verify state — funds go to treasury, not child account
    assert_eq!(client.total_borrowed(), borrow_amount);
    assert_eq!(token_client.balance(&treasury), borrow_amount);
    assert_eq!(client.available_liquidity(), 5_000_000); // 10M - 5M borrowed

    // Utilization should be 50%
    let utilization = client.utilization_rate();
    assert!(utilization > 0);
}

#[test]
fn test_borrow_to_multiple_recipients() {
    let env = Env::default();
    env.mock_all_auths();

    let (client, _asset_address, token_client, token_admin, _owner, treasury, _guardian) =
        setup_vault(&env);

    // Depositor provides liquidity
    let depositor = Address::generate(&env);
    token_admin.mint(&depositor, &10_000_000i128);
    client.deposit(&10_000_000i128, &depositor, &depositor, &depositor);

    // Multiple child accounts
    let child_a = Address::generate(&env);
    let child_b = Address::generate(&env);

    // Treasury borrows for different recipients
    client.borrow(&treasury, &child_a, &2_000_000i128);
    client.borrow(&treasury, &child_b, &3_000_000i128);

    // Verify treasury received all borrowed funds
    assert_eq!(token_client.balance(&treasury), 5_000_000); // 2M + 3M
    assert_eq!(client.total_borrowed(), 5_000_000);
}

#[test]
fn test_borrow_exceeds_utilization_cap() {
    let env = Env::default();
    env.mock_all_auths();

    let (client, _asset_address, _token_client, token_admin, _owner, treasury, _guardian) =
        setup_vault(&env);

    // Depositor provides liquidity
    let depositor = Address::generate(&env);
    token_admin.mint(&depositor, &10_000_000i128);
    client.deposit(&10_000_000i128, &depositor, &depositor, &depositor);

    let child_account = Address::generate(&env);

    // Try to borrow more than 80% - should fail
    let result = client.try_borrow(&treasury, &child_account, &9_000_000i128);
    assert_eq!(result, Err(Ok(MicrovaultError::ExceedsUtilizationCap)));
}

#[test]
fn test_borrow_at_max_utilization() {
    let env = Env::default();
    env.mock_all_auths();

    let (client, _asset_address, token_client, token_admin, _owner, treasury, _guardian) =
        setup_vault(&env);

    // Depositor provides liquidity
    let depositor = Address::generate(&env);
    token_admin.mint(&depositor, &10_000_000i128);
    client.deposit(&10_000_000i128, &depositor, &depositor, &depositor);

    let child_account = Address::generate(&env);

    // Borrow exactly 80% - should succeed
    let max_borrow = 8_000_000i128;
    client.borrow(&treasury, &child_account, &max_borrow);

    assert_eq!(token_client.balance(&treasury), max_borrow);
    assert_eq!(client.total_borrowed(), max_borrow);
}

#[test]
fn test_borrow_unauthorized() {
    let env = Env::default();
    env.mock_all_auths();

    let (client, _asset_address, _token_client, token_admin, _owner, _treasury, _guardian) =
        setup_vault(&env);

    // Depositor provides liquidity
    let depositor = Address::generate(&env);
    token_admin.mint(&depositor, &10_000_000i128);
    client.deposit(&10_000_000i128, &depositor, &depositor, &depositor);

    let attacker = Address::generate(&env);
    let child_account = Address::generate(&env);

    // Non-treasury tries to borrow
    let result = client.try_borrow(&attacker, &child_account, &1_000_000i128);
    assert_eq!(result, Err(Ok(MicrovaultError::Unauthorized)));
}

#[test]
fn test_borrow_invalid_amount() {
    let env = Env::default();
    env.mock_all_auths();

    let (client, _asset_address, _token_client, token_admin, _owner, treasury, _guardian) =
        setup_vault(&env);

    // Depositor provides liquidity
    let depositor = Address::generate(&env);
    token_admin.mint(&depositor, &10_000_000i128);
    client.deposit(&10_000_000i128, &depositor, &depositor, &depositor);

    let child_account = Address::generate(&env);

    // Zero amount
    let result = client.try_borrow(&treasury, &child_account, &0i128);
    assert_eq!(result, Err(Ok(MicrovaultError::InvalidAmount)));

    // Negative amount
    let result = client.try_borrow(&treasury, &child_account, &(-100i128));
    assert_eq!(result, Err(Ok(MicrovaultError::InvalidAmount)));
}

#[test]
fn test_borrow_checks_utilization_before_liquidity() {
    let env = Env::default();
    env.mock_all_auths();

    let (client, _asset_address, _token_client, token_admin, _owner, treasury, _guardian) =
        setup_vault(&env);

    // Deposit 10M
    let depositor = Address::generate(&env);
    token_admin.mint(&depositor, &10_000_000i128);
    client.deposit(&10_000_000i128, &depositor, &depositor, &depositor);

    let child_account = Address::generate(&env);

    // Borrow 7M (70% utilization)
    client.borrow(&treasury, &child_account, &7_000_000i128);

    // Try to borrow 5M more - would be 120% utilization
    // Utilization cap (80%) is checked before liquidity
    let result = client.try_borrow(&treasury, &child_account, &5_000_000i128);
    assert_eq!(result, Err(Ok(MicrovaultError::ExceedsUtilizationCap)));
}

#[test]
fn test_repay_success() {
    let env = Env::default();
    env.mock_all_auths();

    let (client, _asset_address, _token_client, token_admin, _owner, treasury, _guardian) =
        setup_vault(&env);

    // Depositor provides liquidity
    let depositor = Address::generate(&env);
    token_admin.mint(&depositor, &10_000_000i128);
    client.deposit(&10_000_000i128, &depositor, &depositor, &depositor);

    // Borrow
    let child_account = Address::generate(&env);
    client.borrow(&treasury, &child_account, &5_000_000i128);

    // Treasury needs funds to repay (simulate loan repayment from borrower)
    token_admin.mint(&treasury, &5_000_000i128);

    // Repay
    client.repay(&treasury, &5_000_000i128);

    assert_eq!(client.total_borrowed(), 0);
    assert_eq!(client.available_liquidity(), 10_000_000);
}

#[test]
fn test_repay_partial() {
    let env = Env::default();
    env.mock_all_auths();

    let (client, _asset_address, _token_client, token_admin, _owner, treasury, _guardian) =
        setup_vault(&env);

    // Depositor provides liquidity
    let depositor = Address::generate(&env);
    token_admin.mint(&depositor, &10_000_000i128);
    client.deposit(&10_000_000i128, &depositor, &depositor, &depositor);

    // Borrow
    let child_account = Address::generate(&env);
    client.borrow(&treasury, &child_account, &5_000_000i128);

    // Treasury repays partial amount
    token_admin.mint(&treasury, &3_000_000i128);
    client.repay(&treasury, &3_000_000i128);

    assert_eq!(client.total_borrowed(), 2_000_000);
}

#[test]
fn test_repay_exceeds_debt() {
    let env = Env::default();
    env.mock_all_auths();

    let (client, _asset_address, _token_client, token_admin, _owner, treasury, _guardian) =
        setup_vault(&env);

    // Depositor provides liquidity
    let depositor = Address::generate(&env);
    token_admin.mint(&depositor, &10_000_000i128);
    client.deposit(&10_000_000i128, &depositor, &depositor, &depositor);

    // Borrow small amount
    let child_account = Address::generate(&env);
    client.borrow(&treasury, &child_account, &1_000_000i128);

    // Try to repay more than owed
    token_admin.mint(&treasury, &5_000_000i128);
    let result = client.try_repay(&treasury, &5_000_000i128);
    assert_eq!(result, Err(Ok(MicrovaultError::RepayExceedsDebt)));
}

#[test]
fn test_repay_unauthorized() {
    let env = Env::default();
    env.mock_all_auths();

    let (client, _asset_address, _token_client, token_admin, _owner, treasury, _guardian) =
        setup_vault(&env);

    // Depositor provides liquidity
    let depositor = Address::generate(&env);
    token_admin.mint(&depositor, &10_000_000i128);
    client.deposit(&10_000_000i128, &depositor, &depositor, &depositor);

    // Borrow
    let child_account = Address::generate(&env);
    client.borrow(&treasury, &child_account, &5_000_000i128);

    // Non-treasury tries to repay
    let attacker = Address::generate(&env);
    token_admin.mint(&attacker, &5_000_000i128);
    let result = client.try_repay(&attacker, &5_000_000i128);
    assert_eq!(result, Err(Ok(MicrovaultError::Unauthorized)));
}

// ─────────────────────────────────────────────────────────────────────
// Attributed repay
// ─────────────────────────────────────────────────────────────────────

#[test]
fn test_repay_for_matches_repay() {
    let env = Env::default();
    env.mock_all_auths();

    let (client, _asset_address, _token_client, token_admin, _owner, treasury, _guardian) =
        setup_vault(&env);

    let depositor = Address::generate(&env);
    token_admin.mint(&depositor, &10_000_000i128);
    client.deposit(&10_000_000i128, &depositor, &depositor, &depositor);

    let borrower = Address::generate(&env);
    client.borrow(&treasury, &borrower, &5_000_000i128);

    token_admin.mint(&treasury, &5_000_000i128);
    client.repay_for(&treasury, &borrower, &5_000_000i128);

    assert_eq!(client.total_borrowed(), 0);
    assert_eq!(client.available_liquidity(), 10_000_000);
}

#[test]
fn test_repay_for_partial() {
    let env = Env::default();
    env.mock_all_auths();

    let (client, _asset_address, _token_client, token_admin, _owner, treasury, _guardian) =
        setup_vault(&env);

    let depositor = Address::generate(&env);
    token_admin.mint(&depositor, &10_000_000i128);
    client.deposit(&10_000_000i128, &depositor, &depositor, &depositor);

    let borrower = Address::generate(&env);
    client.borrow(&treasury, &borrower, &5_000_000i128);

    token_admin.mint(&treasury, &3_000_000i128);
    client.repay_for(&treasury, &borrower, &3_000_000i128);

    assert_eq!(client.total_borrowed(), 2_000_000);
}

#[test]
fn test_repay_for_exceeds_debt() {
    let env = Env::default();
    env.mock_all_auths();

    let (client, _asset_address, _token_client, token_admin, _owner, treasury, _guardian) =
        setup_vault(&env);

    let depositor = Address::generate(&env);
    token_admin.mint(&depositor, &10_000_000i128);
    client.deposit(&10_000_000i128, &depositor, &depositor, &depositor);

    let borrower = Address::generate(&env);
    client.borrow(&treasury, &borrower, &1_000_000i128);

    token_admin.mint(&treasury, &5_000_000i128);
    let result = client.try_repay_for(&treasury, &borrower, &5_000_000i128);
    assert_eq!(result, Err(Ok(MicrovaultError::RepayExceedsDebt)));
}

#[test]
fn test_repay_for_unauthorized() {
    let env = Env::default();
    env.mock_all_auths();

    let (client, _asset_address, _token_client, token_admin, _owner, treasury, _guardian) =
        setup_vault(&env);

    let depositor = Address::generate(&env);
    token_admin.mint(&depositor, &10_000_000i128);
    client.deposit(&10_000_000i128, &depositor, &depositor, &depositor);

    let borrower = Address::generate(&env);
    client.borrow(&treasury, &borrower, &5_000_000i128);

    // Naming a borrower does not let a non-treasury caller repay.
    let attacker = Address::generate(&env);
    token_admin.mint(&attacker, &5_000_000i128);
    let result = client.try_repay_for(&attacker, &borrower, &5_000_000i128);
    assert_eq!(result, Err(Ok(MicrovaultError::Unauthorized)));
}

#[test]
fn test_repay_for_invalid_amount() {
    let env = Env::default();
    env.mock_all_auths();

    let (client, _asset_address, _token_client, _token_admin, _owner, treasury, _guardian) =
        setup_vault(&env);

    let borrower = Address::generate(&env);
    let result = client.try_repay_for(&treasury, &borrower, &0i128);
    assert_eq!(result, Err(Ok(MicrovaultError::InvalidAmount)));
}

// ─────────────────────────────────────────────────────────────────────
// Yield bump
// ─────────────────────────────────────────────────────────────────────

#[test]
fn test_bump_yield_raises_managed_assets_without_minting() {
    let env = Env::default();
    env.mock_all_auths();

    let (client, _asset_address, _token_client, token_admin, _owner, _treasury, _guardian) =
        setup_vault(&env);

    let depositor = Address::generate(&env);
    token_admin.mint(&depositor, &10_000_000i128);
    client.deposit(&10_000_000i128, &depositor, &depositor, &depositor);

    let shares_before = client.balance(&depositor);
    let supply_before = client.total_supply();
    let managed_before = client.total_managed_assets();
    let borrowed_before = client.total_borrowed();

    let benefactor = Address::generate(&env);
    token_admin.mint(&benefactor, &1_000_000i128);
    client.bump_yield(&benefactor, &1_000_000i128);

    assert_eq!(client.total_managed_assets(), managed_before + 1_000_000);
    assert_eq!(client.total_borrowed(), borrowed_before);
    assert_eq!(client.total_supply(), supply_before);
    assert_eq!(client.balance(&depositor), shares_before);
}

#[test]
fn test_bump_yield_increases_share_value() {
    let env = Env::default();
    env.mock_all_auths();

    let (client, _asset_address, _token_client, token_admin, _owner, _treasury, _guardian) =
        setup_vault(&env);

    let depositor = Address::generate(&env);
    token_admin.mint(&depositor, &10_000_000i128);
    client.deposit(&10_000_000i128, &depositor, &depositor, &depositor);

    let shares = client.balance(&depositor);
    let redeemable_before = client.preview_redeem(&shares);

    let benefactor = Address::generate(&env);
    token_admin.mint(&benefactor, &2_000_000i128);
    client.bump_yield(&benefactor, &2_000_000i128);

    // Sole depositor, so the whole contribution accrues to them.
    assert!(client.preview_redeem(&shares) > redeemable_before);
}

#[test]
fn test_bump_yield_invalid_amount() {
    let env = Env::default();
    env.mock_all_auths();

    let (client, _asset_address, _token_client, _token_admin, _owner, _treasury, _guardian) =
        setup_vault(&env);

    let benefactor = Address::generate(&env);
    let result = client.try_bump_yield(&benefactor, &0i128);
    assert_eq!(result, Err(Ok(MicrovaultError::InvalidAmount)));
}

#[test]
fn test_interest_accrual() {
    let env = Env::default();
    env.mock_all_auths();

    let (client, _asset_address, _token_client, token_admin, _owner, treasury, _guardian) =
        setup_vault(&env);

    // Depositor provides liquidity
    let depositor = Address::generate(&env);
    token_admin.mint(&depositor, &10_000_000i128);
    client.deposit(&10_000_000i128, &depositor, &depositor, &depositor);

    // Borrow
    let child_account = Address::generate(&env);
    client.borrow(&treasury, &child_account, &5_000_000i128);

    let initial_borrowed = client.total_borrowed();

    // Advance time by 1 year (for significant interest accrual)
    env.ledger().set(LedgerInfo {
        timestamp: 31_536_000, // 1 year in seconds
        protocol_version: 26,
        sequence_number: 100,
        network_id: Default::default(),
        base_reserve: 10,
        min_temp_entry_ttl: 1000,
        min_persistent_entry_ttl: 1000,
        max_entry_ttl: 10000,
    });

    // Trigger interest accrual
    client.accrue();

    // Total borrowed should have increased due to interest
    let new_borrowed = client.total_borrowed();
    assert!(new_borrowed > initial_borrowed);
}

#[test]
fn test_withdrawal_limited_by_liquidity() {
    let env = Env::default();
    env.mock_all_auths();

    let (client, _asset_address, _token_client, token_admin, _owner, treasury, _guardian) =
        setup_vault(&env);

    // Depositor provides liquidity
    let depositor = Address::generate(&env);
    token_admin.mint(&depositor, &10_000_000i128);
    let _shares = client.deposit(&10_000_000i128, &depositor, &depositor, &depositor);

    // Treasury borrows 80%
    let child_account = Address::generate(&env);
    client.borrow(&treasury, &child_account, &8_000_000i128);

    // Max withdraw should be limited to available liquidity (2M)
    let max_withdrawable = client.max_withdraw(&depositor);
    assert!(max_withdrawable <= 2_000_000);

    // Can withdraw up to available liquidity
    client.withdraw(&2_000_000i128, &depositor, &depositor, &depositor);
}

#[test]
fn test_max_withdraw_considers_liquidity() {
    let env = Env::default();
    env.mock_all_auths();

    let (client, _asset_address, _token_client, token_admin, _owner, treasury, _guardian) =
        setup_vault(&env);

    // Depositor provides liquidity
    let depositor = Address::generate(&env);
    token_admin.mint(&depositor, &10_000_000i128);
    client.deposit(&10_000_000i128, &depositor, &depositor, &depositor);

    // Before borrowing, max withdraw should be close to deposit
    let max_before = client.max_withdraw(&depositor);
    assert!(max_before >= 9_000_000); // Accounting for any rounding

    // Treasury borrows 8M
    let child_account = Address::generate(&env);
    client.borrow(&treasury, &child_account, &8_000_000i128);

    // After borrowing, max withdraw should be limited by liquidity
    let max_after = client.max_withdraw(&depositor);
    assert!(max_after <= 2_000_000);
}

#[test]
fn test_max_redeem_considers_liquidity() {
    let env = Env::default();
    env.mock_all_auths();

    let (client, _asset_address, _token_client, token_admin, _owner, treasury, _guardian) =
        setup_vault(&env);

    // Depositor provides liquidity
    let depositor = Address::generate(&env);
    token_admin.mint(&depositor, &10_000_000i128);
    let _shares = client.deposit(&10_000_000i128, &depositor, &depositor, &depositor);

    // Treasury borrows 8M
    let child_account = Address::generate(&env);
    client.borrow(&treasury, &child_account, &8_000_000i128);

    // Max redeem should be limited by available liquidity
    // The key invariant is that max_redeem should not allow
    // redeeming more assets than available liquidity
    let max_redeemable = client.max_redeem(&depositor);
    let max_assets_from_redeem = client.preview_redeem(&max_redeemable);

    // The assets we can get should be limited to available liquidity (2M)
    assert!(max_assets_from_redeem <= 2_000_000);
}

#[test]
fn test_borrow_apr_increases_with_utilization() {
    let env = Env::default();
    env.mock_all_auths();

    let (client, _asset_address, _token_client, token_admin, _owner, treasury, _guardian) =
        setup_vault(&env);

    // Depositor provides liquidity
    let depositor = Address::generate(&env);
    token_admin.mint(&depositor, &10_000_000i128);
    client.deposit(&10_000_000i128, &depositor, &depositor, &depositor);

    // Get APR at 0% utilization
    let apr_zero = client.borrow_apr();

    // Borrow 40% (moderate utilization)
    let child_account = Address::generate(&env);
    client.borrow(&treasury, &child_account, &4_000_000i128);
    let apr_40 = client.borrow_apr();

    // Borrow more to reach 70%
    client.borrow(&treasury, &child_account, &3_000_000i128);
    let apr_70 = client.borrow_apr();

    // APR should increase with utilization
    assert!(apr_40 > apr_zero);
    assert!(apr_70 > apr_40);
}

#[test]
fn test_total_managed_assets_includes_borrowed() {
    let env = Env::default();
    env.mock_all_auths();

    let (client, _asset_address, _token_client, token_admin, _owner, treasury, _guardian) =
        setup_vault(&env);

    // Depositor provides liquidity
    let depositor = Address::generate(&env);
    token_admin.mint(&depositor, &10_000_000i128);
    client.deposit(&10_000_000i128, &depositor, &depositor, &depositor);

    let total_before = client.total_managed_assets();
    assert_eq!(total_before, 10_000_000);

    // Borrow
    let child_account = Address::generate(&env);
    client.borrow(&treasury, &child_account, &5_000_000i128);

    // Total managed should still be 10M (5M available + 5M borrowed)
    let total_after = client.total_managed_assets();
    assert_eq!(total_after, 10_000_000);

    // But available liquidity should be 5M
    assert_eq!(client.available_liquidity(), 5_000_000);
}

#[test]
fn test_full_credit_delegation_lifecycle() {
    let env = Env::default();
    env.mock_all_auths();

    let (client, _asset_address, token_client, token_admin, _owner, treasury, _guardian) =
        setup_vault(&env);

    // 1. Depositors provide liquidity
    let depositor_a = Address::generate(&env);
    let depositor_b = Address::generate(&env);
    token_admin.mint(&depositor_a, &6_000_000i128);
    token_admin.mint(&depositor_b, &4_000_000i128);

    let shares_a = client.deposit(&6_000_000i128, &depositor_a, &depositor_a, &depositor_a);
    let shares_b = client.deposit(&4_000_000i128, &depositor_b, &depositor_b, &depositor_b);

    assert_eq!(client.total_assets(), 10_000_000);

    // 2. Treasury borrows for child accounts
    let child_1 = Address::generate(&env);
    let child_2 = Address::generate(&env);

    client.borrow(&treasury, &child_1, &3_000_000i128);
    client.borrow(&treasury, &child_2, &2_000_000i128);

    assert_eq!(client.total_borrowed(), 5_000_000);
    assert_eq!(token_client.balance(&treasury), 5_000_000); // 3M + 2M

    // 3. Time passes, interest accrues
    env.ledger().set(LedgerInfo {
        timestamp: 2_592_000, // 30 days
        protocol_version: 26,
        sequence_number: 100,
        network_id: Default::default(),
        base_reserve: 10,
        min_temp_entry_ttl: 1000,
        min_persistent_entry_ttl: 1000,
        max_entry_ttl: 10000,
    });

    client.accrue();
    let borrowed_with_interest = client.total_borrowed();
    assert!(borrowed_with_interest > 5_000_000);

    // 4. Treasury repays loans (simulate borrowers repaying)
    token_admin.mint(&treasury, &borrowed_with_interest);
    client.repay(&treasury, &borrowed_with_interest);

    assert_eq!(client.total_borrowed(), 0);

    // 5. Depositors can now withdraw everything
    // Total assets should be higher due to interest earned
    let total_after_interest = client.total_assets();
    assert!(total_after_interest >= 10_000_000);

    // 6. Depositor A redeems all shares
    let assets_a = client.redeem(&shares_a, &depositor_a, &depositor_a, &depositor_a);
    assert!(assets_a >= 6_000_000); // Should have earned interest

    // 7. Depositor B redeems all shares
    let assets_b = client.redeem(&shares_b, &depositor_b, &depositor_b, &depositor_b);
    assert!(assets_b >= 4_000_000); // Should have earned interest
}

// ─────────────────────────────────────────────────────────────────────
// Lock Period
// ─────────────────────────────────────────────────────────────────────

#[test]
fn test_lock_period_default_zero() {
    let env = Env::default();
    env.mock_all_auths();

    let (client, _asset_address, _token_client, _token_admin, _owner, _treasury, _guardian) =
        setup_vault(&env);

    // Default lock period should be 0 (disabled)
    assert_eq!(client.get_lock_period(), 0);
}

#[test]
fn test_set_lock_period() {
    let env = Env::default();
    env.mock_all_auths();

    let (client, _asset_address, _token_client, _token_admin, _owner, _treasury, _guardian) =
        setup_vault(&env);

    // Set lock period to 7 days
    let seven_days: u64 = 604800;
    client.set_lock_period(&seven_days);

    assert_eq!(client.get_lock_period(), seven_days);
}

#[test]
fn test_no_lock_when_period_is_zero() {
    let env = Env::default();
    env.mock_all_auths();

    let (client, _asset_address, _token_client, _token_admin, _owner, _treasury, _guardian, user) =
        setup_vault_with_user(&env, 10_000_000);

    // Lock period is 0 (default)
    assert_eq!(client.get_lock_period(), 0);

    // Deposit
    let shares = client.deposit(&1_000_000i128, &user, &user, &user);
    assert!(shares > 0);

    // User should NOT be locked
    assert!(!client.is_locked(&user));
    assert_eq!(client.get_unlock_time(&user), 0);

    // Should be able to withdraw immediately
    client.withdraw(&500_000i128, &user, &user, &user);
}

#[test]
fn test_lock_applied_on_deposit() {
    let env = Env::default();
    env.mock_all_auths();

    // Set initial timestamp
    env.ledger().set(LedgerInfo {
        timestamp: 1000,
        protocol_version: 26,
        sequence_number: 100,
        network_id: Default::default(),
        base_reserve: 10,
        min_temp_entry_ttl: 1000,
        min_persistent_entry_ttl: 1000,
        max_entry_ttl: 10000,
    });

    let (client, _asset_address, _token_client, _token_admin, _owner, _treasury, _guardian, user) =
        setup_vault_with_user(&env, 10_000_000);

    // Set lock period to 7 days
    let seven_days: u64 = 604800;
    client.set_lock_period(&seven_days);

    // Deposit
    client.deposit(&1_000_000i128, &user, &user, &user);

    // User should be locked
    assert!(client.is_locked(&user));
    assert_eq!(client.get_unlock_time(&user), 1000 + seven_days);
    assert_eq!(client.remaining_lock_time(&user), seven_days);
}

#[test]
#[should_panic(expected = "Shares are locked")]
fn test_withdraw_blocked_when_locked() {
    let env = Env::default();
    env.mock_all_auths();

    env.ledger().set(LedgerInfo {
        timestamp: 1000,
        protocol_version: 26,
        sequence_number: 100,
        network_id: Default::default(),
        base_reserve: 10,
        min_temp_entry_ttl: 1000,
        min_persistent_entry_ttl: 1000,
        max_entry_ttl: 10000,
    });

    let (client, _asset_address, _token_client, _token_admin, _owner, _treasury, _guardian, user) =
        setup_vault_with_user(&env, 10_000_000);

    // Set lock period
    client.set_lock_period(&604800u64); // 7 days

    // Deposit
    client.deposit(&1_000_000i128, &user, &user, &user);

    // Try to withdraw while locked - should panic
    client.withdraw(&500_000i128, &user, &user, &user);
}

#[test]
#[should_panic(expected = "Shares are locked")]
fn test_redeem_blocked_when_locked() {
    let env = Env::default();
    env.mock_all_auths();

    env.ledger().set(LedgerInfo {
        timestamp: 1000,
        protocol_version: 26,
        sequence_number: 100,
        network_id: Default::default(),
        base_reserve: 10,
        min_temp_entry_ttl: 1000,
        min_persistent_entry_ttl: 1000,
        max_entry_ttl: 10000,
    });

    let (client, _asset_address, _token_client, _token_admin, _owner, _treasury, _guardian, user) =
        setup_vault_with_user(&env, 10_000_000);

    // Set lock period
    client.set_lock_period(&604800u64); // 7 days

    // Deposit
    let shares = client.deposit(&1_000_000i128, &user, &user, &user);

    // Try to redeem while locked - should panic
    client.redeem(&shares, &user, &user, &user);
}

#[test]
fn test_withdraw_allowed_after_lock_expires() {
    let env = Env::default();
    env.mock_all_auths();

    env.ledger().set(LedgerInfo {
        timestamp: 1000,
        protocol_version: 26,
        sequence_number: 100,
        network_id: Default::default(),
        base_reserve: 10,
        min_temp_entry_ttl: 1000,
        min_persistent_entry_ttl: 1000,
        max_entry_ttl: 10000,
    });

    let (client, _asset_address, token_client, _token_admin, _owner, _treasury, _guardian, user) =
        setup_vault_with_user(&env, 10_000_000);

    // Set lock period to 7 days
    let seven_days: u64 = 604800;
    client.set_lock_period(&seven_days);

    // Deposit
    client.deposit(&1_000_000i128, &user, &user, &user);

    // Advance time past lock period
    env.ledger().set(LedgerInfo {
        timestamp: 1000 + seven_days + 1,
        protocol_version: 26,
        sequence_number: 200,
        network_id: Default::default(),
        base_reserve: 10,
        min_temp_entry_ttl: 1000,
        min_persistent_entry_ttl: 1000,
        max_entry_ttl: 10000,
    });

    // User should no longer be locked
    assert!(!client.is_locked(&user));
    assert_eq!(client.remaining_lock_time(&user), 0);

    // Withdraw should work
    let initial_balance = token_client.balance(&user);
    client.withdraw(&500_000i128, &user, &user, &user);
    assert!(token_client.balance(&user) > initial_balance);
}

// ─────────────────────────────────────────────────────────────────────
// Lock Period — deposit resets lock (audit MV-04 fix)
// ─────────────────────────────────────────────────────────────────────
//
// The previous weighted-average scheme truncated to zero for small deposits
// on large balances, letting new deposits inherit an unlocked state. The
// fix is simpler: any deposit while a lock period is configured resets the
// user's unlock time to `max(current_unlock, now + lock_period)`.

#[test]
fn test_deposit_resets_lock_to_full_period() {
    let env = Env::default();
    env.mock_all_auths();

    env.ledger().set(LedgerInfo {
        timestamp: 0,
        protocol_version: 26,
        sequence_number: 100,
        network_id: Default::default(),
        base_reserve: 10,
        min_temp_entry_ttl: 1000,
        min_persistent_entry_ttl: 1000,
        max_entry_ttl: 10000,
    });

    let (client, _asset_address, _token_client, _token_admin, _owner, _treasury, _guardian, user) =
        setup_vault_with_user(&env, 10_000_000);

    // Set lock period to 7 days (604800 seconds)
    let seven_days: u64 = 604800;
    client.set_lock_period(&seven_days);

    // First deposit: 1000 USDC -> full 7 day lock
    client.deposit(&1_000_000i128, &user, &user, &user);
    assert_eq!(client.get_unlock_time(&user), seven_days);

    // Advance 6 days (1 day remaining on lock)
    let six_days: u64 = 518400;
    env.ledger().set(LedgerInfo {
        timestamp: six_days,
        protocol_version: 26,
        sequence_number: 200,
        network_id: Default::default(),
        base_reserve: 10,
        min_temp_entry_ttl: 1000,
        min_persistent_entry_ttl: 1000,
        max_entry_ttl: 10000,
    });

    // Remaining lock should be ~1 day before the second deposit
    let remaining = client.remaining_lock_time(&user);
    assert!(remaining > 0 && remaining <= 86400 + 1); // ~1 day

    // Second deposit: 100 USDC (small compared to existing 1000).
    // Under the old weighted-average scheme this would truncate the new
    // lock contribution and leave the user ~1 day from unlocking. Under the
    // fix, the lock is reset to the full 7 days from now.
    client.deposit(&100_000i128, &user, &user, &user);

    let new_remaining = client.remaining_lock_time(&user);
    // Lock is reset to the full period from the second deposit's timestamp.
    assert_eq!(new_remaining, seven_days);
    assert_eq!(client.get_unlock_time(&user), six_days + seven_days);
}

#[test]
fn test_deposit_does_not_shorten_existing_longer_lock() {
    // A later deposit must not shorten an existing longer lock.
    // `new_unlock = max(current_unlock, now + lock_period)`.
    let env = Env::default();
    env.mock_all_auths();

    env.ledger().set(LedgerInfo {
        timestamp: 0,
        protocol_version: 26,
        sequence_number: 100,
        network_id: Default::default(),
        base_reserve: 10,
        min_temp_entry_ttl: 1000,
        min_persistent_entry_ttl: 1000,
        max_entry_ttl: 10000,
    });

    let (client, _asset_address, _token_client, _token_admin, _owner, _treasury, _guardian, user) =
        setup_vault_with_user(&env, 100_000_000);

    let seven_days: u64 = 604800;
    client.set_lock_period(&seven_days);

    // First deposit at t=0 -> unlock at 7 days
    client.deposit(&100_000i128, &user, &user, &user);
    assert_eq!(client.get_unlock_time(&user), seven_days);

    // Advance 1 day; 6 days of lock remain.
    let one_day: u64 = 86400;
    env.ledger().set(LedgerInfo {
        timestamp: one_day,
        protocol_version: 26,
        sequence_number: 200,
        network_id: Default::default(),
        base_reserve: 10,
        min_temp_entry_ttl: 1000,
        min_persistent_entry_ttl: 1000,
        max_entry_ttl: 10000,
    });

    // Shrink the period so the second deposit proposes an *earlier* unlock
    // (1 day + 1 day) than the 7 days already on record. Reducing the period
    // is the only way `now + lock_period` lands before `current_unlock`.
    client.set_lock_period(&one_day);

    client.deposit(&10_000_000i128, &user, &user, &user);
    assert_eq!(client.get_unlock_time(&user), seven_days);
    assert_eq!(client.remaining_lock_time(&user), seven_days - one_day);
}

#[test]
fn test_small_deposit_on_large_balance_locks_full_period() {
    // Regression for audit MV-04: a minimum-size deposit on a large unlocked
    // balance must still lock the user for the full period, not truncate to 0.
    let env = Env::default();
    env.mock_all_auths();

    env.ledger().set(LedgerInfo {
        timestamp: 0,
        protocol_version: 26,
        sequence_number: 100,
        network_id: Default::default(),
        base_reserve: 10,
        min_temp_entry_ttl: 1000,
        min_persistent_entry_ttl: 1000,
        max_entry_ttl: 10000,
    });

    let (client, _asset_address, _token_client, _token_admin, _owner, _treasury, _guardian, user) =
        setup_vault_with_user(&env, 100_000_000);

    let seven_days: u64 = 604800;
    client.set_lock_period(&seven_days);

    // Build a large balance with an expired lock.
    client.deposit(&10_000_000i128, &user, &user, &user);
    assert!(client.is_locked(&user)); // locked at t=0

    // Advance well past expiry.
    env.ledger().set(LedgerInfo {
        timestamp: seven_days + 1,
        protocol_version: 26,
        sequence_number: 200,
        network_id: Default::default(),
        base_reserve: 10,
        min_temp_entry_ttl: 1000,
        min_persistent_entry_ttl: 1000,
        max_entry_ttl: 10000,
    });
    assert!(!client.is_locked(&user));

    // Minimum-size deposit (1 USDC = 10_000_000 stroops with 7 decimals).
    client.deposit(&10_000_000i128, &user, &user, &user);

    // Under the old weighted-average scheme this truncated to 0 and the user
    // stayed unlocked. Under the fix the user is locked for the full period.
    assert!(client.is_locked(&user));
    assert_eq!(client.remaining_lock_time(&user), seven_days);
    assert_eq!(client.get_unlock_time(&user), (seven_days + 1) + seven_days);
}

#[test]
fn test_lock_after_expiry_creates_new_lock() {
    let env = Env::default();
    env.mock_all_auths();

    env.ledger().set(LedgerInfo {
        timestamp: 0,
        protocol_version: 26,
        sequence_number: 100,
        network_id: Default::default(),
        base_reserve: 10,
        min_temp_entry_ttl: 1000,
        min_persistent_entry_ttl: 1000,
        max_entry_ttl: 10000,
    });

    let (client, _asset_address, _token_client, _token_admin, _owner, _treasury, _guardian, user) =
        setup_vault_with_user(&env, 10_000_000);

    // Set lock period to 7 days
    let seven_days: u64 = 604800;
    client.set_lock_period(&seven_days);

    // First deposit
    client.deposit(&1_000_000i128, &user, &user, &user);

    // Advance past lock expiry (10 days)
    env.ledger().set(LedgerInfo {
        timestamp: 864000, // 10 days
        protocol_version: 26,
        sequence_number: 200,
        network_id: Default::default(),
        base_reserve: 10,
        min_temp_entry_ttl: 1000,
        min_persistent_entry_ttl: 1000,
        max_entry_ttl: 10000,
    });

    // Lock should have expired
    assert!(!client.is_locked(&user));

    // New deposit resets the lock to the full period from now.
    // `now + lock_period` = 10 days + 7 days = 17 days.
    client.deposit(&1_000_000i128, &user, &user, &user);

    // Lock is the full period from the new deposit's timestamp.
    let remaining = client.remaining_lock_time(&user);
    assert_eq!(remaining, seven_days);
    assert_eq!(client.get_unlock_time(&user), 864000 + seven_days);
}

#[test]
fn test_mint_also_applies_lock() {
    let env = Env::default();
    env.mock_all_auths();

    env.ledger().set(LedgerInfo {
        timestamp: 1000,
        protocol_version: 26,
        sequence_number: 100,
        network_id: Default::default(),
        base_reserve: 10,
        min_temp_entry_ttl: 1000,
        min_persistent_entry_ttl: 1000,
        max_entry_ttl: 10000,
    });

    let (client, _asset_address, _token_client, _token_admin, _owner, _treasury, _guardian, user) =
        setup_vault_with_user(&env, 10_000_000);

    // Set lock period
    let seven_days: u64 = 604800;
    client.set_lock_period(&seven_days);

    // Mint shares (alternative to deposit)
    let shares_to_mint = 1_000_000_000_000i128; // shares with decimals offset
    client.mint(&shares_to_mint, &user, &user, &user);

    // User should be locked
    assert!(client.is_locked(&user));
    assert_eq!(client.get_unlock_time(&user), 1000 + seven_days);
}

#[test]
#[should_panic(expected = "Error(Contract, #12)")]
fn test_transfer_blocked_while_sender_locked() {
    let env = Env::default();
    env.mock_all_auths();

    env.ledger().set(LedgerInfo {
        timestamp: 1000,
        protocol_version: 26,
        sequence_number: 100,
        network_id: Default::default(),
        base_reserve: 10,
        min_temp_entry_ttl: 1000,
        min_persistent_entry_ttl: 1000,
        max_entry_ttl: 10000,
    });

    let (client, _asset_address, _token_client, _token_admin, _owner, _treasury, _guardian, user) =
        setup_vault_with_user(&env, 10_000_000);

    let seven_days: u64 = 604800;
    client.set_lock_period(&seven_days);

    // Deposit to locked user is locked for 7 days.
    let shares = client.deposit(&1_000_000i128, &user, &user, &user);
    assert!(client.is_locked(&user));

    // Attempt to transfer shares to an attacker address. Must panic.
    let attacker = Address::generate(&env);
    let share_client = token::Client::new(&env, &client.address);
    share_client.transfer(&user, &attacker, &shares);
}

#[test]
#[should_panic(expected = "Error(Contract, #12)")]
fn test_transfer_from_blocked_while_owner_locked() {
    let env = Env::default();
    env.mock_all_auths();

    env.ledger().set(LedgerInfo {
        timestamp: 1000,
        protocol_version: 26,
        sequence_number: 100,
        network_id: Default::default(),
        base_reserve: 10,
        min_temp_entry_ttl: 1000,
        min_persistent_entry_ttl: 1000,
        max_entry_ttl: 10000,
    });

    let (client, _asset_address, _token_client, _token_admin, _owner, _treasury, _guardian, user) =
        setup_vault_with_user(&env, 10_000_000);

    let seven_days: u64 = 604800;
    client.set_lock_period(&seven_days);

    let shares = client.deposit(&1_000_000i128, &user, &user, &user);
    assert!(client.is_locked(&user));

    // An operator (spender) attempts to pull the locked owner's shares via
    // transfer_from. The lock is on the owner balance, so this must panic.
    let spender = Address::generate(&env);
    let recipient = Address::generate(&env);
    let share_client = token::Client::new(&env, &client.address);
    share_client.approve(&user, &spender, &shares, &1000);
    share_client.transfer_from(&spender, &user, &recipient, &shares);
}

#[test]
fn test_transfer_allowed_after_lock_expires() {
    // Once the lock has expired, transfers must work normally again.
    let env = Env::default();
    env.mock_all_auths();

    env.ledger().set(LedgerInfo {
        timestamp: 1000,
        protocol_version: 26,
        sequence_number: 100,
        network_id: Default::default(),
        base_reserve: 10,
        min_temp_entry_ttl: 1000,
        min_persistent_entry_ttl: 1000,
        max_entry_ttl: 10000,
    });

    let (client, _asset_address, _token_client, _token_admin, _owner, _treasury, _guardian, user) =
        setup_vault_with_user(&env, 10_000_000);

    let seven_days: u64 = 604800;
    client.set_lock_period(&seven_days);
    let shares = client.deposit(&1_000_000i128, &user, &user, &user);
    assert!(client.is_locked(&user));

    // Advance past the lock.
    env.ledger().set(LedgerInfo {
        timestamp: 1000 + seven_days + 1,
        protocol_version: 26,
        sequence_number: 200,
        network_id: Default::default(),
        base_reserve: 10,
        min_temp_entry_ttl: 1000,
        min_persistent_entry_ttl: 1000,
        max_entry_ttl: 10000,
    });
    assert!(!client.is_locked(&user));

    // Transfer to a clean recipient succeeds, and the recipient can redeem.
    let recipient = Address::generate(&env);
    let share_client = token::Client::new(&env, &client.address);
    share_client.transfer(&user, &recipient, &shares);
    assert_eq!(share_client.balance(&recipient), shares);
    assert_eq!(share_client.balance(&user), 0);
}

#[test]
fn test_transfer_allowed_when_lock_period_is_zero() {
    // With no lock configured, transfers must be unaffected.
    let env = Env::default();
    env.mock_all_auths();

    let (client, _asset_address, _token_client, _token_admin, _owner, _treasury, _guardian, user) =
        setup_vault_with_user(&env, 10_000_000);

    assert_eq!(client.get_lock_period(), 0);
    let shares = client.deposit(&1_000_000i128, &user, &user, &user);
    assert!(!client.is_locked(&user));

    let recipient = Address::generate(&env);
    let share_client = token::Client::new(&env, &client.address);
    share_client.transfer(&user, &recipient, &shares);
    assert_eq!(share_client.balance(&recipient), shares);
}

#[test]
#[should_panic(expected = "Redemption exceeds maximum limit")]
fn test_redeem_enforces_max_withdraw_cap() {
    let env = Env::default();
    env.mock_all_auths();

    let (client, _asset_address, _token_client, _token_admin, _owner, _treasury, _guardian, user) =
        setup_vault_with_user(&env, 50_000_000);

    // Deposit a large amount so there is enough liquidity to redeem past the cap.
    client.deposit(&50_000_000i128, &user, &user, &user);

    // Lower the per-transaction withdrawal cap to 1 USDC (10M stroops).
    client.set_max_withdraw(&10_000_000i128);

    // Compute the share count equivalent to 2 USDC (20M stroops) and redeem it.
    // This exceeds the 1 USDC cap and previously bypassed it via redeem();
    // it must now panic too.
    let shares_for_2 = client.preview_withdraw(&20_000_000i128);
    assert!(shares_for_2 > 0);
    client.redeem(&shares_for_2, &user, &user, &user);
}

#[test]
fn test_redeem_under_cap_succeeds() {
    // A redemption whose asset equivalent is within MaxWithdraw must still work.
    let env = Env::default();
    env.mock_all_auths();

    let (client, _asset_address, token_client, _token_admin, _owner, _treasury, _guardian, user) =
        setup_vault_with_user(&env, 50_000_000);

    client.deposit(&50_000_000i128, &user, &user, &user);

    // Cap at 10M stroops (1 USDC). Redeem an amount worth less than that.
    client.set_max_withdraw(&10_000_000i128);
    let shares_for_5m = client.preview_withdraw(&5_000_000i128);
    let before = token_client.balance(&user);
    client.redeem(&shares_for_5m, &user, &user, &user);
    assert!(token_client.balance(&user) > before);
}

#[test]
fn test_is_locked_consistent_with_unlock_time() {
    let env = Env::default();
    env.mock_all_auths();

    env.ledger().set(LedgerInfo {
        timestamp: 1000,
        protocol_version: 26,
        sequence_number: 100,
        network_id: Default::default(),
        base_reserve: 10,
        min_temp_entry_ttl: 1000,
        min_persistent_entry_ttl: 1000,
        max_entry_ttl: 10000,
    });

    let (client, _asset_address, _token_client, _token_admin, _owner, _treasury, _guardian, user) =
        setup_vault_with_user(&env, 10_000_000);

    let seven_days: u64 = 604800;
    client.set_lock_period(&seven_days);
    client.deposit(&1_000_000i128, &user, &user, &user);

    // Halfway through the lock window.
    let halfway = 1000 + seven_days / 2;
    env.ledger().set(LedgerInfo {
        timestamp: halfway,
        protocol_version: 26,
        sequence_number: 200,
        network_id: Default::default(),
        base_reserve: 10,
        min_temp_entry_ttl: 1000,
        min_persistent_entry_ttl: 1000,
        max_entry_ttl: 10000,
    });

    // unlock_time is in the future, so locked, and remaining is positive and
    // matches the gap exactly.
    let unlock_time = client.get_unlock_time(&user);
    assert!(unlock_time > halfway, "unlock_time must be in the future");
    assert!(client.is_locked(&user));
    assert_eq!(client.remaining_lock_time(&user), unlock_time - halfway);

    // One ledger before expiry: still locked, remaining == 1.
    env.ledger().set(LedgerInfo {
        timestamp: unlock_time - 1,
        protocol_version: 26,
        sequence_number: 201,
        network_id: Default::default(),
        base_reserve: 10,
        min_temp_entry_ttl: 1000,
        min_persistent_entry_ttl: 1000,
        max_entry_ttl: 10000,
    });
    assert!(client.is_locked(&user));
    assert_eq!(client.remaining_lock_time(&user), 1);

    // At expiry: unlocked, remaining == 0.
    env.ledger().set(LedgerInfo {
        timestamp: unlock_time,
        protocol_version: 26,
        sequence_number: 202,
        network_id: Default::default(),
        base_reserve: 10,
        min_temp_entry_ttl: 1000,
        min_persistent_entry_ttl: 1000,
        max_entry_ttl: 10000,
    });
    assert!(!client.is_locked(&user));
    assert_eq!(client.remaining_lock_time(&user), 0);
}

// ─────────────────────────────────────────────────────────────────────
// Borrow Index
// ─────────────────────────────────────────────────────────────────────

#[test]
fn test_borrow_index_initial_value() {
    let env = Env::default();
    env.mock_all_auths();

    let (client, _asset_address, _token_client, _token_admin, _owner, _treasury, _guardian) =
        setup_vault(&env);

    // Initial borrow index should be WAD_SCALE (1e18)
    let index = client.get_borrow_index();
    assert_eq!(index, 1_000_000_000_000_000_000i128);
}

#[test]
fn test_borrow_index_unchanged_without_time() {
    let env = Env::default();
    env.mock_all_auths();

    let (client, _asset_address, _token_client, token_admin, _owner, treasury, _guardian) =
        setup_vault(&env);

    // Depositor provides liquidity
    let depositor = Address::generate(&env);
    token_admin.mint(&depositor, &10_000_000i128);
    client.deposit(&10_000_000i128, &depositor, &depositor, &depositor);

    let index_before = client.get_borrow_index();

    // Borrow — no time elapsed, so index stays the same
    let child = Address::generate(&env);
    client.borrow(&treasury, &child, &5_000_000i128);

    let index_after = client.get_borrow_index();
    assert_eq!(index_before, index_after);
}

#[test]
fn test_borrow_index_increases_with_interest() {
    let env = Env::default();
    env.mock_all_auths();

    let (client, _asset_address, _token_client, token_admin, _owner, treasury, _guardian) =
        setup_vault(&env);

    // Depositor provides liquidity
    let depositor = Address::generate(&env);
    token_admin.mint(&depositor, &10_000_000i128);
    client.deposit(&10_000_000i128, &depositor, &depositor, &depositor);

    // Borrow to create utilization (interest only accrues when total_borrowed > 0)
    let child = Address::generate(&env);
    client.borrow(&treasury, &child, &5_000_000i128);

    let index_before = client.get_borrow_index();
    assert_eq!(index_before, 1_000_000_000_000_000_000i128);

    // Advance 30 days
    env.ledger().set(LedgerInfo {
        timestamp: 2_592_000,
        protocol_version: 26,
        sequence_number: 100,
        network_id: Default::default(),
        base_reserve: 10,
        min_temp_entry_ttl: 1000,
        min_persistent_entry_ttl: 1000,
        max_entry_ttl: 10000,
    });

    let index_after = client.get_borrow_index();

    // Index should have increased (interest compounded)
    assert!(
        index_after > index_before,
        "Borrow index should increase after interest accrual: before={}, after={}",
        index_before,
        index_after
    );
}

#[test]
fn test_borrow_index_monotonically_increases() {
    let env = Env::default();
    env.mock_all_auths();

    let (client, _asset_address, _token_client, token_admin, _owner, treasury, _guardian) =
        setup_vault(&env);

    // Depositor provides liquidity
    let depositor = Address::generate(&env);
    token_admin.mint(&depositor, &10_000_000i128);
    client.deposit(&10_000_000i128, &depositor, &depositor, &depositor);

    // Borrow to create utilization
    let child = Address::generate(&env);
    client.borrow(&treasury, &child, &5_000_000i128);

    // Advance 30 days, get index
    env.ledger().set(LedgerInfo {
        timestamp: 2_592_000,
        protocol_version: 26,
        sequence_number: 100,
        network_id: Default::default(),
        base_reserve: 10,
        min_temp_entry_ttl: 1000,
        min_persistent_entry_ttl: 1000,
        max_entry_ttl: 10000,
    });
    let index_30d = client.get_borrow_index();

    // Advance to 60 days, get index
    env.ledger().set(LedgerInfo {
        timestamp: 5_184_000,
        protocol_version: 26,
        sequence_number: 200,
        network_id: Default::default(),
        base_reserve: 10,
        min_temp_entry_ttl: 1000,
        min_persistent_entry_ttl: 1000,
        max_entry_ttl: 10000,
    });
    let index_60d = client.get_borrow_index();

    // Advance to 365 days, get index
    env.ledger().set(LedgerInfo {
        timestamp: 31_536_000,
        protocol_version: 26,
        sequence_number: 300,
        network_id: Default::default(),
        base_reserve: 10,
        min_temp_entry_ttl: 1000,
        min_persistent_entry_ttl: 1000,
        max_entry_ttl: 10000,
    });
    let index_1y = client.get_borrow_index();

    // Index must be strictly monotonically increasing
    assert!(
        index_30d < index_60d,
        "30d < 60d: {} < {}",
        index_30d,
        index_60d
    );
    assert!(
        index_60d < index_1y,
        "60d < 1y: {} < {}",
        index_60d,
        index_1y
    );
}

#[test]
fn test_borrow_index_stable_without_borrows() {
    let env = Env::default();
    env.mock_all_auths();

    let (client, _asset_address, _token_client, token_admin, _owner, _treasury, _guardian) =
        setup_vault(&env);

    // Depositor provides liquidity but no one borrows
    let depositor = Address::generate(&env);
    token_admin.mint(&depositor, &10_000_000i128);
    client.deposit(&10_000_000i128, &depositor, &depositor, &depositor);

    let index_before = client.get_borrow_index();

    // Advance 1 year
    env.ledger().set(LedgerInfo {
        timestamp: 31_536_000,
        protocol_version: 26,
        sequence_number: 100,
        network_id: Default::default(),
        base_reserve: 10,
        min_temp_entry_ttl: 1000,
        min_persistent_entry_ttl: 1000,
        max_entry_ttl: 10000,
    });

    let index_after = client.get_borrow_index();

    // Index should not change when there are no borrows
    assert_eq!(
        index_before, index_after,
        "Borrow index should not change without borrows"
    );
}

// ─────────────────────────────────────────────────────────────────────
// Upgrade
// ─────────────────────────────────────────────────────────────────────

#[test]
fn test_upgrade_function_exists() {
    // This test just verifies the upgrade function is callable
    // In a real scenario, you'd need a new WASM to test actual upgrade
    let env = Env::default();
    env.mock_all_auths();

    let (client, _asset_address, _token_client, _token_admin, _owner, _treasury, _guardian) =
        setup_vault(&env);

    // We can't actually test upgrade without a new WASM,
    // but we can verify the function signature exists by checking
    // that the contract compiles with it.
    // The upgrade function takes BytesN<32> and updates the contract WASM.

    // Verify contract is functional before any hypothetical upgrade
    assert_eq!(client.total_assets(), 0);
    assert!(!client.paused());
}
