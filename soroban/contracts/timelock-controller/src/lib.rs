//! Microvault TimelockController
//!
//! Standalone governance contract that enforces time-delayed execution of
//! privileged vault operations. Designed to be set as the vault owner so that
//! all admin calls are subject to a configurable minimum delay.
//!
//! Built on the OpenZeppelin Stellar Contracts library:
//! <https://docs.openzeppelin.com/stellar-contracts>
//!
//! # Authors
//!
//! Samuel Mugane <smugane@shambarecords.com>
//! Peter Wesley <peter.wesley@shambarecords.com>

#![no_std]

use soroban_sdk::{
    auth::{Context, ContractContext, CustomAccountInterface},
    contract, contractimpl, contracttype,
    crypto::Hash,
    panic_with_error, symbol_short, Address, BytesN, Env, IntoVal, Symbol, Val, Vec,
};
use stellar_access::access_control::{
    ensure_role, get_role_member_count, grant_role_no_auth, set_admin, AccessControl,
};
use stellar_governance::timelock::{
    cancel_operation, execute_operation, get_min_delay as timelock_get_min_delay,
    get_operation_state, hash_operation as timelock_hash_operation, is_operation_done,
    is_operation_pending, is_operation_ready, schedule_operation, set_execute_operation,
    set_min_delay as timelock_set_min_delay, Operation, OperationState, TimelockError,
};
use stellar_macros::{only_admin, only_role};

const PROPOSER_ROLE: Symbol = symbol_short!("proposer");
const EXECUTOR_ROLE: Symbol = symbol_short!("executor");
const CANCELLER_ROLE: Symbol = symbol_short!("canceler");

/// Metadata attached to each auth context during self-administration via
/// [`CustomAccountInterface`]. Carries the predecessor, salt, and optional
/// executor needed to reconstruct and validate the operation.
#[contracttype]
#[derive(Clone, Debug, PartialEq)]
pub struct OperationMeta {
    pub predecessor: BytesN<32>,
    pub salt: BytesN<32>,
    pub executor: Option<Address>,
}

/// TimelockController — governance contract for time-delayed execution.
///
/// Wraps the OpenZeppelin `stellar-governance` timelock primitives with
/// role-based access control and a [`CustomAccountInterface`] for
/// self-administered operations.
#[contract]
pub struct TimelockController;

/// [`CustomAccountInterface`] implementation for self-administration.
///
/// When the controller needs to call its own admin functions (e.g.
/// `update_delay`), the caller invokes the function directly. Soroban
/// routes the auth check through `__check_auth`, which:
///
/// 1. Verifies the call targets this contract.
/// 2. If executors exist, requires an executor to authorize the call.
/// 3. Reconstructs the [`Operation`] and marks it as executed via
///    [`set_execute_operation`], which validates the delay has passed.
#[contractimpl]
impl CustomAccountInterface for TimelockController {
    type Error = TimelockError;
    type Signature = Vec<OperationMeta>;

    fn __check_auth(
        e: Env,
        _signature_payload: Hash<32>,
        context_meta: Vec<OperationMeta>,
        auth_contexts: Vec<Context>,
    ) -> Result<(), Self::Error> {
        for (context, meta) in auth_contexts.iter().zip(context_meta) {
            match context.clone() {
                Context::Contract(ContractContext {
                    contract,
                    fn_name,
                    args,
                }) => {
                    if contract != e.current_contract_address() {
                        panic_with_error!(&e, TimelockError::Unauthorized)
                    }

                    if get_role_member_count(&e, &EXECUTOR_ROLE) != 0 {
                        let args_for_auth = (
                            Symbol::new(&e, "execute_op"),
                            contract.clone(),
                            fn_name.clone(),
                            args.clone(),
                            meta.predecessor.clone(),
                            meta.salt.clone(),
                        )
                            .into_val(&e);

                        let executor = meta.executor.expect("Executor must be present");
                        ensure_role(&e, &EXECUTOR_ROLE, &executor);
                        executor.require_auth_for_args(args_for_auth);
                    }

                    let op = Operation {
                        target: contract,
                        function: fn_name,
                        args,
                        predecessor: meta.predecessor,
                        salt: meta.salt,
                    };
                    set_execute_operation(&e, &op);
                }
                _ => panic_with_error!(&e, TimelockError::Unauthorized),
            }
        }
        Ok(())
    }
}

#[contractimpl]
impl TimelockController {
    /// Initialize the timelock controller.
    ///
    /// # Arguments
    ///
    /// * `min_delay` - Minimum delay (in ledger sequence counts) before a scheduled
    ///   operation becomes executable.
    /// * `proposers` - Addresses granted the proposer role (and canceler role
    ///   automatically).
    /// * `executors` - Addresses granted the executor role. If empty, anyone
    ///   can execute ready operations.
    /// * `admin` - Access control admin. Defaults to the contract itself
    ///   (`None`) for self-administration.
    pub fn __constructor(
        e: &Env,
        min_delay: u32,
        proposers: Vec<Address>,
        executors: Vec<Address>,
        admin: Option<Address>,
    ) {
        let admin_addr = admin.unwrap_or_else(|| e.current_contract_address());
        set_admin(e, &admin_addr);

        for proposer in proposers.iter() {
            grant_role_no_auth(e, &proposer, &PROPOSER_ROLE, &admin_addr);
            grant_role_no_auth(e, &proposer, &CANCELLER_ROLE, &admin_addr);
        }

        for executor in executors.iter() {
            grant_role_no_auth(e, &executor, &EXECUTOR_ROLE, &admin_addr);
        }

        timelock_set_min_delay(e, min_delay);
    }

    /// Schedule an operation for time-delayed execution.
    ///
    /// Returns the operation ID (`BytesN<32>`) computed as the keccak256 hash
    /// of the operation parameters. The operation enters the `Waiting` state
    /// and becomes `Ready` after `delay` ledger sequence counts have passed.
    ///
    /// # Panics
    ///
    /// Panics if `proposer` does not hold the proposer role or if `delay` is
    /// less than the configured minimum delay.
    #[allow(clippy::too_many_arguments)]
    #[only_role(proposer, "proposer")]
    pub fn schedule_op(
        e: &Env,
        target: Address,
        function: Symbol,
        args: Vec<Val>,
        predecessor: BytesN<32>,
        salt: BytesN<32>,
        delay: u32,
        proposer: Address,
    ) -> BytesN<32> {
        let operation = Operation {
            target,
            function,
            args,
            predecessor,
            salt,
        };
        schedule_operation(e, &operation, delay)
    }

    /// Execute a ready operation.
    ///
    /// Calls `target.function(args)` via the timelock's `execute_operation`,
    /// which verifies the operation is in the `Ready` state and that any
    /// predecessor operation is `Done`.
    ///
    /// If executors are configured, `executor` must be `Some` and hold the
    /// executor role. If no executors exist, anyone can call this function.
    pub fn execute_op(
        e: &Env,
        target: Address,
        function: Symbol,
        args: Vec<Val>,
        predecessor: BytesN<32>,
        salt: BytesN<32>,
        executor: Option<Address>,
    ) -> Val {
        if get_role_member_count(e, &EXECUTOR_ROLE) != 0 {
            let executor = executor.expect("Executor must be present");
            ensure_role(e, &EXECUTOR_ROLE, &executor);
            executor.require_auth();
        }

        let operation = Operation {
            target,
            function,
            args,
            predecessor,
            salt,
        };
        execute_operation(e, &operation)
    }

    /// Cancel a pending operation, reverting it to the `Unset` state.
    ///
    /// # Panics
    ///
    /// Panics if `canceller` does not hold the canceler role or if the
    /// operation is not in a cancellable state (`Waiting`).
    #[only_role(canceller, "canceler")]
    pub fn cancel_op(e: &Env, operation_id: BytesN<32>, canceller: Address) {
        cancel_operation(e, &operation_id);
    }

    /// Update the minimum delay for new operations.
    ///
    /// This is an admin-only function. When the controller is self-administered,
    /// changing the delay must itself go through the timelock: schedule an
    /// operation targeting this contract's `update_delay`, wait for the delay,
    /// then call `update_delay` directly (validated by `__check_auth`).
    #[only_admin]
    pub fn update_delay(e: &Env, new_delay: u32) {
        timelock_set_min_delay(e, new_delay);
    }

    /// Upgrade the contract WASM. Admin only.
    ///
    /// When self-administered, upgrading must go through the timelock:
    /// schedule an operation targeting this contract's `upgrade`, wait for
    /// the delay, then execute it (validated by `__check_auth`).
    #[only_admin]
    pub fn upgrade(e: &Env, new_wasm_hash: BytesN<32>) {
        e.deployer().update_current_contract_wasm(new_wasm_hash);
    }

    /// Returns the minimum delay (in ledger sequence counts) for new operations.
    pub fn get_min_delay(e: &Env) -> u32 {
        timelock_get_min_delay(e)
    }

    /// Compute the operation ID for the given parameters without modifying state.
    ///
    /// The ID is the keccak256 hash of `(target, function, args, predecessor, salt)`.
    pub fn hash_operation(
        e: &Env,
        target: Address,
        function: Symbol,
        args: Vec<Val>,
        predecessor: BytesN<32>,
        salt: BytesN<32>,
    ) -> BytesN<32> {
        let operation = Operation {
            target,
            function,
            args,
            predecessor,
            salt,
        };
        timelock_hash_operation(e, &operation)
    }

    /// Returns the current state of an operation: `Unset`, `Waiting`, `Ready`,
    /// or `Done`.
    pub fn get_operation_state(e: &Env, operation_id: BytesN<32>) -> OperationState {
        get_operation_state(e, &operation_id)
    }

    /// Returns `true` if the operation is in the `Waiting` or `Ready` state.
    pub fn is_operation_pending(e: &Env, operation_id: BytesN<32>) -> bool {
        is_operation_pending(e, &operation_id)
    }

    /// Returns `true` if the operation delay has passed and it can be executed.
    pub fn is_operation_ready(e: &Env, operation_id: BytesN<32>) -> bool {
        is_operation_ready(e, &operation_id)
    }

    /// Returns `true` if the operation has been executed (terminal state).
    pub fn is_operation_done(e: &Env, operation_id: BytesN<32>) -> bool {
        is_operation_done(e, &operation_id)
    }
}

#[contractimpl(contracttrait)]
impl AccessControl for TimelockController {}

#[cfg(test)]
mod test;
