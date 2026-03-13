//! MicroVault ERC-4626 Vault
//!
//! SEP-56 / ERC-4626 tokenized vault for USDC credit delegation on Stellar,
//! built on the OpenZeppelin Stellar Contracts library. Depositors receive
//! share tokens representing their pro-rata claim on vault assets including
//! accrued interest.
//!
//! Built on the OpenZeppelin Stellar Contracts library:
//! <https://docs.openzeppelin.com/stellar-contracts>
//!
//! # Author
//!
//! Samuel Mugane <smugane@shambarecords.com>

#![no_std]

use soroban_sdk::{
    contract, contracterror, contractevent, contractimpl, Address, BytesN, Env, MuxedAddress,
    String,
};
use stellar_access::ownable::{self, Ownable};
use stellar_contract_utils::math::wad::{Wad, WAD_SCALE};
use stellar_contract_utils::pausable::{self as pausable_mod, Pausable};
use stellar_macros::{only_owner, when_not_paused};
use stellar_tokens::{
    fungible::{Base, FungibleToken},
    vault::{FungibleVault, Vault},
};

#[contracterror]
#[derive(Copy, Clone, Debug, Eq, PartialEq, PartialOrd, Ord)]
#[repr(u32)]
pub enum MicroVaultError {
    Unauthorized = 1,
    CannotSweepUnderlyingAsset = 2,
    InvalidAmount = 3,
    ExceedsMaxDeposit = 4,
    ExceedsMaxWithdraw = 5,
    TreasuryNotSet = 6,
    ExceedsUtilizationCap = 9,
    InsufficientLiquidity = 10,
    RepayExceedsDebt = 11,
    SharesLocked = 12,
}

/// Emitted when the treasury address is changed.
#[contractevent]
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct TreasuryUpdated {
    pub old_treasury: Address,
    pub new_treasury: Address,
}

/// Emitted when foreign assets are recovered from the contract.
#[contractevent]
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct ForeignAssetSwept {
    pub token: Address,
    pub recipient: Address,
    pub amount: i128,
}

/// Emitted when the maximum deposit limit is changed.
#[contractevent]
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct MaxDepositUpdated {
    pub old_limit: i128,
    pub new_limit: i128,
}

/// Emitted when the maximum withdrawal limit is changed.
#[contractevent]
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct MaxWithdrawUpdated {
    pub old_limit: i128,
    pub new_limit: i128,
}

/// Emitted when the vault is paused by the owner or guardian.
#[contractevent]
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct VaultPaused {
    pub by: Address,
}

/// Emitted when the vault is unpaused by the owner.
#[contractevent]
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct VaultUnpaused {
    pub by: Address,
}

/// Emitted when the guardian address is changed.
#[contractevent]
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct GuardianUpdated {
    pub old_guardian: Address,
    pub new_guardian: Address,
}

/// Emitted when the treasury borrows from the vault.
#[contractevent]
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct Borrowed {
    pub treasury: Address,
    pub recipient: Address,
    pub amount: i128,
    pub total_borrowed: i128,
}

/// Emitted when the treasury repays borrowed funds.
#[contractevent]
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct Repaid {
    pub treasury: Address,
    pub amount: i128,
    pub total_borrowed: i128,
}

/// Emitted when compound interest is accrued on borrowed funds.
#[contractevent]
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct InterestAccrued {
    pub interest_amount: i128,
    pub new_total_borrowed: i128,
    pub utilization_rate: i128,
}

/// Emitted when the deposit lock period configuration is changed.
#[contractevent]
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct LockPeriodUpdated {
    pub old_period: u64,
    pub new_period: u64,
}

/// Emitted when a user's share unlock time is updated (on deposit/mint).
#[contractevent]
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct UserLockUpdated {
    pub user: Address,
    pub unlock_time: u64,
}

#[soroban_sdk::contracttype]
pub enum DataKey {
    Treasury,
    Guardian,
    MaxDeposit,
    MaxWithdraw,
    TotalBorrowed,
    LastAccrualTime,
    LockPeriod,
    UserUnlockTime(Address),
    BorrowIndex, // Cumulative debt index
}

/// Default maximum deposit limit (1M USDC with 6 decimals).
const DEFAULT_MAX_DEPOSIT: i128 = 1_000_000_000_000;

/// Default maximum withdrawal limit (1M USDC with 6 decimals).
const DEFAULT_MAX_WITHDRAW: i128 = 1_000_000_000_000;

/// Decimals offset for share-inflation attack protection.
const DECIMALS_OFFSET: u32 = 6;

/// Default lock period — 0 disables locking.
const DEFAULT_LOCK_PERIOD: u64 = 0;

/// Utilization cap: 80% in WAD (0.8 * 10^18).
const UTILIZATION_CAP: i128 = 800_000_000_000_000_000;

/// Optimal utilization point for the interest rate model (80% in WAD).
const OPTIMAL_UTILIZATION: i128 = 800_000_000_000_000_000;

/// Base interest rate: 2% APR in WAD.
const BASE_RATE: i128 = 20_000_000_000_000_000;

/// Slope 1: 7.5% per 100% utilization (below optimal). At 80% utilization the
/// slope-1 contribution is 6%, giving a total of 8% APR at optimal.
const SLOPE1: i128 = 75_000_000_000_000_000;

/// Slope 2: 500% per 100% utilization (above optimal). Creates a steep
/// penalty curve that discourages utilization beyond the optimal point.
const SLOPE2: i128 = 5_000_000_000_000_000_000;

/// Seconds per year, used for APR-to-per-second rate conversion.
const SECONDS_PER_YEAR: u64 = 31_536_000;

/// MicroVault ERC-4626 tokenized vault contract.
#[contract]
pub struct MicroVaultContract;

#[contractimpl]
impl MicroVaultContract {
    /// Initialize the vault with the underlying asset and initial configuration.
    ///
    /// # Arguments
    ///
    /// * `owner` - Contract owner (typically the TimelockController address).
    /// * `guardian` - Guardian address that can pause the vault immediately.
    /// * `asset` - Underlying asset address (e.g. USDC).
    /// * `treasury` - Treasury wallet for credit delegation operations.
    /// * `name` - Share token name (e.g. "MicroVault USDC Shares").
    /// * `symbol` - Share token symbol (e.g. "mvUSDC").
    pub fn __constructor(
        e: &Env,
        owner: Address,
        guardian: Address,
        asset: Address,
        treasury: Address,
        name: String,
        symbol: String,
    ) {
        ownable::set_owner(e, &owner);
        e.storage().instance().set(&DataKey::Guardian, &guardian);
        Vault::set_asset(e, asset);
        Vault::set_decimals_offset(e, DECIMALS_OFFSET);
        Base::set_metadata(e, Vault::decimals(e), name, symbol);
        e.storage().instance().set(&DataKey::Treasury, &treasury);
        e.storage()
            .instance()
            .set(&DataKey::MaxDeposit, &DEFAULT_MAX_DEPOSIT);
        e.storage()
            .instance()
            .set(&DataKey::MaxWithdraw, &DEFAULT_MAX_WITHDRAW);
        e.storage().instance().set(&DataKey::TotalBorrowed, &0i128);
        e.storage()
            .instance()
            .set(&DataKey::LastAccrualTime, &e.ledger().timestamp());
        e.storage()
            .instance()
            .set(&DataKey::BorrowIndex, &WAD_SCALE);
    }

    // ─────────────────────────────────────────────────────────────────────
    // View Functions
    // ─────────────────────────────────────────────────────────────────────

    /// Returns the treasury address.
    pub fn treasury(e: &Env) -> Result<Address, MicroVaultError> {
        e.storage()
            .instance()
            .get(&DataKey::Treasury)
            .ok_or(MicroVaultError::TreasuryNotSet)
    }

    /// Returns the guardian address, if set.
    pub fn guardian(e: &Env) -> Option<Address> {
        e.storage().instance().get(&DataKey::Guardian)
    }

    /// Returns the per-transaction maximum deposit limit.
    pub fn get_max_deposit(e: &Env) -> i128 {
        e.storage()
            .instance()
            .get(&DataKey::MaxDeposit)
            .unwrap_or(DEFAULT_MAX_DEPOSIT)
    }

    /// Returns the per-transaction maximum withdrawal limit.
    pub fn get_max_withdraw(e: &Env) -> i128 {
        e.storage()
            .instance()
            .get(&DataKey::MaxWithdraw)
            .unwrap_or(DEFAULT_MAX_WITHDRAW)
    }

    /// Returns the total amount currently borrowed by the treasury.
    pub fn total_borrowed(e: &Env) -> i128 {
        e.storage()
            .instance()
            .get(&DataKey::TotalBorrowed)
            .unwrap_or(0)
    }

    /// Returns available liquidity (contract token balance, excluding borrowed).
    pub fn available_liquidity(e: &Env) -> i128 {
        let token = Vault::query_asset(e);
        let token_client = soroban_sdk::token::Client::new(e, &token);
        token_client.balance(&e.current_contract_address())
    }

    /// Raw total managed assets without accruing interest first.
    /// Used internally by `accrue_interest` and `utilization_rate` to avoid
    /// circular recursion.
    fn raw_total_managed_assets(e: &Env) -> i128 {
        Self::available_liquidity(e) + Self::total_borrowed(e)
    }

    /// Returns total managed assets: available liquidity plus outstanding borrows.
    ///
    /// This is the denominator for share-to-asset conversions, ensuring that
    /// depositors' shares appreciate as interest accrues on borrowed funds.
    /// Interest is accrued first so that the returned value is always current.
    pub fn total_managed_assets(e: &Env) -> i128 {
        Self::accrue_interest(e);
        Self::raw_total_managed_assets(e)
    }

    /// Returns the current utilization rate as a WAD-scaled value (18 decimals).
    ///
    /// Utilization = total_borrowed / total_managed_assets.
    pub fn utilization_rate(e: &Env) -> i128 {
        Self::accrue_interest(e);
        let borrowed = Self::total_borrowed(e);
        if borrowed == 0 {
            return 0;
        }
        let total_managed = Self::raw_total_managed_assets(e);
        if total_managed == 0 {
            return 0;
        }
        Wad::from_ratio(e, borrowed, total_managed).raw()
    }

    /// Returns the cumulative borrow index (WAD scale, 1e18 = 1.0).
    ///
    /// Accrues interest first so the index is always current.
    pub fn get_borrow_index(e: &Env) -> i128 {
        Self::accrue_interest(e);
        e.storage()
            .instance()
            .get(&DataKey::BorrowIndex)
            .unwrap_or(WAD_SCALE)
    }

    /// Returns the current borrow APR as a WAD-scaled value based on utilization.
    pub fn borrow_apr(e: &Env) -> i128 {
        let utilization = Self::utilization_rate(e);
        Self::calculate_borrow_rate(e, utilization)
    }

    /// Returns the current deposit lock period in seconds (0 = disabled).
    pub fn get_lock_period(e: &Env) -> u64 {
        e.storage()
            .instance()
            .get(&DataKey::LockPeriod)
            .unwrap_or(DEFAULT_LOCK_PERIOD)
    }

    /// Returns the ledger timestamp at which `user`'s shares unlock.
    pub fn get_unlock_time(e: &Env, user: Address) -> u64 {
        e.storage()
            .persistent()
            .get(&DataKey::UserUnlockTime(user))
            .unwrap_or(0)
    }

    /// Returns `true` if `user`'s shares are currently locked.
    pub fn is_locked(e: &Env, user: Address) -> bool {
        let unlock_time = Self::get_unlock_time(e, user.clone());
        e.ledger().timestamp() < unlock_time
    }

    /// Returns the remaining lock time in seconds for `user` (0 if unlocked).
    pub fn remaining_lock_time(e: &Env, user: Address) -> u64 {
        let unlock_time = Self::get_unlock_time(e, user);
        unlock_time.saturating_sub(e.ledger().timestamp())
    }

    /// Dual-slope interest rate model.
    ///
    /// Below optimal utilization: `base_rate + utilization * slope1`.
    /// Above optimal: adds `(utilization - optimal) * slope2` on top.
    fn calculate_borrow_rate(e: &Env, utilization: i128) -> i128 {
        let util_wad = Wad::from_raw(utilization);
        let base_wad = Wad::from_raw(BASE_RATE);
        let slope1_wad = Wad::from_raw(SLOPE1);
        let slope2_wad = Wad::from_raw(SLOPE2);

        if utilization <= OPTIMAL_UTILIZATION {
            let slope_contribution = util_wad
                .checked_mul(e, slope1_wad)
                .unwrap_or(Wad::from_raw(0));
            (base_wad + slope_contribution).raw()
        } else {
            let optimal_wad = Wad::from_raw(OPTIMAL_UTILIZATION);
            let slope1_at_optimal = optimal_wad
                .checked_mul(e, slope1_wad)
                .unwrap_or(Wad::from_raw(0));
            let base_at_optimal = base_wad + slope1_at_optimal;
            let excess = Wad::from_raw(utilization - OPTIMAL_UTILIZATION);
            let excess_contribution = excess
                .checked_mul(e, slope2_wad)
                .unwrap_or(Wad::from_raw(0));
            (base_at_optimal + excess_contribution).raw()
        }
    }

    // ─────────────────────────────────────────────────────────────────────
    // Admin Functions
    // ─────────────────────────────────────────────────────────────────────

    /// Set the per-transaction maximum deposit limit. Owner only.
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

    /// Set the per-transaction maximum withdrawal limit. Owner only.
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

    /// Set the deposit lock period in seconds. Owner only.
    ///
    /// Set to 0 to disable locking. Existing locks are not retroactively
    /// changed; only new deposits are affected.
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

    /// Update the guardian address. Owner only.
    ///
    /// The guardian can pause the vault immediately without going through the
    /// timelock, enabling rapid emergency response.
    #[only_owner]
    pub fn set_guardian(e: &Env, new_guardian: Address) {
        let old_guardian: Address = e
            .storage()
            .instance()
            .get(&DataKey::Guardian)
            .unwrap_or_else(|| panic!("Guardian not set"));
        e.storage()
            .instance()
            .set(&DataKey::Guardian, &new_guardian);
        GuardianUpdated {
            old_guardian,
            new_guardian,
        }
        .publish(e);
    }

    /// Upgrade the contract WASM. Owner only.
    #[only_owner]
    pub fn upgrade(e: &Env, new_wasm_hash: BytesN<32>) {
        e.deployer().update_current_contract_wasm(new_wasm_hash);
    }

    // ─────────────────────────────────────────────────────────────────────
    // Lock Management (Internal)
    // ─────────────────────────────────────────────────────────────────────

    /// Compute a weighted-average unlock time when a user deposits additional
    /// shares on top of existing ones.
    ///
    /// Formula: `new_unlock = now + (remaining * existing + lock_period * new) / total`
    fn update_lock_time(e: &Env, user: &Address, existing_shares: i128, new_shares: i128) {
        let lock_period = Self::get_lock_period(e);
        if lock_period == 0 {
            return;
        }

        let current_time = e.ledger().timestamp();
        let current_unlock: u64 = e
            .storage()
            .persistent()
            .get(&DataKey::UserUnlockTime(user.clone()))
            .unwrap_or(current_time);
        let remaining_lock = current_unlock.saturating_sub(current_time);

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

    // ─────────────────────────────────────────────────────────────────────
    // Treasury Management
    // ─────────────────────────────────────────────────────────────────────

    /// Update the treasury address. Owner only.
    ///
    /// When the vault owner is a [`TimelockController`], this function is
    /// invoked via `execute_op` after a time delay, providing on-chain
    /// transparency before the change takes effect.
    #[only_owner]
    pub fn set_treasury(e: &Env, new_treasury: Address) {
        let old_treasury: Address = e
            .storage()
            .instance()
            .get(&DataKey::Treasury)
            .unwrap_or_else(|| panic!("Treasury not set"));
        e.storage()
            .instance()
            .set(&DataKey::Treasury, &new_treasury);
        TreasuryUpdated {
            old_treasury,
            new_treasury,
        }
        .publish(e);
    }

    // ─────────────────────────────────────────────────────────────────────
    // Emergency Functions
    // ─────────────────────────────────────────────────────────────────────

    /// Recover foreign tokens mistakenly sent to the vault. Treasury only.
    ///
    /// Cannot be used to sweep the underlying asset (e.g. USDC).
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

        let underlying_asset = Vault::query_asset(e);
        if token_to_recover == underlying_asset {
            return Err(MicroVaultError::CannotSweepUnderlyingAsset);
        }
        if amount <= 0 {
            return Err(MicroVaultError::InvalidAmount);
        }

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

    // ─────────────────────────────────────────────────────────────────────
    // Credit Delegation
    // ─────────────────────────────────────────────────────────────────────

    /// Accrue compound interest on the outstanding borrow using per-second
    /// compounding via `Wad::pow`.
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

        // Compute utilization inline to avoid circular call through
        // utilization_rate → total_managed_assets → accrue_interest.
        let total_managed = Self::raw_total_managed_assets(e);
        let utilization = if total_managed > 0 {
            Wad::from_ratio(e, total_borrowed, total_managed).raw()
        } else {
            0
        };
        let annual_rate = Wad::from_raw(Self::calculate_borrow_rate(e, utilization));
        let rate_per_second = annual_rate
            .checked_div_int(SECONDS_PER_YEAR as i128)
            .unwrap_or(Wad::from_raw(0));

        // compound_factor = (1 + rate_per_second) ^ time_elapsed
        let one = Wad::from_raw(WAD_SCALE);
        let base = one + rate_per_second;
        let exponent = if time_elapsed > u32::MAX as u64 {
            u32::MAX
        } else {
            time_elapsed as u32
        };
        let compound_factor = base.pow(e, exponent);

        let new_total_borrowed = compound_factor
            .checked_mul_int(total_borrowed)
            .map(|w| w.to_integer())
            .unwrap_or(total_borrowed);

        // Update cumulative borrow index
        let old_index: i128 = e
            .storage()
            .instance()
            .get(&DataKey::BorrowIndex)
            .unwrap_or(WAD_SCALE);
        let old_index_wad = Wad::from_raw(old_index);
        let new_index = old_index_wad
            .checked_mul(e, compound_factor)
            .unwrap_or(old_index_wad);
        e.storage()
            .instance()
            .set(&DataKey::BorrowIndex, &new_index.raw());

        let interest = new_total_borrowed - total_borrowed;
        if interest > 0 {
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

    /// Borrow funds from the vault and transfer to `recipient`. Treasury only.
    ///
    /// Accrues interest before processing. Enforces the 80% utilization cap
    /// and checks available liquidity.
    #[when_not_paused]
    pub fn borrow(
        e: &Env,
        treasury_caller: Address,
        recipient: Address,
        amount: i128,
    ) -> Result<(), MicroVaultError> {
        treasury_caller.require_auth();

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

        Self::accrue_interest(e);

        let current_borrowed = Self::total_borrowed(e);
        let total_managed = Self::raw_total_managed_assets(e);
        let new_borrowed = current_borrowed + amount;

        let new_utilization = if total_managed > 0 {
            Wad::from_ratio(e, new_borrowed, total_managed).raw()
        } else {
            return Err(MicroVaultError::InvalidAmount);
        };
        if new_utilization > UTILIZATION_CAP {
            return Err(MicroVaultError::ExceedsUtilizationCap);
        }

        let available = Self::available_liquidity(e);
        if amount > available {
            return Err(MicroVaultError::InsufficientLiquidity);
        }

        e.storage()
            .instance()
            .set(&DataKey::TotalBorrowed, &new_borrowed);

        let underlying_asset = Vault::query_asset(e);
        let token_client = soroban_sdk::token::Client::new(e, &underlying_asset);
        token_client.transfer(&e.current_contract_address(), &treasury, &amount);

        Borrowed {
            treasury: treasury_caller,
            recipient,
            amount,
            total_borrowed: new_borrowed,
        }
        .publish(e);

        Ok(())
    }

    /// Repay borrowed funds to the vault. Treasury only.
    ///
    /// Accrues interest before processing. Panics if `amount` exceeds
    /// outstanding debt.
    #[when_not_paused]
    pub fn repay(e: &Env, treasury_caller: Address, amount: i128) -> Result<(), MicroVaultError> {
        treasury_caller.require_auth();

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

        Self::accrue_interest(e);

        let current_borrowed = Self::total_borrowed(e);
        if amount > current_borrowed {
            return Err(MicroVaultError::RepayExceedsDebt);
        }

        let underlying_asset = Vault::query_asset(e);
        let token_client = soroban_sdk::token::Client::new(e, &underlying_asset);
        token_client.transfer(&treasury_caller, e.current_contract_address(), &amount);

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

    /// Trigger interest accrual. Callable by anyone.
    pub fn accrue(e: &Env) {
        Self::accrue_interest(e);
    }
}

#[contractimpl(contracttrait)]
impl FungibleToken for MicroVaultContract {
    type ContractType = Vault;

    fn decimals(e: &Env) -> u32 {
        Vault::decimals(e)
    }
}

#[contractimpl]
impl FungibleVault for MicroVaultContract {
    #[when_not_paused]
    fn deposit(e: &Env, assets: i128, receiver: Address, from: Address, operator: Address) -> i128 {
        let max_deposit: i128 = e
            .storage()
            .instance()
            .get(&DataKey::MaxDeposit)
            .unwrap_or(DEFAULT_MAX_DEPOSIT);
        if assets > max_deposit {
            panic!("Deposit exceeds maximum limit");
        }

        let existing_shares = Base::balance(e, &receiver);
        let new_shares = Vault::deposit(e, assets, receiver.clone(), from, operator);
        MicroVaultContract::update_lock_time(e, &receiver, existing_shares, new_shares);
        new_shares
    }

    #[when_not_paused]
    fn mint(e: &Env, shares: i128, receiver: Address, from: Address, operator: Address) -> i128 {
        let existing_shares = Base::balance(e, &receiver);
        let assets_used = Vault::mint(e, shares, receiver.clone(), from, operator);
        MicroVaultContract::update_lock_time(e, &receiver, existing_shares, shares);
        assets_used
    }

    #[when_not_paused]
    fn withdraw(
        e: &Env,
        assets: i128,
        receiver: Address,
        owner: Address,
        operator: Address,
    ) -> i128 {
        if MicroVaultContract::is_locked(e, owner.clone()) {
            panic!("Shares are locked");
        }

        let max_withdraw: i128 = e
            .storage()
            .instance()
            .get(&DataKey::MaxWithdraw)
            .unwrap_or(DEFAULT_MAX_WITHDRAW);
        if assets > max_withdraw {
            panic!("Withdrawal exceeds maximum limit");
        }

        let available = MicroVaultContract::available_liquidity(e);
        if assets > available {
            panic!("Insufficient liquidity for withdrawal");
        }

        Vault::withdraw(e, assets, receiver, owner, operator)
    }

    #[when_not_paused]
    fn redeem(e: &Env, shares: i128, receiver: Address, owner: Address, operator: Address) -> i128 {
        if MicroVaultContract::is_locked(e, owner.clone()) {
            panic!("Shares are locked");
        }

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

    /// Returns total assets including outstanding borrows so that share
    /// holders benefit from accrued interest.
    fn total_assets(e: &Env) -> i128 {
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
        let available = MicroVaultContract::available_liquidity(e);
        owner_max.min(limit).min(available)
    }

    fn max_redeem(e: &Env, owner: Address) -> i128 {
        if pausable_mod::paused(e) {
            return 0;
        }
        let owner_max_shares = Vault::max_redeem(e, owner);
        let owner_max_assets = Vault::preview_redeem(e, owner_max_shares);
        let available = MicroVaultContract::available_liquidity(e);
        if owner_max_assets <= available {
            owner_max_shares
        } else {
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

#[contractimpl(contracttrait)]
impl Ownable for MicroVaultContract {}

#[contractimpl]
impl Pausable for MicroVaultContract {
    fn paused(e: &Env) -> bool {
        pausable_mod::paused(e)
    }

    /// Pause the vault. Callable by the owner **or** the guardian.
    ///
    /// This is intentionally not gated by `#[only_owner]` so that the
    /// guardian can trigger an immediate emergency pause without going
    /// through the TimelockController delay.
    fn pause(e: &Env, caller: Address) {
        caller.require_auth();

        let is_owner = ownable::get_owner(e).map(|o| o == caller).unwrap_or(false);
        let is_guardian: bool = e
            .storage()
            .instance()
            .get(&DataKey::Guardian)
            .map(|g: Address| g == caller)
            .unwrap_or(false);
        if !is_owner && !is_guardian {
            panic!("Unauthorized: caller is not owner or guardian");
        }

        pausable_mod::pause(e);
        VaultPaused { by: caller }.publish(e);
    }

    /// Unpause the vault. Owner only (timelocked).
    #[only_owner]
    fn unpause(e: &Env, caller: Address) {
        caller.require_auth();
        pausable_mod::unpause(e);
        VaultUnpaused { by: caller }.publish(e);
    }
}

#[cfg(test)]
mod test;
