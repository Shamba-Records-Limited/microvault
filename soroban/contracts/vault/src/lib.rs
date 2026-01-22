// MicroVault ERC-4626 Vault Implementation
// Built on OpenZeppelin Stellar Contracts Library
// Implements SEP-56 Tokenized Vault Standard for USDC lending pool

#![no_std]

use soroban_sdk::{
    contract, contracterror, contractevent, contractimpl, Address, BytesN, Env, String,
};
use stellar_access::ownable::{self, Ownable};
use stellar_contract_utils::pausable::{self as pausable_mod, Pausable};
use stellar_macros::{default_impl, only_owner, when_not_paused};
use stellar_tokens::{
    fungible::{Base, FungibleToken},
    vault::{FungibleVault, Vault},
};

// ============================================================================
// Custom Errors
// ============================================================================

#[contracterror]
#[derive(Copy, Clone, Debug, Eq, PartialEq, PartialOrd, Ord)]
#[repr(u32)]
pub enum MicroVaultError {
    /// Caller is not authorized for this operation
    Unauthorized = 1,
    /// Cannot sweep the underlying asset
    CannotSweepUnderlyingAsset = 2,
    /// Amount must be positive
    InvalidAmount = 3,
    /// Deposit exceeds maximum limit
    ExceedsMaxDeposit = 4,
    /// Withdrawal exceeds maximum limit
    ExceedsMaxWithdraw = 5,
    /// Treasury not set
    TreasuryNotSet = 6,
    /// Timelock not expired
    TimelockNotExpired = 7,
    /// No pending update
    NoPendingUpdate = 8,
    /// Borrow would exceed utilization cap
    ExceedsUtilizationCap = 9,
    /// Insufficient liquidity for withdrawal
    InsufficientLiquidity = 10,
    /// Repay amount exceeds debt
    RepayExceedsDebt = 11,
    /// Shares are locked and cannot be withdrawn
    SharesLocked = 12,
}

// ============================================================================
// Custom Events
// ============================================================================

/// Emitted when a treasury update is proposed
#[contractevent]
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct TreasuryUpdateProposed {
    pub current_treasury: Address,
    pub proposed_treasury: Address,
    pub execute_time: u64,
}

/// Emitted when a treasury update is executed
#[contractevent]
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct TreasuryUpdated {
    pub old_treasury: Address,
    pub new_treasury: Address,
}

/// Emitted when a treasury update is cancelled
#[contractevent]
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct TreasuryUpdateCancelled {
    pub treasury: Address,
}

/// Emitted when foreign assets are swept from the contract
#[contractevent]
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct ForeignAssetSwept {
    pub token: Address,
    pub recipient: Address,
    pub amount: i128,
}

/// Emitted when the max deposit limit is updated
#[contractevent]
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct MaxDepositUpdated {
    pub old_limit: i128,
    pub new_limit: i128,
}

/// Emitted when the max withdraw limit is updated
#[contractevent]
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct MaxWithdrawUpdated {
    pub old_limit: i128,
    pub new_limit: i128,
}

/// Emitted when the vault is paused
#[contractevent]
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct VaultPaused {
    pub by: Address,
}

/// Emitted when the vault is unpaused
#[contractevent]
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct VaultUnpaused {
    pub by: Address,
}

/// Emitted when treasury borrows from the vault
#[contractevent]
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct Borrowed {
    pub treasury: Address,
    pub recipient: Address,
    pub amount: i128,
    pub total_borrowed: i128,
}

/// Emitted when treasury repays to the vault
#[contractevent]
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct Repaid {
    pub treasury: Address,
    pub amount: i128,
    pub total_borrowed: i128,
}

/// Emitted when interest is accrued
#[contractevent]
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct InterestAccrued {
    pub interest_amount: i128,
    pub new_total_borrowed: i128,
    pub utilization_rate: i128,
}

/// Emitted when the lock period is updated
#[contractevent]
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct LockPeriodUpdated {
    pub old_period: u64,
    pub new_period: u64,
}

/// Emitted when a user's lock time is updated
#[contractevent]
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct UserLockUpdated {
    pub user: Address,
    pub unlock_time: u64,
}

// ============================================================================
// Storage Keys
// ============================================================================

#[soroban_sdk::contracttype]
pub enum DataKey {
    /// Treasury address for credit delegation
    Treasury,
    /// Pending treasury address for timelock
    PendingTreasury,
    /// Treasury update timestamp
    TreasuryUpdateTime,
    /// Maximum deposit limit
    MaxDeposit,
    /// Maximum withdrawal limit
    MaxWithdraw,
    /// Total amount borrowed by treasury
    TotalBorrowed,
    /// Last interest accrual timestamp
    LastAccrualTime,
    /// Lock period in seconds (0 = no lock)
    LockPeriod,
    /// User's unlock timestamp
    UserUnlockTime(Address),
}

// ============================================================================
// Constants
// ============================================================================

/// Treasury update timelock period (2 days in seconds)
const TIMELOCK_PERIOD: u64 = 172800;

/// Default maximum deposit limit (1M USDC with 6 decimals)
const DEFAULT_MAX_DEPOSIT: i128 = 1_000_000_000_000;

/// Default maximum withdrawal limit (1M USDC with 6 decimals)
const DEFAULT_MAX_WITHDRAW: i128 = 1_000_000_000_000;

/// Decimals offset for inflation attack protection
const DECIMALS_OFFSET: u32 = 6;

/// Default lock period (0 = no lock, disabled by default)
const DEFAULT_LOCK_PERIOD: u64 = 0;

// ============================================================================
// Credit Delegation Constants
// ============================================================================

/// Precision for rate calculations (18 decimals)
const RATE_PRECISION: i128 = 1_000_000_000_000_000_000;

/// Utilization cap (80% = 0.8 * RATE_PRECISION)
const UTILIZATION_CAP: i128 = 800_000_000_000_000_000;

/// Optimal utilization point (80% - same as cap for this model)
const OPTIMAL_UTILIZATION: i128 = 800_000_000_000_000_000;

/// Base interest rate per year (2% = 0.02 * RATE_PRECISION)
const BASE_RATE: i128 = 20_000_000_000_000_000;

/// Slope 1: Rate increase per utilization below optimal (8% max at optimal)
/// slope1 = (8% - 2%) / 80% = 7.5% per 100% utilization
const SLOPE1: i128 = 75_000_000_000_000_000;

/// Slope 2: Rate increase per utilization above optimal (steeper)
/// At 100% utilization: base + slope1_contribution + slope2_contribution
/// slope2 = 100% per 20% = 500% per 100% utilization (aggressive)
const SLOPE2: i128 = 5_000_000_000_000_000_000;

/// Seconds per year (for APR to per-second conversion)
const SECONDS_PER_YEAR: u64 = 31_536_000;

// ============================================================================
// Contract
// ============================================================================

#[contract]
pub struct MicroVaultContract;

#[contractimpl]
impl MicroVaultContract {
    /// Initialize the vault
    ///
    /// # Arguments
    /// * `owner` - Contract owner address (admin)
    /// * `asset` - Address of the underlying asset (e.g., USDC)
    /// * `treasury` - Treasury wallet address for credit delegation
    /// * `name` - Vault share token name
    /// * `symbol` - Vault share token symbol
    pub fn __constructor(
        e: &Env,
        owner: Address,
        asset: Address,
        treasury: Address,
        name: String,
        symbol: String,
    ) {
        // Set the contract owner
        ownable::set_owner(e, &owner);

        // Initialize the vault with the underlying asset
        Vault::set_asset(e, asset);

        // Set decimals offset for inflation attack protection
        Vault::set_decimals_offset(e, DECIMALS_OFFSET);

        // Set token metadata (decimals inherited from vault)
        Base::set_metadata(e, Vault::decimals(e), name, symbol);

        // Store treasury address
        e.storage().instance().set(&DataKey::Treasury, &treasury);

        // Initialize limits
        e.storage()
            .instance()
            .set(&DataKey::MaxDeposit, &DEFAULT_MAX_DEPOSIT);
        e.storage()
            .instance()
            .set(&DataKey::MaxWithdraw, &DEFAULT_MAX_WITHDRAW);

        // Initialize credit delegation state
        e.storage().instance().set(&DataKey::TotalBorrowed, &0i128);
        e.storage()
            .instance()
            .set(&DataKey::LastAccrualTime, &e.ledger().timestamp());
    }

    // ========================================================================
    // View Functions
    // ========================================================================

    /// Get the treasury address
    pub fn treasury(e: &Env) -> Result<Address, MicroVaultError> {
        e.storage()
            .instance()
            .get(&DataKey::Treasury)
            .ok_or(MicroVaultError::TreasuryNotSet)
    }

    /// Get the maximum deposit limit
    pub fn get_max_deposit(e: &Env) -> i128 {
        e.storage()
            .instance()
            .get(&DataKey::MaxDeposit)
            .unwrap_or(DEFAULT_MAX_DEPOSIT)
    }

    /// Get the maximum withdrawal limit
    pub fn get_max_withdraw(e: &Env) -> i128 {
        e.storage()
            .instance()
            .get(&DataKey::MaxWithdraw)
            .unwrap_or(DEFAULT_MAX_WITHDRAW)
    }

    /// Get total amount borrowed by treasury
    pub fn total_borrowed(e: &Env) -> i128 {
        e.storage()
            .instance()
            .get(&DataKey::TotalBorrowed)
            .unwrap_or(0)
    }

    /// Get available liquidity (assets in vault minus borrowed)
    pub fn available_liquidity(e: &Env) -> i128 {
        let token = Vault::query_asset(e);
        let token_client = soroban_sdk::token::Client::new(e, &token);
        let vault_balance = token_client.balance(&e.current_contract_address());
        vault_balance
    }

    /// Get total assets including borrowed (for share calculations)
    pub fn total_managed_assets(e: &Env) -> i128 {
        let available = Self::available_liquidity(e);
        let borrowed = Self::total_borrowed(e);
        available + borrowed
    }

    /// Get current utilization rate (borrowed / total_managed * RATE_PRECISION)
    pub fn utilization_rate(e: &Env) -> i128 {
        let borrowed = Self::total_borrowed(e);
        if borrowed == 0 {
            return 0;
        }
        let total_managed = Self::total_managed_assets(e);
        if total_managed == 0 {
            return 0;
        }
        (borrowed * RATE_PRECISION) / total_managed
    }

    /// Get current borrow APR based on utilization
    pub fn borrow_apr(e: &Env) -> i128 {
        let utilization = Self::utilization_rate(e);
        Self::calculate_borrow_rate(utilization)
    }

    /// Get the current lock period in seconds
    pub fn get_lock_period(e: &Env) -> u64 {
        e.storage()
            .instance()
            .get(&DataKey::LockPeriod)
            .unwrap_or(DEFAULT_LOCK_PERIOD)
    }

    /// Get a user's unlock timestamp
    pub fn get_unlock_time(e: &Env, user: Address) -> u64 {
        e.storage()
            .persistent()
            .get(&DataKey::UserUnlockTime(user))
            .unwrap_or(0)
    }

    /// Check if a user's shares are currently locked
    pub fn is_locked(e: &Env, user: Address) -> bool {
        let unlock_time = Self::get_unlock_time(e, user.clone());
        e.ledger().timestamp() < unlock_time
    }

    /// Get remaining lock time in seconds for a user
    pub fn remaining_lock_time(e: &Env, user: Address) -> u64 {
        let unlock_time = Self::get_unlock_time(e, user);
        unlock_time.saturating_sub(e.ledger().timestamp())
    }

    /// Calculate borrow rate based on utilization (internal)
    fn calculate_borrow_rate(utilization: i128) -> i128 {
        if utilization <= OPTIMAL_UTILIZATION {
            // Below optimal: base_rate + (utilization / optimal) * slope1
            BASE_RATE + (utilization * SLOPE1) / RATE_PRECISION
        } else {
            // Above optimal: base + slope1 contribution + excess * slope2
            let base_at_optimal = BASE_RATE + (OPTIMAL_UTILIZATION * SLOPE1) / RATE_PRECISION;
            let excess_utilization = utilization - OPTIMAL_UTILIZATION;
            base_at_optimal + (excess_utilization * SLOPE2) / RATE_PRECISION
        }
    }

    // ========================================================================
    // Admin Functions
    // ========================================================================

    /// Update maximum deposit limit (only owner)
    #[only_owner]
    pub fn set_max_deposit(e: &Env, new_limit: i128) {
        let old_limit: i128 = e
            .storage()
            .instance()
            .get(&DataKey::MaxDeposit)
            .unwrap_or(DEFAULT_MAX_DEPOSIT);

        e.storage().instance().set(&DataKey::MaxDeposit, &new_limit);

        MaxDepositUpdated {
            old_limit,
            new_limit,
        }
        .publish(e);
    }

    /// Update maximum withdrawal limit (only owner)
    #[only_owner]
    pub fn set_max_withdraw(e: &Env, new_limit: i128) {
        let old_limit: i128 = e
            .storage()
            .instance()
            .get(&DataKey::MaxWithdraw)
            .unwrap_or(DEFAULT_MAX_WITHDRAW);

        e.storage()
            .instance()
            .set(&DataKey::MaxWithdraw, &new_limit);

        MaxWithdrawUpdated {
            old_limit,
            new_limit,
        }
        .publish(e);
    }

    /// Update lock period for deposits (only owner)
    /// Set to 0 to disable locking
    #[only_owner]
    pub fn set_lock_period(e: &Env, new_period: u64) {
        let old_period: u64 = e
            .storage()
            .instance()
            .get(&DataKey::LockPeriod)
            .unwrap_or(DEFAULT_LOCK_PERIOD);

        e.storage()
            .instance()
            .set(&DataKey::LockPeriod, &new_period);

        LockPeriodUpdated {
            old_period,
            new_period,
        }
        .publish(e);
    }

    /// Upgrade contract to new WASM code (only owner)
    #[only_owner]
    pub fn upgrade(e: &Env, new_wasm_hash: BytesN<32>) {
        e.deployer().update_current_contract_wasm(new_wasm_hash);
    }

    // ========================================================================
    // Lock Management (Internal)
    // ========================================================================

    /// Update user's lock time using weighted average calculation
    /// This is called internally during deposits
    fn update_lock_time(e: &Env, user: &Address, existing_shares: i128, new_shares: i128) {
        let lock_period = Self::get_lock_period(e);

        // If lock period is 0, no locking is applied
        if lock_period == 0 {
            return;
        }

        let current_time = e.ledger().timestamp();

        // Get current unlock time (default to current time if not set)
        let current_unlock: u64 = e
            .storage()
            .persistent()
            .get(&DataKey::UserUnlockTime(user.clone()))
            .unwrap_or(current_time);

        // Calculate remaining lock time (0 if already unlocked)
        let remaining_lock = current_unlock.saturating_sub(current_time);

        // If no existing shares, apply full lock period
        if existing_shares == 0 {
            let unlock_time = current_time + lock_period;
            e.storage()
                .persistent()
                .set(&DataKey::UserUnlockTime(user.clone()), &unlock_time);

            UserLockUpdated {
                user: user.clone(),
                unlock_time,
            }
            .publish(e);
            return;
        }

        // Calculate weighted average lock time
        let total_shares = existing_shares + new_shares;
        let weighted_lock = ((remaining_lock as i128 * existing_shares)
            + (lock_period as i128 * new_shares))
            / total_shares;

        let new_unlock = current_time + (weighted_lock as u64);

        e.storage()
            .persistent()
            .set(&DataKey::UserUnlockTime(user.clone()), &new_unlock);

        UserLockUpdated {
            user: user.clone(),
            unlock_time: new_unlock,
        }
        .publish(e);
    }

    // ========================================================================
    // Treasury Management with Timelock
    // ========================================================================

    /// Propose a treasury update with timelock
    pub fn propose_treasury_update(
        e: &Env,
        current_treasury: Address,
        new_treasury: Address,
    ) -> Result<(), MicroVaultError> {
        current_treasury.require_auth();

        let treasury: Address = e
            .storage()
            .instance()
            .get(&DataKey::Treasury)
            .ok_or(MicroVaultError::TreasuryNotSet)?;

        if current_treasury != treasury {
            return Err(MicroVaultError::Unauthorized);
        }

        let update_time = e.ledger().timestamp() + TIMELOCK_PERIOD;

        e.storage()
            .instance()
            .set(&DataKey::PendingTreasury, &new_treasury);
        e.storage()
            .instance()
            .set(&DataKey::TreasuryUpdateTime, &update_time);

        TreasuryUpdateProposed {
            current_treasury,
            proposed_treasury: new_treasury,
            execute_time: update_time,
        }
        .publish(e);

        Ok(())
    }

    /// Execute pending treasury update after timelock
    pub fn execute_treasury_update(e: &Env) -> Result<(), MicroVaultError> {
        let update_time: u64 = e
            .storage()
            .instance()
            .get(&DataKey::TreasuryUpdateTime)
            .ok_or(MicroVaultError::NoPendingUpdate)?;

        if e.ledger().timestamp() < update_time {
            return Err(MicroVaultError::TimelockNotExpired);
        }

        let old_treasury: Address = e
            .storage()
            .instance()
            .get(&DataKey::Treasury)
            .ok_or(MicroVaultError::TreasuryNotSet)?;

        let new_treasury: Address = e
            .storage()
            .instance()
            .get(&DataKey::PendingTreasury)
            .ok_or(MicroVaultError::NoPendingUpdate)?;

        e.storage()
            .instance()
            .set(&DataKey::Treasury, &new_treasury);
        e.storage().instance().remove(&DataKey::PendingTreasury);
        e.storage().instance().remove(&DataKey::TreasuryUpdateTime);

        TreasuryUpdated {
            old_treasury,
            new_treasury,
        }
        .publish(e);

        Ok(())
    }

    /// Cancel pending treasury update
    pub fn cancel_treasury_update(
        e: &Env,
        current_treasury: Address,
    ) -> Result<(), MicroVaultError> {
        current_treasury.require_auth();

        let treasury: Address = e
            .storage()
            .instance()
            .get(&DataKey::Treasury)
            .ok_or(MicroVaultError::TreasuryNotSet)?;

        if current_treasury != treasury {
            return Err(MicroVaultError::Unauthorized);
        }

        e.storage().instance().remove(&DataKey::PendingTreasury);
        e.storage().instance().remove(&DataKey::TreasuryUpdateTime);

        TreasuryUpdateCancelled {
            treasury: current_treasury,
        }
        .publish(e);

        Ok(())
    }

    // ========================================================================
    // Emergency Functions
    // ========================================================================

    /// Emergency sweep function for foreign assets sent directly to contract ID
    pub fn sweep_foreign_asset(
        e: &Env,
        current_treasury: Address,
        token_to_recover: Address,
        recipient: Address,
        amount: i128,
    ) -> Result<(), MicroVaultError> {
        current_treasury.require_auth();

        let treasury: Address = e
            .storage()
            .instance()
            .get(&DataKey::Treasury)
            .ok_or(MicroVaultError::TreasuryNotSet)?;

        if current_treasury != treasury {
            return Err(MicroVaultError::Unauthorized);
        }

        // Cannot sweep the underlying asset
        let underlying_asset = Vault::query_asset(e);
        if token_to_recover == underlying_asset {
            return Err(MicroVaultError::CannotSweepUnderlyingAsset);
        }

        if amount <= 0 {
            return Err(MicroVaultError::InvalidAmount);
        }

        // Transfer the foreign token
        let token_client = soroban_sdk::token::Client::new(e, &token_to_recover);
        token_client.transfer(&e.current_contract_address(), &recipient, &amount);

        ForeignAssetSwept {
            token: token_to_recover,
            recipient,
            amount,
        }
        .publish(e);

        Ok(())
    }

    // ========================================================================
    // Credit Delegation Functions
    // ========================================================================

    /// Accrue interest on borrowed amount (called before any borrow/repay)
    fn accrue_interest(e: &Env) {
        let last_accrual: u64 = e
            .storage()
            .instance()
            .get(&DataKey::LastAccrualTime)
            .unwrap_or(e.ledger().timestamp());

        let current_time = e.ledger().timestamp();
        let time_elapsed = current_time.saturating_sub(last_accrual);

        if time_elapsed == 0 {
            return;
        }

        let total_borrowed = Self::total_borrowed(e);
        if total_borrowed == 0 {
            e.storage()
                .instance()
                .set(&DataKey::LastAccrualTime, &current_time);
            return;
        }

        // Calculate interest
        let utilization = Self::utilization_rate(e);
        let borrow_rate = Self::calculate_borrow_rate(utilization);

        // Interest = principal * rate * time / seconds_per_year
        // Using high precision to avoid rounding errors
        let interest = (total_borrowed * borrow_rate * (time_elapsed as i128))
            / (RATE_PRECISION * (SECONDS_PER_YEAR as i128));

        if interest > 0 {
            let new_total_borrowed = total_borrowed + interest;
            e.storage()
                .instance()
                .set(&DataKey::TotalBorrowed, &new_total_borrowed);

            InterestAccrued {
                interest_amount: interest,
                new_total_borrowed,
                utilization_rate: utilization,
            }
            .publish(e);
        }

        e.storage()
            .instance()
            .set(&DataKey::LastAccrualTime, &current_time);
    }

    /// Treasury borrows funds from the vault and sends to recipient (child account)
    ///
    /// # Arguments
    /// * `treasury_caller` - Treasury address (must sign)
    /// * `recipient` - Child account address to receive the funds
    /// * `amount` - Amount to borrow
    #[when_not_paused]
    pub fn borrow(
        e: &Env,
        treasury_caller: Address,
        recipient: Address,
        amount: i128,
    ) -> Result<(), MicroVaultError> {
        treasury_caller.require_auth();

        // Verify caller is treasury
        let treasury: Address = e
            .storage()
            .instance()
            .get(&DataKey::Treasury)
            .ok_or(MicroVaultError::TreasuryNotSet)?;

        if treasury_caller != treasury {
            return Err(MicroVaultError::Unauthorized);
        }

        if amount <= 0 {
            return Err(MicroVaultError::InvalidAmount);
        }

        // Accrue interest first
        Self::accrue_interest(e);

        // Check utilization cap
        let current_borrowed = Self::total_borrowed(e);
        let total_managed = Self::total_managed_assets(e);
        let new_borrowed = current_borrowed + amount;

        // Calculate new utilization after borrow
        let new_utilization = if total_managed > 0 {
            (new_borrowed * RATE_PRECISION) / total_managed
        } else {
            return Err(MicroVaultError::InvalidAmount);
        };

        if new_utilization > UTILIZATION_CAP {
            return Err(MicroVaultError::ExceedsUtilizationCap);
        }

        // Check available liquidity
        let available = Self::available_liquidity(e);
        if amount > available {
            return Err(MicroVaultError::InsufficientLiquidity);
        }

        // Update total borrowed
        e.storage()
            .instance()
            .set(&DataKey::TotalBorrowed, &new_borrowed);

        // Transfer funds directly to recipient (child account)
        let underlying_asset = Vault::query_asset(e);
        let token_client = soroban_sdk::token::Client::new(e, &underlying_asset);
        token_client.transfer(&e.current_contract_address(), &recipient, &amount);

        Borrowed {
            treasury: treasury_caller,
            recipient,
            amount,
            total_borrowed: new_borrowed,
        }
        .publish(e);

        Ok(())
    }

    /// Treasury repays borrowed funds to the vault
    #[when_not_paused]
    pub fn repay(e: &Env, treasury_caller: Address, amount: i128) -> Result<(), MicroVaultError> {
        treasury_caller.require_auth();

        // Verify caller is treasury
        let treasury: Address = e
            .storage()
            .instance()
            .get(&DataKey::Treasury)
            .ok_or(MicroVaultError::TreasuryNotSet)?;

        if treasury_caller != treasury {
            return Err(MicroVaultError::Unauthorized);
        }

        if amount <= 0 {
            return Err(MicroVaultError::InvalidAmount);
        }

        // Accrue interest first
        Self::accrue_interest(e);

        let current_borrowed = Self::total_borrowed(e);
        if amount > current_borrowed {
            return Err(MicroVaultError::RepayExceedsDebt);
        }

        // Transfer funds from treasury to vault
        let underlying_asset = Vault::query_asset(e);
        let token_client = soroban_sdk::token::Client::new(e, &underlying_asset);
        token_client.transfer(&treasury, &e.current_contract_address(), &amount);

        // Update total borrowed
        let new_borrowed = current_borrowed - amount;
        e.storage()
            .instance()
            .set(&DataKey::TotalBorrowed, &new_borrowed);

        Repaid {
            treasury: treasury_caller,
            amount,
            total_borrowed: new_borrowed,
        }
        .publish(e);

        Ok(())
    }

    /// Force interest accrual (can be called by anyone)
    pub fn accrue(e: &Env) {
        Self::accrue_interest(e);
    }
}

// ============================================================================
// FungibleToken Implementation (for share tokens)
// ============================================================================

#[default_impl]
#[contractimpl]
impl FungibleToken for MicroVaultContract {
    type ContractType = Vault;

    fn decimals(e: &Env) -> u32 {
        Vault::decimals(e)
    }
}

// ============================================================================
// FungibleVault Implementation
// ============================================================================

#[contractimpl]
impl FungibleVault for MicroVaultContract {
    /// Deposit assets and receive shares
    #[when_not_paused]
    fn deposit(e: &Env, assets: i128, receiver: Address, from: Address, operator: Address) -> i128 {
        operator.require_auth();

        // Check deposit limit
        let max_deposit: i128 = e
            .storage()
            .instance()
            .get(&DataKey::MaxDeposit)
            .unwrap_or(DEFAULT_MAX_DEPOSIT);

        if assets > max_deposit {
            panic!("Deposit exceeds maximum limit");
        }

        // Get existing shares BEFORE deposit for lock calculation
        let existing_shares = Base::balance(e, &receiver);

        // Perform the deposit (mints new shares)
        let new_shares = Vault::deposit(e, assets, receiver.clone(), from, operator);

        // Update lock time with weighted calculation
        MicroVaultContract::update_lock_time(e, &receiver, existing_shares, new_shares);

        new_shares
    }

    /// Mint shares by providing assets
    #[when_not_paused]
    fn mint(e: &Env, shares: i128, receiver: Address, from: Address, operator: Address) -> i128 {
        operator.require_auth();

        // Get existing shares BEFORE mint for lock calculation
        let existing_shares = Base::balance(e, &receiver);

        // Perform the mint
        let assets_used = Vault::mint(e, shares, receiver.clone(), from, operator);

        // Update lock time with weighted calculation
        MicroVaultContract::update_lock_time(e, &receiver, existing_shares, shares);

        assets_used
    }

    /// Withdraw assets by burning shares
    #[when_not_paused]
    fn withdraw(
        e: &Env,
        assets: i128,
        receiver: Address,
        owner: Address,
        operator: Address,
    ) -> i128 {
        operator.require_auth();

        // Check if shares are locked
        if MicroVaultContract::is_locked(e, owner.clone()) {
            panic!("Shares are locked");
        }

        // Check withdrawal limit
        let max_withdraw: i128 = e
            .storage()
            .instance()
            .get(&DataKey::MaxWithdraw)
            .unwrap_or(DEFAULT_MAX_WITHDRAW);

        if assets > max_withdraw {
            panic!("Withdrawal exceeds maximum limit");
        }

        // Check available liquidity (credit delegation constraint)
        let available = MicroVaultContract::available_liquidity(e);
        if assets > available {
            panic!("Insufficient liquidity for withdrawal");
        }

        Vault::withdraw(e, assets, receiver, owner, operator)
    }

    /// Redeem shares for assets
    #[when_not_paused]
    fn redeem(e: &Env, shares: i128, receiver: Address, owner: Address, operator: Address) -> i128 {
        operator.require_auth();

        // Check if shares are locked
        if MicroVaultContract::is_locked(e, owner.clone()) {
            panic!("Shares are locked");
        }

        // Check available liquidity (credit delegation constraint)
        let assets_to_receive = Vault::preview_redeem(e, shares);
        let available = MicroVaultContract::available_liquidity(e);
        if assets_to_receive > available {
            panic!("Insufficient liquidity for redemption");
        }

        Vault::redeem(e, shares, receiver, owner, operator)
    }

    fn convert_to_shares(e: &Env, assets: i128) -> i128 {
        Vault::convert_to_shares(e, assets)
    }

    fn convert_to_assets(e: &Env, shares: i128) -> i128 {
        Vault::convert_to_assets(e, shares)
    }

    fn query_asset(e: &Env) -> Address {
        Vault::query_asset(e)
    }

    fn total_assets(e: &Env) -> i128 {
        // Include borrowed amounts in total assets for share calculations
        // This ensures depositors earn interest from loans
        MicroVaultContract::total_managed_assets(e)
    }

    fn max_deposit(e: &Env, _receiver: Address) -> i128 {
        if pausable_mod::paused(e) {
            return 0;
        }
        e.storage()
            .instance()
            .get(&DataKey::MaxDeposit)
            .unwrap_or(DEFAULT_MAX_DEPOSIT)
    }

    fn max_mint(e: &Env, receiver: Address) -> i128 {
        if pausable_mod::paused(e) {
            return 0;
        }
        Vault::max_mint(e, receiver)
    }

    fn max_withdraw(e: &Env, owner: Address) -> i128 {
        if pausable_mod::paused(e) {
            return 0;
        }
        let limit: i128 = e
            .storage()
            .instance()
            .get(&DataKey::MaxWithdraw)
            .unwrap_or(DEFAULT_MAX_WITHDRAW);
        let owner_max = Vault::max_withdraw(e, owner);
        // Also constrained by available liquidity
        let available = MicroVaultContract::available_liquidity(e);
        owner_max.min(limit).min(available)
    }

    fn max_redeem(e: &Env, owner: Address) -> i128 {
        if pausable_mod::paused(e) {
            return 0;
        }
        let owner_max_shares = Vault::max_redeem(e, owner);
        // Check what assets those shares convert to
        let owner_max_assets = Vault::preview_redeem(e, owner_max_shares);
        // Constrain by available liquidity
        let available = MicroVaultContract::available_liquidity(e);
        if owner_max_assets <= available {
            owner_max_shares
        } else {
            // Convert available liquidity back to shares
            Vault::convert_to_shares(e, available)
        }
    }

    fn preview_deposit(e: &Env, assets: i128) -> i128 {
        Vault::preview_deposit(e, assets)
    }

    fn preview_mint(e: &Env, shares: i128) -> i128 {
        Vault::preview_mint(e, shares)
    }

    fn preview_withdraw(e: &Env, assets: i128) -> i128 {
        Vault::preview_withdraw(e, assets)
    }

    fn preview_redeem(e: &Env, shares: i128) -> i128 {
        Vault::preview_redeem(e, shares)
    }
}

// ============================================================================
// Ownable Implementation
// ============================================================================

#[default_impl]
#[contractimpl]
impl Ownable for MicroVaultContract {}

// ============================================================================
// Pausable Implementation
// ============================================================================

#[contractimpl]
impl Pausable for MicroVaultContract {
    fn paused(e: &Env) -> bool {
        pausable_mod::paused(e)
    }

    #[only_owner]
    fn pause(e: &Env, caller: Address) {
        caller.require_auth();
        pausable_mod::pause(e);

        VaultPaused { by: caller }.publish(e);
    }

    #[only_owner]
    fn unpause(e: &Env, caller: Address) {
        caller.require_auth();
        pausable_mod::unpause(e);

        VaultUnpaused { by: caller }.publish(e);
    }
}

// ============================================================================
// Tests
// ============================================================================

#[cfg(test)]
mod test;
