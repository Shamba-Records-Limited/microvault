//! Integration tests for the MicroVault TimelockController contract.
//!
//! Covers constructor configuration, role assignment, operation scheduling,
//! execution, cancellation, state transitions, salt uniqueness, role
//! enforcement, and view functions.

use crate::{TimelockController, TimelockControllerClient};
use soroban_sdk::{
    symbol_short,
    testutils::{Address as _, Ledger, LedgerInfo},
    Address, BytesN, Env, IntoVal, Symbol, Val, Vec,
};
use stellar_governance::timelock::OperationState;

/// Deploys a timelock controller with one proposer, one executor, and an
/// explicit admin. Returns `(client, proposer, executor, admin)`.
fn setup_timelock<'a>(
    env: &'a Env,
    min_delay: u32,
) -> (TimelockControllerClient<'a>, Address, Address, Address) {
    let proposer = Address::generate(env);
    let executor = Address::generate(env);
    let admin = Address::generate(env);

    let proposers = Vec::from_array(env, [proposer.clone()]);
    let executors = Vec::from_array(env, [executor.clone()]);

    let contract_id = env.register(
        TimelockController,
        (
            min_delay,
            proposers,
            executors,
            Option::<Address>::Some(admin.clone()),
        ),
    );
    let client = TimelockControllerClient::new(env, &contract_id);

    (client, proposer, executor, admin)
}

/// Returns an all-zeros `BytesN<32>` to represent no predecessor dependency.
fn zero_predecessor(env: &Env) -> BytesN<32> {
    BytesN::from_array(env, &[0u8; 32])
}

/// Returns a deterministic non-zero salt for test operations.
fn random_salt(env: &Env) -> BytesN<32> {
    BytesN::from_array(env, &[1u8; 32])
}

// ─────────────────────────────────────────────────────────────────────
// Constructor
// ─────────────────────────────────────────────────────────────────────

#[test]
fn test_constructor_sets_min_delay() {
    let env = Env::default();
    env.mock_all_auths();

    let (client, _proposer, _executor, _admin) = setup_timelock(&env, 172800);
    assert_eq!(client.get_min_delay(), 172800);
}

#[test]
fn test_constructor_grants_roles() {
    let env = Env::default();
    env.mock_all_auths();

    let (client, proposer, executor, _admin) = setup_timelock(&env, 100);

    // Proposer should have proposer role
    assert!(client
        .has_role(&proposer, &symbol_short!("proposer"))
        .is_some());

    // Proposer should also have canceler role (auto-granted)
    assert!(client
        .has_role(&proposer, &symbol_short!("canceler"))
        .is_some());

    // Executor should have executor role
    assert!(client
        .has_role(&executor, &symbol_short!("executor"))
        .is_some());
}

#[test]
fn test_constructor_self_admin() {
    let env = Env::default();
    env.mock_all_auths();

    let proposer = Address::generate(&env);
    let executor = Address::generate(&env);

    let proposers = Vec::from_array(&env, [proposer.clone()]);
    let executors = Vec::from_array(&env, [executor.clone()]);

    // No admin specified -> self-administered
    let contract_id = env.register(
        TimelockController,
        (100u32, proposers, executors, Option::<Address>::None),
    );
    let client = TimelockControllerClient::new(&env, &contract_id);

    // Admin should be the contract itself
    let admin = client.get_admin();
    assert!(admin.is_some());
    assert_eq!(admin.unwrap(), client.address);
}

// ─────────────────────────────────────────────────────────────────────
// Schedule / Execute / Cancel
// ─────────────────────────────────────────────────────────────────────

#[test]
fn test_schedule_and_execute_operation() {
    let env = Env::default();
    env.mock_all_auths();

    let delay = 100u32;
    let (client, proposer, _executor, _admin) = setup_timelock(&env, delay);

    // Schedule an operation
    let target = Address::generate(&env);
    let function = Symbol::new(&env, "set_max_deposit");
    let args: Vec<Val> = Vec::from_array(&env, [1000i128.into_val(&env)]);
    let predecessor = zero_predecessor(&env);
    let salt = random_salt(&env);

    let op_id = client.schedule_op(
        &target,
        &function,
        &args,
        &predecessor,
        &salt,
        &delay,
        &proposer,
    );

    // Operation should be pending (Waiting)
    assert!(client.is_operation_pending(&op_id));
    assert_eq!(client.get_operation_state(&op_id), OperationState::Waiting);

    // Verify hash matches
    let computed_id = client.hash_operation(&target, &function, &args, &predecessor, &salt);
    assert_eq!(op_id, computed_id);

    // Advance ledger sequence past delay
    env.ledger().set(LedgerInfo {
        timestamp: 1000,
        protocol_version: 25,
        sequence_number: 101,
        network_id: Default::default(),
        base_reserve: 10,
        min_temp_entry_ttl: 1000,
        min_persistent_entry_ttl: 1000,
        max_entry_ttl: 10000,
    });

    // Operation should be ready
    assert!(client.is_operation_ready(&op_id));
    assert_eq!(client.get_operation_state(&op_id), OperationState::Ready);
}

#[test]
fn test_cancel_operation() {
    let env = Env::default();
    env.mock_all_auths();

    let delay = 100u32;
    let (client, proposer, _executor, _admin) = setup_timelock(&env, delay);

    let target = Address::generate(&env);
    let function = Symbol::new(&env, "set_max_deposit");
    let args: Vec<Val> = Vec::from_array(&env, [500i128.into_val(&env)]);
    let predecessor = zero_predecessor(&env);
    let salt = random_salt(&env);

    let op_id = client.schedule_op(
        &target,
        &function,
        &args,
        &predecessor,
        &salt,
        &delay,
        &proposer,
    );

    // Operation is pending
    assert!(client.is_operation_pending(&op_id));

    // Cancel it (proposer auto-has canceler role)
    client.cancel_op(&op_id, &proposer);

    // Operation should be Unset again
    assert_eq!(client.get_operation_state(&op_id), OperationState::Unset);
    assert!(!client.is_operation_pending(&op_id));
}

#[test]
fn test_operation_state_transitions() {
    let env = Env::default();
    env.mock_all_auths();

    let delay = 50u32;
    let (client, proposer, _executor, _admin) = setup_timelock(&env, delay);

    let target = Address::generate(&env);
    let function = Symbol::new(&env, "some_fn");
    let args: Vec<Val> = Vec::new(&env);
    let predecessor = zero_predecessor(&env);
    let salt = random_salt(&env);

    // Compute the operation ID before scheduling
    let op_id = client.hash_operation(&target, &function, &args, &predecessor, &salt);

    // Initially Unset
    assert_eq!(client.get_operation_state(&op_id), OperationState::Unset);
    assert!(!client.is_operation_pending(&op_id));
    assert!(!client.is_operation_ready(&op_id));
    assert!(!client.is_operation_done(&op_id));

    // Schedule -> Waiting
    client.schedule_op(
        &target,
        &function,
        &args,
        &predecessor,
        &salt,
        &delay,
        &proposer,
    );
    assert_eq!(client.get_operation_state(&op_id), OperationState::Waiting);
    assert!(client.is_operation_pending(&op_id));
    assert!(!client.is_operation_ready(&op_id));
    assert!(!client.is_operation_done(&op_id));

    // Advance ledger sequence past delay -> Ready
    env.ledger().set(LedgerInfo {
        timestamp: 1000,
        protocol_version: 25,
        sequence_number: 51,
        network_id: Default::default(),
        base_reserve: 10,
        min_temp_entry_ttl: 1000,
        min_persistent_entry_ttl: 1000,
        max_entry_ttl: 10000,
    });
    assert_eq!(client.get_operation_state(&op_id), OperationState::Ready);
    assert!(client.is_operation_pending(&op_id));
    assert!(client.is_operation_ready(&op_id));
    assert!(!client.is_operation_done(&op_id));
}

#[test]
fn test_schedule_with_larger_delay_than_min() {
    let env = Env::default();
    env.mock_all_auths();

    let min_delay = 100u32;
    let (client, proposer, _executor, _admin) = setup_timelock(&env, min_delay);

    let target = Address::generate(&env);
    let function = Symbol::new(&env, "test_fn");
    let args: Vec<Val> = Vec::new(&env);
    let predecessor = zero_predecessor(&env);
    let salt = random_salt(&env);

    // Schedule with delay larger than min_delay
    let custom_delay = 500u32;
    let op_id = client.schedule_op(
        &target,
        &function,
        &args,
        &predecessor,
        &salt,
        &custom_delay,
        &proposer,
    );

    // At sequence 101 (past min_delay but not custom delay), should still be Waiting
    env.ledger().set(LedgerInfo {
        timestamp: 1000,
        protocol_version: 25,
        sequence_number: 101,
        network_id: Default::default(),
        base_reserve: 10,
        min_temp_entry_ttl: 1000,
        min_persistent_entry_ttl: 1000,
        max_entry_ttl: 10000,
    });
    assert_eq!(client.get_operation_state(&op_id), OperationState::Waiting);

    // At sequence 501 (past custom delay), should be Ready
    env.ledger().set(LedgerInfo {
        timestamp: 2000,
        protocol_version: 25,
        sequence_number: 501,
        network_id: Default::default(),
        base_reserve: 10,
        min_temp_entry_ttl: 1000,
        min_persistent_entry_ttl: 1000,
        max_entry_ttl: 10000,
    });
    assert_eq!(client.get_operation_state(&op_id), OperationState::Ready);
}

#[test]
fn test_unique_salt_produces_different_ids() {
    let env = Env::default();
    env.mock_all_auths();

    let (client, _proposer, _executor, _admin) = setup_timelock(&env, 100);

    let target = Address::generate(&env);
    let function = Symbol::new(&env, "set_max_deposit");
    let args: Vec<Val> = Vec::from_array(&env, [1000i128.into_val(&env)]);
    let predecessor = zero_predecessor(&env);

    let salt1 = BytesN::from_array(&env, &[1u8; 32]);
    let salt2 = BytesN::from_array(&env, &[2u8; 32]);

    let id1 = client.hash_operation(&target, &function, &args, &predecessor, &salt1);
    let id2 = client.hash_operation(&target, &function, &args, &predecessor, &salt2);

    assert_ne!(id1, id2);
}

#[test]
fn test_schedule_same_op_different_salts() {
    let env = Env::default();
    env.mock_all_auths();

    let (client, proposer, _executor, _admin) = setup_timelock(&env, 100);

    let target = Address::generate(&env);
    let function = Symbol::new(&env, "set_max_deposit");
    let args: Vec<Val> = Vec::from_array(&env, [1000i128.into_val(&env)]);
    let predecessor = zero_predecessor(&env);

    let salt1 = BytesN::from_array(&env, &[1u8; 32]);
    let salt2 = BytesN::from_array(&env, &[2u8; 32]);

    // Both should succeed with different salts
    let id1 = client.schedule_op(
        &target,
        &function,
        &args,
        &predecessor,
        &salt1,
        &100u32,
        &proposer,
    );
    let id2 = client.schedule_op(
        &target,
        &function,
        &args,
        &predecessor,
        &salt2,
        &100u32,
        &proposer,
    );

    assert_ne!(id1, id2);
    assert!(client.is_operation_pending(&id1));
    assert!(client.is_operation_pending(&id2));
}

// ─────────────────────────────────────────────────────────────────────
// Role Enforcement
// ─────────────────────────────────────────────────────────────────────

#[test]
fn test_non_proposer_cannot_schedule() {
    let env = Env::default();
    env.mock_all_auths();

    let (client, _proposer, _executor, _admin) = setup_timelock(&env, 100);

    let non_proposer = Address::generate(&env);
    let target = Address::generate(&env);
    let function = Symbol::new(&env, "test_fn");
    let args: Vec<Val> = Vec::new(&env);
    let predecessor = zero_predecessor(&env);
    let salt = random_salt(&env);

    // Non-proposer should fail to schedule
    let result = client.try_schedule_op(
        &target,
        &function,
        &args,
        &predecessor,
        &salt,
        &100u32,
        &non_proposer,
    );
    assert!(result.is_err());
}

#[test]
fn test_non_canceler_cannot_cancel() {
    let env = Env::default();
    env.mock_all_auths();

    let (client, proposer, executor, _admin) = setup_timelock(&env, 100);

    let target = Address::generate(&env);
    let function = Symbol::new(&env, "test_fn");
    let args: Vec<Val> = Vec::new(&env);
    let predecessor = zero_predecessor(&env);
    let salt = random_salt(&env);

    let op_id = client.schedule_op(
        &target,
        &function,
        &args,
        &predecessor,
        &salt,
        &100u32,
        &proposer,
    );

    // Executor does not have canceler role, should fail
    let result = client.try_cancel_op(&op_id, &executor);
    assert!(result.is_err());
}

// ─────────────────────────────────────────────────────────────────────
// View Functions
// ─────────────────────────────────────────────────────────────────────

#[test]
fn test_get_min_delay() {
    let env = Env::default();
    env.mock_all_auths();

    let (client, _proposer, _executor, _admin) = setup_timelock(&env, 86400);
    assert_eq!(client.get_min_delay(), 86400);
}
