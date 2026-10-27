//! Microvault SEP-56 Vault
//!
//! SEP-56 tokenized vault for USDC credit delegation on Stellar,
//! built on the OpenZeppelin Stellar Contracts library. Depositors receive
//! share tokens representing their pro-rata claim on vault assets including
//! accrued interest.
//!
//! Built on the OpenZeppelin Stellar Contracts library:
//! <https://docs.openzeppelin.com/stellar-contracts>
//!
//! # Roles
//!
//! | Role | Calls |
//! |---|---|
//! | Owner (typically the `TimelockController`) | `set_*` limits and roles, `write_off`, `unpause`, `upgrade` |
//! | Guardian | `pause` |
//! | Treasury | `borrow`, `repay`, `repay_for`, `sweep_foreign_asset` |
//! | Compliance role | `allow_depositor`, `disallow_depositor`, `freeze_depositor`, `extend_exit_deadline` |
//! | Depositor | `deposit`, `mint`, `withdraw`, `redeem`, `request_redeem`, `cancel_redeem`, share transfers |
//! | Anyone | `accrue`, `bump_yield`, `process_queue`, views |
//!
//! # Lifecycle
//!
//! 1. Deposit: `deposit` or `mint` takes USDC from `from` and mints shares to `receiver`,
//!    who must authorize when they are not the operator. With a lock period set, the
//!    receiver's whole balance re-locks.
//! 2. Borrow: the treasury draws USDC while utilization stays at or below 80%.
//! 3. Accrue: debt compounds per second at 2% APR plus 7.5% per unit of utilization
//!    (8% at the 80% kink), plus 500% per unit above it (108% at full utilization),
//!    clamped to `max_borrow_rate` when set.
//! 4. Repay: the treasury returns principal and interest; `bump_yield` adds yield
//!    without minting shares. `write_off` removes defaulted debt.
//! 5. Exit: `withdraw` or `redeem` burns unlocked shares, capped per transaction and
//!    by idle liquidity. When liquidity is short, `request_redeem` queues shares and
//!    `process_queue` pays requests oldest first; while any request is open, instant
//!    exits and borrows are refused.
//!
//! Shares are priced on total managed assets (idle USDC plus outstanding debt), so
//! interest and write-offs move the share price; borrows and repayments do not.
//!
//! Pausing halts deposits, exits, borrows, repayments and queue fills; `write_off`,
//! `bump_yield` and `cancel_redeem` still run. The owner or guardian can pause; only
//! the owner can unpause.
//!
//! # Revocation
//!
//! While allowlist enforcement is on, both sides of a deposit, mint or transfer must
//! be allowlisted. `disallow_depositor` opens a withdraw grace window (default 30 days)
//! in which the lock is waived; at its deadline the shares freeze. `freeze_depositor`
//! sets the deadline to now, `extend_exit_deadline` moves it later, and
//! `allow_depositor` clears it.
//!
//! # Units
//!
//! Amounts are in the underlying asset's base units (USDC has 7 decimals). Rates and
//! utilization are WAD-scaled (1e18 = 100%). Times are ledger timestamps in seconds.
//!
//! # Authors
//!
//!
//! Samuel Mugane <smugane@shambarecords.com>
//! Peter Wesley <peter.wesley@shambarecords.com>

#![no_std]

use soroban_sdk::{
    contract, contracterror, contractevent, contractimpl, panic_with_error, Address, BytesN,
    ContractExecutable, Env, MuxedAddress, String,
};
use stellar_access::ownable::{self, Ownable};
use stellar_contract_utils::math::i128_fixed_point::{
    checked_mul_div_with_rounding, mul_div_with_rounding,
};
use stellar_contract_utils::math::wad::{Wad, WAD_SCALE};
use stellar_contract_utils::math::Rounding;
use stellar_contract_utils::pausable::{self as pausable_mod, Pausable};
use stellar_macros::{only_owner, when_not_paused};
use stellar_tokens::{
    fungible::{
        allowlist::AllowList,
        combinations::Compose,
        total_supply::{self, FungibleTotalSupply, TotalSupply},
        Base, FungibleToken, ALLOW_BLOCK_EXTEND_AMOUNT, ALLOW_BLOCK_TTL_THRESHOLD,
    },
    vault::{emit_deposit, emit_withdraw, FungibleVault, Vault, VaultTokenError},
};

/// Contract error codes. Codes 7 and 8 belonged to the retired in-contract
/// timelock and must not be reused.
#[contracterror]
#[derive(Copy, Clone, Debug, Eq, PartialEq, PartialOrd, Ord)]
#[repr(u32)]
pub enum MicrovaultError {
    /// The caller is not the treasury, compliance role, owner or guardian the call requires.
    Unauthorized = 1,
    /// `sweep_foreign_asset` targeted the vault's underlying asset.
    CannotSweepUnderlyingAsset = 2,
    /// An amount or limit is not positive, or there are no assets to borrow against.
    InvalidAmount = 3,
    /// The deposit, or the cost of the minted shares, exceeds the per-transaction limit.
    ExceedsMaxDeposit = 4,
    /// The withdrawal exceeds the per-transaction limit.
    ExceedsMaxWithdraw = 5,
    /// No treasury address is stored.
    TreasuryNotSet = 6,
    /// The borrow would take utilization above 80%.
    ExceedsUtilizationCap = 9,
    /// The vault holds too little idle USDC for the borrow or exit.
    InsufficientLiquidity = 10,
    /// The repayment is larger than the outstanding debt.
    RepayExceedsDebt = 11,
    /// The owner's shares are still inside the deposit lock period.
    SharesLocked = 12,
    /// The redeemed shares are worth more than the per-transaction withdrawal limit.
    ExceedsMaxRedeem = 13,
    /// An address is not on the allowlist while enforcement is on.
    AddressNotAllowed = 14,
    /// No compliance role address is stored.
    ComplianceRoleNotSet = 15,
    /// The owner's post-revocation exit deadline has passed.
    ExitWindowClosed = 16,
    /// The new exit deadline is not later than the current one, or none is set.
    InvalidExitDeadline = 17,
    /// No guardian address is stored.
    GuardianNotSet = 18,
    /// Redemption requests are waiting, so instant exits and borrows are refused.
    QueueNotEmpty = 19,
    /// The queued shares are worth less than the minimum request.
    QueueRequestTooSmall = 20,
    /// No open redemption request has that id.
    QueueEntryNotFound = 21,
    /// Interest accrual overflowed.
    MathOverflow = 22,
}

/// Emitted when the treasury address is changed.
#[contractevent]
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct TreasuryUpdated {
    /// Previous treasury.
    pub old_treasury: Address,
    /// New treasury.
    pub new_treasury: Address,
}

/// Emitted when foreign assets are recovered from the contract.
#[contractevent]
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct ForeignAssetSwept {
    /// Token contract recovered.
    pub token: Address,
    /// Address the tokens were sent to.
    pub recipient: Address,
    /// Amount in the token's base units.
    pub amount: i128,
}

/// Emitted when the maximum deposit limit is changed.
#[contractevent]
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct MaxDepositUpdated {
    /// Previous limit, in base units.
    pub old_limit: i128,
    /// New limit, in base units.
    pub new_limit: i128,
}

/// Emitted when the maximum withdrawal limit is changed.
#[contractevent]
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct MaxWithdrawUpdated {
    /// Previous limit, in base units.
    pub old_limit: i128,
    /// New limit, in base units.
    pub new_limit: i128,
}

/// Emitted when the vault is paused by the owner or guardian.
#[contractevent]
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct VaultPaused {
    /// Owner or guardian that paused.
    pub by: Address,
}

/// Emitted when the vault is unpaused by the owner.
#[contractevent]
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct VaultUnpaused {
    /// Owner that unpaused.
    pub by: Address,
}

/// Emitted when the guardian address is changed.
#[contractevent]
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct GuardianUpdated {
    /// Previous guardian.
    pub old_guardian: Address,
    /// New guardian.
    pub new_guardian: Address,
}

/// Emitted when the treasury borrows from the vault.
#[contractevent]
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct Borrowed {
    /// Treasury that borrowed and received the USDC.
    pub treasury: Address,
    /// Borrower the loan is attributed to; receives nothing on-chain.
    pub recipient: Address,
    /// Amount borrowed, in base units.
    pub amount: i128,
    /// Outstanding debt after the borrow, in base units.
    pub total_borrowed: i128,
}

/// Emitted when the treasury repays borrowed funds.
///
/// `borrower` names who the repayment was made on behalf of. It is `None` for
/// `repay` and `Some` for `repay_for`; the treasury is the payer either way.
#[contractevent]
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct Repaid {
    /// Treasury that paid.
    pub treasury: Address,
    /// Borrower the repayment is attributed to, if any.
    pub borrower: Option<Address>,
    /// Amount repaid, in base units.
    pub amount: i128,
    /// Outstanding debt after the repayment, in base units.
    pub total_borrowed: i128,
}

/// Emitted when assets are contributed to the vault without minting shares.
#[contractevent]
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct YieldBumped {
    /// Contributor.
    pub from: Address,
    /// Amount contributed, in base units.
    pub amount: i128,
    /// Total managed assets after the contribution, in base units.
    pub total_managed: i128,
}

/// Emitted when compound interest is accrued on borrowed funds.
#[contractevent]
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct InterestAccrued {
    /// Interest added to the debt, in base units.
    pub interest_amount: i128,
    /// Outstanding debt after accrual, in base units.
    pub new_total_borrowed: i128,
    /// Utilization the rate was priced at, WAD-scaled.
    pub utilization_rate: i128,
}

/// Emitted when the deposit lock period configuration is changed.
#[contractevent]
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct LockPeriodUpdated {
    /// Previous lock period, in seconds.
    pub old_period: u64,
    /// New lock period, in seconds.
    pub new_period: u64,
}

/// Emitted when a user's share unlock time is updated (on deposit/mint).
#[contractevent]
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct UserLockUpdated {
    /// Share holder.
    pub user: Address,
    /// Ledger timestamp at which the shares unlock.
    pub unlock_time: u64,
}

/// Emitted when the compliance role address is changed.
///
/// `old_role` is `None` on the first assignment, which is the normal case for a
/// vault upgraded from a WASM that predates the allowlist.
#[contractevent]
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct ComplianceRoleUpdated {
    /// Previous compliance role, if any.
    pub old_role: Option<Address>,
    /// New compliance role.
    pub new_role: Address,
}

/// Emitted when allowlist enforcement is switched on or off.
#[contractevent]
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct AllowlistEnforcementUpdated {
    /// Previous setting.
    pub old_enforced: bool,
    /// New setting.
    pub new_enforced: bool,
}

/// Emitted when the post-revocation withdraw grace period is changed.
#[contractevent]
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct WithdrawGracePeriodUpdated {
    /// Previous grace period, in seconds.
    pub old_period: u64,
    /// New grace period, in seconds.
    pub new_period: u64,
}

/// Emitted when a depositor's exit deadline is set or moved. `frozen` is true
/// when the deadline already binds, as after `freeze_depositor`.
#[contractevent]
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct ExitDeadlineSet {
    #[topic]
    /// Revoked or frozen share holder.
    pub account: Address,
    /// Ledger timestamp from which exits are refused.
    pub deadline: u64,
    /// Whether the deadline has already passed.
    pub frozen: bool,
}

/// Emitted when a depositor's exit deadline is removed by re-allowing them.
#[contractevent]
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct ExitDeadlineCleared {
    #[topic]
    /// Re-allowed share holder.
    pub account: Address,
}

/// Emitted when the borrow rate ceiling is changed.
#[contractevent]
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct MaxBorrowRateUpdated {
    /// Previous ceiling, WAD-scaled; `None` when uncapped.
    pub old_rate: Option<i128>,
    /// New ceiling, WAD-scaled.
    pub new_rate: i128,
}

/// Emitted when the minimum redemption request is changed.
#[contractevent]
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct MinQueueAssetsUpdated {
    /// Previous minimum, in base units.
    pub old_min: i128,
    /// New minimum, in base units.
    pub new_min: i128,
}

/// Emitted when defaulted debt is written off.
#[contractevent]
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct WrittenOff {
    #[topic]
    /// Borrower the debt is attributed to.
    pub borrower: Address,
    /// Principal written off, in base units at `origin_index`.
    pub principal: i128,
    /// Borrow index when the principal was drawn, WAD-scaled.
    pub origin_index: i128,
    /// Debt removed, in base units.
    pub amount: i128,
    /// Outstanding debt after the write-off, in base units.
    pub total_borrowed: i128,
}

/// Emitted when shares are queued for redemption.
#[contractevent]
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct RedeemQueued {
    #[topic]
    /// Share owner.
    pub owner: Address,
    /// Request id.
    pub id: u64,
    /// Address that will receive the USDC.
    pub receiver: Address,
    /// Shares escrowed.
    pub shares: i128,
}

/// Emitted when a queued request is filled in whole or in part.
#[contractevent]
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct RedeemFilled {
    #[topic]
    /// Share owner.
    pub owner: Address,
    /// Request id.
    pub id: u64,
    /// Address paid.
    pub receiver: Address,
    /// Shares burned.
    pub shares: i128,
    /// USDC paid, in base units.
    pub assets: i128,
    /// Shares still queued under this id.
    pub remaining: i128,
}

/// Emitted when an owner cancels a queued request.
#[contractevent]
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct RedeemCancelled {
    #[topic]
    /// Share owner.
    pub owner: Address,
    /// Request id.
    pub id: u64,
    /// Shares returned.
    pub shares: i128,
}

/// Emitted when a frozen owner's queued shares are returned unpaid.
#[contractevent]
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct RedeemReturned {
    #[topic]
    /// Share owner.
    pub owner: Address,
    /// Request id.
    pub id: u64,
    /// Shares returned.
    pub shares: i128,
}

/// A queued redemption.
#[soroban_sdk::contracttype]
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct RedeemRequest {
    /// Share owner.
    pub owner: Address,
    /// Address that will receive the USDC.
    pub receiver: Address,
    /// Shares still escrowed.
    pub shares: i128,
    /// Ledger timestamp of the request.
    pub requested_at: u64,
}

/// Storage keys. Variant order is the on-chain encoding; append only.
#[soroban_sdk::contracttype]
pub enum DataKey {
    /// Instance: treasury address.
    Treasury,
    /// Instance: guardian address.
    Guardian,
    /// Instance: per-transaction deposit limit.
    MaxDeposit,
    /// Instance: per-transaction withdrawal limit.
    MaxWithdraw,
    /// Instance: outstanding debt including accrued interest.
    TotalBorrowed,
    /// Instance: ledger timestamp of the last accrual.
    LastAccrualTime,
    /// Instance: deposit lock period in seconds.
    LockPeriod,
    /// Persistent: a holder's unlock timestamp.
    UserUnlockTime(Address),
    /// Instance: cumulative borrow index, WAD-scaled.
    BorrowIndex,
    /// Instance: compliance role address.
    ComplianceRole,
    /// Instance: whether allowlist enforcement is on.
    AllowlistEnforced,
    /// Instance: post-revocation withdraw grace period in seconds.
    WithdrawGracePeriod,
    /// Persistent: a revoked holder's exit deadline.
    ExitDeadline(Address),
    /// Instance: borrow rate ceiling, WAD-scaled; absent means uncapped.
    MaxBorrowRate,
    /// Instance: id of the oldest possibly open redemption request.
    QueueHead,
    /// Instance: next redemption request id.
    QueueTail,
    /// Instance: shares held in escrow for queued requests.
    QueuedShares,
    /// Persistent: an open redemption request.
    QueueEntry(u64),
    /// Instance: smallest redemption request, in base units.
    MinQueueAssets,
}

/// Where a share owner stands on exiting the vault after revocation.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
enum ExitState {
    Normal,
    Grace,
    Frozen,
}

/// Default maximum deposit limit (1M USDC with 7 decimals).
const DEFAULT_MAX_DEPOSIT: i128 = 10_000_000_000_000;

/// Default maximum withdrawal limit (1M USDC with 7 decimals).
const DEFAULT_MAX_WITHDRAW: i128 = 10_000_000_000_000;

/// Decimals offset for share-inflation attack protection.
const DECIMALS_OFFSET: u32 = 6;

/// Default lock period — 0 disables locking.
const DEFAULT_LOCK_PERIOD: u64 = 0;

/// Default window a revoked depositor has to withdraw: 30 days.
const DEFAULT_WITHDRAW_GRACE_PERIOD: u64 = 30 * 24 * 60 * 60;

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

/// Default smallest redemption request: 10 USDC.
const DEFAULT_MIN_QUEUE_ASSETS: i128 = 100_000_000;

/// Seconds per year, used for APR-to-per-second rate conversion.
const SECONDS_PER_YEAR: u64 = 31_536_000;

/// Microvault SEP-56 tokenized vault contract.
#[contract]
pub struct MicrovaultContract;

#[contractimpl]
impl MicrovaultContract {
    /// Initialize the vault with the underlying asset and initial configuration.
    ///
    /// # Arguments
    ///
    /// * `owner` - Contract owner (typically the TimelockController address).
    /// * `guardian` - Guardian address that can pause the vault immediately.
    /// * `asset` - Underlying asset address (e.g. USDC).
    /// * `treasury` - Treasury wallet for credit delegation operations.
    /// * `name` - Share token name (e.g. "Microvault USDC Shares").
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
    ///
    /// # Errors
    ///
    /// `TreasuryNotSet`.
    pub fn treasury(e: &Env) -> Result<Address, MicrovaultError> {
        e.storage()
            .instance()
            .get(&DataKey::Treasury)
            .ok_or(MicrovaultError::TreasuryNotSet)
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

    /// Returns the borrow rate ceiling (WAD-scaled), if one is set.
    pub fn max_borrow_rate(e: &Env) -> Option<i128> {
        e.storage().instance().get(&DataKey::MaxBorrowRate)
    }

    /// Dual-slope rate, clamped to `max_borrow_rate` when set.
    fn calculate_borrow_rate(e: &Env, utilization: i128) -> i128 {
        let rate = Self::curve_rate(e, utilization);
        match Self::max_borrow_rate(e) {
            Some(ceiling) => rate.min(ceiling),
            None => rate,
        }
    }

    /// Dual-slope interest rate model.
    ///
    /// Below optimal utilization: `base_rate + utilization * slope1`.
    /// Above optimal: adds `(utilization - optimal) * slope2` on top.
    fn curve_rate(e: &Env, utilization: i128) -> i128 {
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
        if new_limit <= 0 {
            panic_with_error!(e, MicrovaultError::InvalidAmount)
        }
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
        if new_limit <= 0 {
            panic_with_error!(e, MicrovaultError::InvalidAmount)
        }
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

    /// Cap the borrow rate at `new_rate` (WAD-scaled APR). Owner only (timelocked).
    #[only_owner]
    pub fn set_max_borrow_rate(e: &Env, new_rate: i128) {
        if new_rate <= 0 {
            panic_with_error!(e, MicrovaultError::InvalidAmount)
        }
        Self::accrue_interest(e);
        let old_rate = Self::max_borrow_rate(e);
        e.storage()
            .instance()
            .set(&DataKey::MaxBorrowRate, &new_rate);
        MaxBorrowRateUpdated { old_rate, new_rate }.publish(e);
    }

    /// Set the smallest redemption request, in base units. Owner only (timelocked).
    #[only_owner]
    pub fn set_min_queue_assets(e: &Env, new_min: i128) {
        if new_min <= 0 {
            panic_with_error!(e, MicrovaultError::InvalidAmount)
        }
        let old_min = Self::min_queue_assets(e);
        e.storage()
            .instance()
            .set(&DataKey::MinQueueAssets, &new_min);
        MinQueueAssetsUpdated { old_min, new_min }.publish(e);
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
            .unwrap_or_else(|| panic_with_error!(e, MicrovaultError::GuardianNotSet));
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
        e.deployer()
            .update_current_contract(ContractExecutable::Wasm(new_wasm_hash));
    }

    /// Move the share supply from the OZ 0.7.x instance entry to its
    /// persistent entry. Owner only.
    ///
    /// Run once, immediately after upgrading from a 0.7.x-built wasm. Returns
    /// the migrated supply, or `0` once nothing is left to migrate.
    #[only_owner]
    pub fn migrate_total_supply(e: &Env) -> i128 {
        total_supply::migrate_total_supply(e)
    }

    // ─────────────────────────────────────────────────────────────────────
    // Lock Management (Internal)
    // ─────────────────────────────────────────────────────────────────────

    /// Reset the user's lock to `now + lock_period` on every deposit.
    ///
    /// Any deposit made while a lock period is configured re-locks the
    /// user's full balance for the entire `lock_period`. This deliberately
    /// replaces the previous weighted-average scheme, which truncated to zero
    /// for small deposits on large balances (audit MV-04) and allowed new
    /// deposits to inherit an unlocked state.
    ///
    /// The new unlock time is `max(current_unlock, now + lock_period)`, so an
    /// earlier deposit with a longer remaining lock is never shortened by a
    /// later one.
    fn update_lock_time(e: &Env, user: &Address, _existing_shares: i128, _new_shares: i128) {
        let lock_period = Self::get_lock_period(e);
        if lock_period == 0 {
            return;
        }

        let current_time = e.ledger().timestamp();
        let proposed_unlock = current_time + lock_period;
        let current_unlock: u64 = e
            .storage()
            .persistent()
            .get(&DataKey::UserUnlockTime(user.clone()))
            .unwrap_or(0);
        let new_unlock = current_unlock.max(proposed_unlock);

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
    /// When the vault owner is a `TimelockController`, this function is
    /// invoked via `execute_op` after a time delay, providing on-chain
    /// transparency before the change takes effect.
    #[only_owner]
    pub fn set_treasury(e: &Env, new_treasury: Address) {
        let old_treasury: Address = e
            .storage()
            .instance()
            .get(&DataKey::Treasury)
            .unwrap_or_else(|| panic_with_error!(e, MicrovaultError::TreasuryNotSet));
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
    // Compliance Allowlist
    // ─────────────────────────────────────────────────────────────────────

    /// Returns the compliance role address, if set.
    pub fn compliance_role(e: &Env) -> Option<Address> {
        e.storage().instance().get(&DataKey::ComplianceRole)
    }

    /// Returns `true` if allowlist enforcement is switched on.
    ///
    /// Defaults to `false` so that an upgrade from a pre-allowlist WASM does not
    /// lock out existing depositors before they have been backfilled onto the
    /// list. Switch on with `set_allowlist_enforced` once the backfill
    /// is complete.
    pub fn allowlist_enforced(e: &Env) -> bool {
        e.storage()
            .instance()
            .get(&DataKey::AllowlistEnforced)
            .unwrap_or(false)
    }

    /// Returns `true` if `address` is on the allowlist.
    ///
    /// Independent of `allowlist_enforced` — this reports list
    /// membership, not whether membership is currently being enforced.
    pub fn is_allowed(e: &Env, address: Address) -> bool {
        AllowList::allowed(e, &address)
    }

    /// Add `address` to the allowlist. Compliance role only.
    ///
    /// Deliberately not `#[only_owner]`. Like the guardian's `pause`, this is a
    /// fast path that takes effect the moment the compliance key signs, with no
    /// timelock delay — a sanctions hit cannot wait days.
    ///
    /// # Errors
    ///
    /// `Unauthorized`, `ComplianceRoleNotSet`.
    pub fn allow_depositor(
        e: &Env,
        caller: Address,
        address: Address,
    ) -> Result<(), MicrovaultError> {
        Self::require_compliance_role(e, &caller)?;
        AllowList::allow_user(e, &address);
        let key = DataKey::ExitDeadline(address.clone());
        if e.storage().persistent().has(&key) {
            e.storage().persistent().remove(&key);
            ExitDeadlineCleared { account: address }.publish(e);
        }
        Ok(())
    }

    /// Remove `address` from the allowlist. Compliance role only.
    ///
    /// Blocks further deposits, mints and share transfers involving `address`,
    /// and starts its exit window: `withdraw` and `redeem` keep working for
    /// `withdraw_grace_period`, then freeze. An existing deadline is
    /// left as is, so repeating the call cannot extend the window.
    ///
    /// # Errors
    ///
    /// `Unauthorized`, `ComplianceRoleNotSet`.
    pub fn disallow_depositor(
        e: &Env,
        caller: Address,
        address: Address,
    ) -> Result<(), MicrovaultError> {
        Self::require_compliance_role(e, &caller)?;
        AllowList::disallow_user(e, &address);
        if Self::exit_deadline(e, address.clone()).is_none() {
            let deadline = e
                .ledger()
                .timestamp()
                .saturating_add(Self::withdraw_grace_period(e));
            Self::write_exit_deadline(e, &address, deadline);
        }
        Ok(())
    }

    /// Disallow `address` and freeze its shares now, with no grace window.
    /// Compliance role only, not timelocked.
    ///
    /// # Errors
    ///
    /// `Unauthorized`, `ComplianceRoleNotSet`.
    pub fn freeze_depositor(
        e: &Env,
        caller: Address,
        address: Address,
    ) -> Result<(), MicrovaultError> {
        Self::require_compliance_role(e, &caller)?;
        AllowList::disallow_user(e, &address);
        Self::write_exit_deadline(e, &address, e.ledger().timestamp());
        Ok(())
    }

    /// Move an existing exit deadline later. Compliance role only.
    ///
    /// Covers grace lost to a pause or to thin liquidity, and lifts a freeze
    /// without re-allowing the address.
    ///
    /// # Errors
    ///
    /// `Unauthorized`, `ComplianceRoleNotSet`, `InvalidExitDeadline`.
    pub fn extend_exit_deadline(
        e: &Env,
        caller: Address,
        address: Address,
        deadline: u64,
    ) -> Result<(), MicrovaultError> {
        Self::require_compliance_role(e, &caller)?;
        match Self::exit_deadline(e, address.clone()) {
            Some(current) if deadline > current => {
                Self::write_exit_deadline(e, &address, deadline);
                Ok(())
            }
            _ => Err(MicrovaultError::InvalidExitDeadline),
        }
    }

    /// Returns the post-revocation withdraw window in seconds.
    pub fn withdraw_grace_period(e: &Env) -> u64 {
        e.storage()
            .instance()
            .get(&DataKey::WithdrawGracePeriod)
            .unwrap_or(DEFAULT_WITHDRAW_GRACE_PERIOD)
    }

    /// Returns the ledger timestamp from which `address` can no longer
    /// withdraw, if it has been revoked or frozen.
    pub fn exit_deadline(e: &Env, address: Address) -> Option<u64> {
        let key = DataKey::ExitDeadline(address);
        let deadline = e.storage().persistent().get(&key);
        if deadline.is_some() {
            e.storage().persistent().extend_ttl(
                &key,
                ALLOW_BLOCK_TTL_THRESHOLD,
                ALLOW_BLOCK_EXTEND_AMOUNT,
            );
        }
        deadline
    }

    /// Returns `true` if `address`'s shares are frozen right now.
    pub fn is_frozen(e: &Env, address: Address) -> bool {
        Self::exit_state(e, &address) == ExitState::Frozen
    }

    /// Set the post-revocation withdraw window in seconds. Owner only
    /// (timelocked). 0 makes every revocation freeze immediately. Deadlines
    /// already set are not changed.
    #[only_owner]
    pub fn set_withdraw_grace_period(e: &Env, new_period: u64) {
        let old_period = Self::withdraw_grace_period(e);
        e.storage()
            .instance()
            .set(&DataKey::WithdrawGracePeriod, &new_period);
        WithdrawGracePeriodUpdated {
            old_period,
            new_period,
        }
        .publish(e);
    }

    fn write_exit_deadline(e: &Env, address: &Address, deadline: u64) {
        let key = DataKey::ExitDeadline(address.clone());
        e.storage().persistent().set(&key, &deadline);
        e.storage().persistent().extend_ttl(
            &key,
            ALLOW_BLOCK_TTL_THRESHOLD,
            ALLOW_BLOCK_EXTEND_AMOUNT,
        );
        ExitDeadlineSet {
            account: address.clone(),
            deadline,
            frozen: deadline <= e.ledger().timestamp(),
        }
        .publish(e);
    }

    /// Deadlines bind only while enforcement is on and the owner is off the
    /// allowlist, matching the deposit and transfer gates.
    fn exit_state(e: &Env, owner: &Address) -> ExitState {
        if Self::address_allowed(e, owner) {
            return ExitState::Normal;
        }
        match Self::exit_deadline(e, owner.clone()) {
            None => ExitState::Normal,
            Some(deadline) if e.ledger().timestamp() < deadline => ExitState::Grace,
            Some(_) => ExitState::Frozen,
        }
    }

    /// Panic if `owner` is frozen. Returns whether the deposit lock applies:
    /// it is skipped during grace so the window is always usable.
    fn check_exit(e: &Env, owner: &Address) -> bool {
        match Self::exit_state(e, owner) {
            ExitState::Frozen => panic_with_error!(e, MicrovaultError::ExitWindowClosed),
            ExitState::Grace => false,
            ExitState::Normal => true,
        }
    }

    /// Non-panicking form of the `check_exit` + lock gate on withdraw/redeem,
    /// for the `max_*` views.
    fn exit_blocked(e: &Env, owner: &Address) -> bool {
        match Self::exit_state(e, owner) {
            ExitState::Frozen => true,
            ExitState::Grace => false,
            ExitState::Normal => Self::is_locked(e, owner.clone()),
        }
    }

    /// Set the compliance role address. Owner only (timelocked).
    ///
    /// Rotating the compliance key is a rare, deliberate action, so unlike the
    /// allowlist mutations themselves it goes through the owner's timelock.
    #[only_owner]
    pub fn set_compliance_role(e: &Env, new_role: Address) {
        let old_role: Option<Address> = e.storage().instance().get(&DataKey::ComplianceRole);
        e.storage()
            .instance()
            .set(&DataKey::ComplianceRole, &new_role);
        ComplianceRoleUpdated { old_role, new_role }.publish(e);
    }

    /// Switch allowlist enforcement on or off. Owner only (timelocked).
    ///
    /// Sequencing this after the depositor backfill is what keeps an upgrade
    /// from locking out addresses that already hold shares.
    #[only_owner]
    pub fn set_allowlist_enforced(e: &Env, enforced: bool) {
        let old_enforced = Self::allowlist_enforced(e);
        e.storage()
            .instance()
            .set(&DataKey::AllowlistEnforced, &enforced);
        AllowlistEnforcementUpdated {
            old_enforced,
            new_enforced: enforced,
        }
        .publish(e);
    }

    /// Require `caller` to be the configured compliance role, with its auth.
    fn require_compliance_role(e: &Env, caller: &Address) -> Result<(), MicrovaultError> {
        caller.require_auth();

        let role: Address = e
            .storage()
            .instance()
            .get(&DataKey::ComplianceRole)
            .ok_or(MicrovaultError::ComplianceRoleNotSet)?;
        if *caller != role {
            return Err(MicrovaultError::Unauthorized);
        }
        Ok(())
    }

    /// Whether `address` passes the allowlist gate; always true while enforcement is off.
    fn address_allowed(e: &Env, address: &Address) -> bool {
        !Self::allowlist_enforced(e) || AllowList::allowed(e, address)
    }

    /// Panic with [`MicrovaultError::AddressNotAllowed`] if `address` fails the allowlist gate.
    fn require_allowed(e: &Env, address: &Address) {
        if !Self::address_allowed(e, address) {
            panic_with_error!(e, MicrovaultError::AddressNotAllowed);
        }
    }

    // ─────────────────────────────────────────────────────────────────────
    // Emergency Functions
    // ─────────────────────────────────────────────────────────────────────

    /// Recover foreign tokens mistakenly sent to the vault. Treasury only.
    ///
    /// Cannot be used to sweep the underlying asset (e.g. USDC).
    ///
    /// # Errors
    ///
    /// `Unauthorized`, `TreasuryNotSet`, `CannotSweepUnderlyingAsset`, `InvalidAmount`.
    pub fn sweep_foreign_asset(
        e: &Env,
        current_treasury: Address,
        token_to_recover: Address,
        recipient: Address,
        amount: i128,
    ) -> Result<(), MicrovaultError> {
        current_treasury.require_auth();

        let treasury: Address = e
            .storage()
            .instance()
            .get(&DataKey::Treasury)
            .ok_or(MicrovaultError::TreasuryNotSet)?;
        if current_treasury != treasury {
            return Err(MicrovaultError::Unauthorized);
        }

        let underlying_asset = Vault::query_asset(e);
        if token_to_recover == underlying_asset {
            return Err(MicrovaultError::CannotSweepUnderlyingAsset);
        }
        if amount <= 0 {
            return Err(MicrovaultError::InvalidAmount);
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
    /// compounding via `Wad::powi`.
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
        let compound_factor = base.powi(e, exponent);

        let new_total_borrowed = checked_mul_div_with_rounding(
            e,
            total_borrowed,
            compound_factor.raw(),
            WAD_SCALE,
            Rounding::Floor,
        )
        .unwrap_or_else(|| panic_with_error!(e, MicrovaultError::MathOverflow));

        // Update cumulative borrow index
        let old_index: i128 = e
            .storage()
            .instance()
            .get(&DataKey::BorrowIndex)
            .unwrap_or(WAD_SCALE);
        let old_index_wad = Wad::from_raw(old_index);
        let new_index = old_index_wad
            .checked_mul(e, compound_factor)
            .unwrap_or_else(|| panic_with_error!(e, MicrovaultError::MathOverflow));
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

    /// Lend `amount` to the treasury. Treasury only.
    ///
    /// Accrues interest before processing. Enforces the 80% utilization cap
    /// and checks available liquidity. The USDC goes to the treasury;
    /// `recipient` is recorded on the `Borrowed` event only.
    ///
    /// # Errors
    ///
    /// `Unauthorized`, `TreasuryNotSet`, `InvalidAmount`, `ExceedsUtilizationCap`,
    /// `InsufficientLiquidity`.
    #[when_not_paused]
    pub fn borrow(
        e: &Env,
        treasury_caller: Address,
        recipient: Address,
        amount: i128,
    ) -> Result<(), MicrovaultError> {
        treasury_caller.require_auth();

        let treasury: Address = e
            .storage()
            .instance()
            .get(&DataKey::Treasury)
            .ok_or(MicrovaultError::TreasuryNotSet)?;
        if treasury_caller != treasury {
            return Err(MicrovaultError::Unauthorized);
        }
        if amount <= 0 {
            return Err(MicrovaultError::InvalidAmount);
        }
        if Self::queued_shares(e) > 0 {
            return Err(MicrovaultError::QueueNotEmpty);
        }

        Self::accrue_interest(e);

        let current_borrowed = Self::total_borrowed(e);
        let total_managed = Self::raw_total_managed_assets(e);
        let new_borrowed = current_borrowed + amount;

        let new_utilization = if total_managed > 0 {
            Wad::from_ratio(e, new_borrowed, total_managed).raw()
        } else {
            return Err(MicrovaultError::InvalidAmount);
        };
        if new_utilization > UTILIZATION_CAP {
            return Err(MicrovaultError::ExceedsUtilizationCap);
        }

        let available = Self::available_liquidity(e);
        if amount > available {
            return Err(MicrovaultError::InsufficientLiquidity);
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
    /// Accrues interest before processing.
    ///
    /// # Errors
    ///
    /// `Unauthorized`, `TreasuryNotSet`, `InvalidAmount`, `RepayExceedsDebt`.
    #[when_not_paused]
    pub fn repay(e: &Env, treasury_caller: Address, amount: i128) -> Result<(), MicrovaultError> {
        Self::repay_inner(e, treasury_caller, None, amount)
    }

    /// Repay borrowed funds on behalf of a named borrower. Treasury only.
    ///
    /// Behaves exactly as `repay`; the only difference is that `borrower` is
    /// carried onto the `Repaid` event, giving the repayment the same on-chain
    /// attribution `borrow` already gives disbursement. `borrower` does not
    /// authorize anything — the treasury remains the payer.
    ///
    /// # Errors
    ///
    /// `Unauthorized`, `TreasuryNotSet`, `InvalidAmount`, `RepayExceedsDebt`.
    #[when_not_paused]
    pub fn repay_for(
        e: &Env,
        treasury_caller: Address,
        borrower: Address,
        amount: i128,
    ) -> Result<(), MicrovaultError> {
        Self::repay_inner(e, treasury_caller, Some(borrower), amount)
    }

    /// Contribute assets to the vault without minting shares.
    ///
    /// Raises `total_managed_assets`, and therefore the value of every existing
    /// share, without changing `total_borrowed` or the share supply. Used to
    /// return interest earned on loans the vault stopped counting as borrowed
    /// at disbursement, which would otherwise accrue only to the treasury.
    ///
    /// A plain transfer to the contract has the same effect on share price;
    /// this entrypoint exists so the contribution is attributable and countable
    /// rather than indistinguishable from an accident.
    ///
    /// Callable while paused, so a write-off batch can be cushioned before the
    /// vault reopens.
    ///
    /// # Errors
    ///
    /// `InvalidAmount`.
    pub fn bump_yield(e: &Env, from: Address, amount: i128) -> Result<(), MicrovaultError> {
        from.require_auth();

        if amount <= 0 {
            return Err(MicrovaultError::InvalidAmount);
        }

        Self::accrue_interest(e);

        let underlying_asset = Vault::query_asset(e);
        let token_client = soroban_sdk::token::Client::new(e, &underlying_asset);
        token_client.transfer(&from, e.current_contract_address(), &amount);

        YieldBumped {
            from,
            amount,
            total_managed: Self::raw_total_managed_assets(e),
        }
        .publish(e);

        Ok(())
    }

    /// Trigger interest accrual. Callable by anyone.
    pub fn accrue(e: &Env) {
        Self::accrue_interest(e);
    }

    /// Remove defaulted debt so lenders bear the loss. Owner only (timelocked);
    /// callable while paused.
    ///
    /// The amount is `principal × borrow_index / origin_index`, rounded up and
    /// capped at the outstanding debt, computed at execution so interest
    /// accrued during the timelock delay is written off too. Returns the
    /// amount removed.
    #[only_owner]
    pub fn write_off(e: &Env, borrower: Address, principal: i128, origin_index: i128) -> i128 {
        Self::accrue_interest(e);
        let index: i128 = e
            .storage()
            .instance()
            .get(&DataKey::BorrowIndex)
            .unwrap_or(WAD_SCALE);
        if principal <= 0 || origin_index <= 0 || origin_index > index {
            panic_with_error!(e, MicrovaultError::InvalidAmount)
        }

        let owed = mul_div_with_rounding(e, principal, index, origin_index, Rounding::Ceil);
        let current = Self::total_borrowed(e);
        let amount = owed.min(current);
        let total_borrowed = current - amount;
        e.storage()
            .instance()
            .set(&DataKey::TotalBorrowed, &total_borrowed);

        WrittenOff {
            borrower,
            principal,
            origin_index,
            amount,
            total_borrowed,
        }
        .publish(e);
        amount
    }

    // ─────────────────────────────────────────────────────────────────────
    // Share Pricing
    // ─────────────────────────────────────────────────────────────────────

    /// OZ's `convert_to_shares_with_rounding` with `total_managed_assets`
    /// (idle plus outstanding debt) as the asset side.
    fn to_shares(e: &Env, assets: i128, rounding: Rounding) -> i128 {
        if assets < 0 {
            panic_with_error!(e, VaultTokenError::VaultInvalidAssetsAmount);
        }
        if assets == 0 {
            return 0;
        }
        mul_div_with_rounding(
            e,
            assets,
            Self::virtual_supply(e),
            Self::virtual_assets(e),
            rounding,
        )
    }

    /// OZ's `convert_to_assets_with_rounding` with `total_managed_assets`
    /// (idle plus outstanding debt) as the asset side.
    fn to_assets(e: &Env, shares: i128, rounding: Rounding) -> i128 {
        if shares < 0 {
            panic_with_error!(e, VaultTokenError::VaultInvalidSharesAmount);
        }
        if shares == 0 {
            return 0;
        }
        mul_div_with_rounding(
            e,
            shares,
            Self::virtual_assets(e),
            Self::virtual_supply(e),
            rounding,
        )
    }

    fn virtual_assets(e: &Env) -> i128 {
        Self::total_managed_assets(e)
            .checked_add(1)
            .unwrap_or_else(|| panic_with_error!(e, VaultTokenError::MathOverflow))
    }

    fn virtual_supply(e: &Env) -> i128 {
        10_i128
            .checked_pow(Vault::get_decimals_offset(e))
            .and_then(|pow| total_supply::total_supply(e).checked_add(pow))
            .unwrap_or_else(|| panic_with_error!(e, VaultTokenError::MathOverflow))
    }

    // ─────────────────────────────────────────────────────────────────────
    // Withdrawal Queue
    // ─────────────────────────────────────────────────────────────────────

    /// Queue `shares` for redemption to `receiver`, first in first out.
    ///
    /// The shares move into escrow and are priced when filled, so they keep
    /// earning and keep bearing write-offs until then. While any request is
    /// open, `withdraw`, `redeem` and `borrow` are refused. Returns the request id.
    ///
    /// # Errors
    ///
    /// `InvalidAmount`, `ExitWindowClosed`, `SharesLocked`, `ExceedsMaxRedeem`,
    /// `QueueRequestTooSmall`.
    #[when_not_paused]
    pub fn request_redeem(e: &Env, owner: Address, receiver: Address, shares: i128) -> u64 {
        owner.require_auth();
        if shares <= 0 {
            panic_with_error!(e, MicrovaultError::InvalidAmount)
        }
        if Self::check_exit(e, &owner) && Self::is_locked(e, owner.clone()) {
            panic_with_error!(e, MicrovaultError::SharesLocked)
        }
        let assets = Self::to_assets(e, shares, Rounding::Floor);
        if assets > Self::get_max_withdraw(e) {
            panic_with_error!(e, MicrovaultError::ExceedsMaxRedeem)
        }
        if assets < Self::min_queue_assets(e) {
            panic_with_error!(e, MicrovaultError::QueueRequestTooSmall)
        }

        Base::update(e, Some(&owner), Some(&e.current_contract_address()), shares);
        let id = Self::queue_tail(e);
        Self::write_request(
            e,
            id,
            &RedeemRequest {
                owner: owner.clone(),
                receiver: receiver.clone(),
                shares,
                requested_at: e.ledger().timestamp(),
            },
        );
        e.storage().instance().set(&DataKey::QueueTail, &(id + 1));
        Self::set_queued_shares(e, Self::queued_shares(e) + shares);

        RedeemQueued {
            owner,
            id,
            receiver,
            shares,
        }
        .publish(e);
        id
    }

    /// Cancel request `id` and return its unfilled shares. Request owner only;
    /// callable while paused.
    ///
    /// # Errors
    ///
    /// `QueueEntryNotFound`.
    pub fn cancel_redeem(e: &Env, id: u64) {
        let request = Self::queue_entry(e, id)
            .unwrap_or_else(|| panic_with_error!(e, MicrovaultError::QueueEntryNotFound));
        request.owner.require_auth();
        Self::release_request(e, id, &request);
        RedeemCancelled {
            owner: request.owner,
            id,
            shares: request.shares,
        }
        .publish(e);
    }

    /// Fill queued requests from idle liquidity, oldest first, touching at most
    /// `max_entries` requests. Callable by anyone. The oldest request may be
    /// filled in part. A frozen owner's request is returned unpaid. Returns the
    /// number of requests touched.
    #[when_not_paused]
    pub fn process_queue(e: &Env, max_entries: u32) -> u32 {
        let vault = e.current_contract_address();
        let tail = Self::queue_tail(e);
        let mut head = Self::queue_head(e);
        let mut touched = 0;

        while touched < max_entries && head < tail {
            touched += 1;
            let Some(mut request) = Self::queue_entry(e, head) else {
                head += 1;
                continue;
            };

            if Self::exit_state(e, &request.owner) == ExitState::Frozen {
                Self::release_request(e, head, &request);
                RedeemReturned {
                    owner: request.owner,
                    id: head,
                    shares: request.shares,
                }
                .publish(e);
                head += 1;
                continue;
            }

            let liquidity_shares =
                Self::to_shares(e, Self::available_liquidity(e), Rounding::Floor);
            let shares = request.shares.min(liquidity_shares);
            if shares == 0 {
                break;
            }
            let assets = Self::to_assets(e, shares, Rounding::Floor);
            if assets == 0 && shares < request.shares {
                break;
            }

            Vault::withdraw_internal(e, &request.receiver, &vault, assets, shares, &vault);
            emit_withdraw(e, &vault, &request.receiver, &request.owner, assets, shares);
            request.shares -= shares;
            Self::set_queued_shares(e, Self::queued_shares(e) - shares);
            RedeemFilled {
                owner: request.owner.clone(),
                id: head,
                receiver: request.receiver.clone(),
                shares,
                assets,
                remaining: request.shares,
            }
            .publish(e);

            if request.shares > 0 {
                Self::write_request(e, head, &request);
                break;
            }
            e.storage().persistent().remove(&DataKey::QueueEntry(head));
            head += 1;
        }

        if Self::queued_shares(e) == 0 {
            head = tail;
        }
        e.storage().instance().set(&DataKey::QueueHead, &head);
        touched
    }

    /// Returns request `id`, if still open.
    pub fn queue_entry(e: &Env, id: u64) -> Option<RedeemRequest> {
        let key = DataKey::QueueEntry(id);
        let request = e.storage().persistent().get(&key);
        if request.is_some() {
            e.storage().persistent().extend_ttl(
                &key,
                ALLOW_BLOCK_TTL_THRESHOLD,
                ALLOW_BLOCK_EXTEND_AMOUNT,
            );
        }
        request
    }

    /// Returns the id of the oldest possibly open request.
    pub fn queue_head(e: &Env) -> u64 {
        e.storage().instance().get(&DataKey::QueueHead).unwrap_or(0)
    }

    /// Returns the number of ids from the head to the tail, cancelled holes included.
    pub fn queue_length(e: &Env) -> u64 {
        Self::queue_tail(e) - Self::queue_head(e)
    }

    /// Returns the shares held in escrow for queued requests.
    pub fn queued_shares(e: &Env) -> i128 {
        e.storage()
            .instance()
            .get(&DataKey::QueuedShares)
            .unwrap_or(0)
    }

    /// Returns the current value of all queued shares, in base units.
    pub fn queued_assets(e: &Env) -> i128 {
        Self::to_assets(e, Self::queued_shares(e), Rounding::Floor)
    }

    /// Returns the smallest redemption request, in base units.
    pub fn min_queue_assets(e: &Env) -> i128 {
        e.storage()
            .instance()
            .get(&DataKey::MinQueueAssets)
            .unwrap_or(DEFAULT_MIN_QUEUE_ASSETS)
    }

    fn queue_tail(e: &Env) -> u64 {
        e.storage().instance().get(&DataKey::QueueTail).unwrap_or(0)
    }

    fn set_queued_shares(e: &Env, shares: i128) {
        e.storage().instance().set(&DataKey::QueuedShares, &shares);
    }

    fn write_request(e: &Env, id: u64, request: &RedeemRequest) {
        let key = DataKey::QueueEntry(id);
        e.storage().persistent().set(&key, request);
        e.storage().persistent().extend_ttl(
            &key,
            ALLOW_BLOCK_TTL_THRESHOLD,
            ALLOW_BLOCK_EXTEND_AMOUNT,
        );
    }

    /// Return a request's escrowed shares to its owner and close it.
    fn release_request(e: &Env, id: u64, request: &RedeemRequest) {
        Base::update(
            e,
            Some(&e.current_contract_address()),
            Some(&request.owner),
            request.shares,
        );
        e.storage().persistent().remove(&DataKey::QueueEntry(id));
        let queued = Self::queued_shares(e) - request.shares;
        Self::set_queued_shares(e, queued);
        if queued == 0 {
            e.storage()
                .instance()
                .set(&DataKey::QueueHead, &Self::queue_tail(e));
        }
    }

    fn require_queue_empty(e: &Env) {
        if Self::queued_shares(e) > 0 {
            panic_with_error!(e, MicrovaultError::QueueNotEmpty)
        }
    }

    /// Shared body of `repay` and `repay_for`.
    fn repay_inner(
        e: &Env,
        treasury_caller: Address,
        borrower: Option<Address>,
        amount: i128,
    ) -> Result<(), MicrovaultError> {
        treasury_caller.require_auth();

        let treasury: Address = e
            .storage()
            .instance()
            .get(&DataKey::Treasury)
            .ok_or(MicrovaultError::TreasuryNotSet)?;
        if treasury_caller != treasury {
            return Err(MicrovaultError::Unauthorized);
        }
        if amount <= 0 {
            return Err(MicrovaultError::InvalidAmount);
        }

        Self::accrue_interest(e);

        let current_borrowed = Self::total_borrowed(e);
        if amount > current_borrowed {
            return Err(MicrovaultError::RepayExceedsDebt);
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
            borrower,
            amount,
            total_borrowed: new_borrowed,
        }
        .publish(e);

        Ok(())
    }
}

#[contractimpl(contracttrait)]
impl FungibleToken for MicrovaultContract {
    type ContractType = Compose<(Vault, TotalSupply)>;

    fn decimals(e: &Env) -> u32 {
        Vault::decimals(e)
    }

    /// Block transfers while the sender's shares are locked (audit MV-01).
    ///
    /// The lock is keyed on `UserUnlockTime(from)`. Without this override the
    /// default `Base::transfer` would move balances freely, letting a depositor
    /// bypass the lock by transferring shares to a clean address and redeeming
    /// there.
    ///
    /// While allowlist enforcement is on, both `from` and `to` must be allowed,
    /// raising `AddressNotAllowed` (#14) like the deposit path; the check runs
    /// before `AllowList::transfer` so its `UserNotAllowed` (#113) is never
    /// reached. The lock check stays here because `AllowList` has no concept
    /// of it.
    fn transfer(e: &Env, from: Address, to: MuxedAddress, amount: i128) {
        if MicrovaultContract::is_locked(e, from.clone()) {
            panic_with_error!(e, MicrovaultError::SharesLocked);
        }
        if MicrovaultContract::allowlist_enforced(e) {
            MicrovaultContract::require_allowed(e, &from);
            MicrovaultContract::require_allowed(e, &to.address());
            AllowList::transfer(e, &from, &to, amount);
        } else {
            Base::transfer(e, &from, &to, amount);
        }
    }

    /// Same lock and allowlist enforcement as `transfer`, applied to the
    /// spender-authorized path. The lock is on the `from` (owner) balance, and
    /// `AllowList` checks `from` and `to` — neither constrains the spender.
    fn transfer_from(e: &Env, spender: Address, from: Address, to: Address, amount: i128) {
        if MicrovaultContract::is_locked(e, from.clone()) {
            panic_with_error!(e, MicrovaultError::SharesLocked);
        }
        if MicrovaultContract::allowlist_enforced(e) {
            MicrovaultContract::require_allowed(e, &from);
            MicrovaultContract::require_allowed(e, &to);
            AllowList::transfer_from(e, &spender, &from, &to, amount);
        } else {
            Base::transfer_from(e, &spender, &from, &to, amount);
        }
    }
}

#[contractimpl(contracttrait)]
impl FungibleTotalSupply for MicrovaultContract {}

#[contractimpl]
impl FungibleVault for MicrovaultContract {
    /// Both `from` (debited) and `receiver` (credited with shares) must be
    /// allowlisted while enforcement is on. Constraining one side only would
    /// let unscreened money buy shares for a screened party, or the reverse.
    /// A `receiver` other than `operator` must also authorize, since the
    /// deposit re-locks their whole balance.
    ///
    /// # Errors
    ///
    /// `AddressNotAllowed`, `ExceedsMaxDeposit`.
    #[when_not_paused]
    fn deposit(e: &Env, assets: i128, receiver: Address, from: Address, operator: Address) -> i128 {
        MicrovaultContract::require_allowed(e, &from);
        MicrovaultContract::require_allowed(e, &receiver);

        if assets > MicrovaultContract::get_max_deposit(e) {
            panic_with_error!(e, MicrovaultError::ExceedsMaxDeposit)
        }

        operator.require_auth();
        if receiver != operator {
            receiver.require_auth();
        }

        let shares = MicrovaultContract::to_shares(e, assets, Rounding::Floor);
        if shares == 0 && assets > 0 {
            panic_with_error!(e, VaultTokenError::VaultZeroShares)
        }

        let existing_shares = Base::balance(e, &receiver);
        Vault::deposit_internal(e, &receiver, assets, shares, &from, &operator);
        emit_deposit(e, &operator, &from, &receiver, assets, shares);
        MicrovaultContract::update_lock_time(e, &receiver, existing_shares, shares);
        shares
    }

    /// Same gates as `deposit`, with the cap applied to the assets the minted
    /// shares cost.
    ///
    /// # Errors
    ///
    /// `AddressNotAllowed`, `ExceedsMaxDeposit`.
    #[when_not_paused]
    fn mint(e: &Env, shares: i128, receiver: Address, from: Address, operator: Address) -> i128 {
        MicrovaultContract::require_allowed(e, &from);
        MicrovaultContract::require_allowed(e, &receiver);

        let assets = MicrovaultContract::to_assets(e, shares, Rounding::Ceil);
        if assets > MicrovaultContract::get_max_deposit(e) {
            panic_with_error!(e, MicrovaultError::ExceedsMaxDeposit)
        }

        operator.require_auth();
        if receiver != operator {
            receiver.require_auth();
        }

        let existing_shares = Base::balance(e, &receiver);
        Vault::deposit_internal(e, &receiver, assets, shares, &from, &operator);
        emit_deposit(e, &operator, &from, &receiver, assets, shares);
        MicrovaultContract::update_lock_time(e, &receiver, existing_shares, shares);
        assets
    }

    /// Burn the owner's shares for `assets` of USDC. The lock is waived during a revocation grace window.
    ///
    /// # Errors
    ///
    /// `QueueNotEmpty`, `ExitWindowClosed`, `SharesLocked`, `ExceedsMaxWithdraw`, `InsufficientLiquidity`.
    #[when_not_paused]
    fn withdraw(
        e: &Env,
        assets: i128,
        receiver: Address,
        owner: Address,
        operator: Address,
    ) -> i128 {
        MicrovaultContract::require_queue_empty(e);
        if MicrovaultContract::check_exit(e, &owner)
            && MicrovaultContract::is_locked(e, owner.clone())
        {
            panic_with_error!(e, MicrovaultError::SharesLocked)
        }

        if assets > MicrovaultContract::get_max_withdraw(e) {
            panic_with_error!(e, MicrovaultError::ExceedsMaxWithdraw)
        }

        let available = MicrovaultContract::available_liquidity(e);
        if assets > available {
            panic_with_error!(e, MicrovaultError::InsufficientLiquidity)
        }

        operator.require_auth();
        let owner_assets =
            MicrovaultContract::to_assets(e, Base::balance(e, &owner), Rounding::Floor);
        if assets > owner_assets {
            panic_with_error!(e, VaultTokenError::VaultExceededMaxWithdraw)
        }

        let shares = MicrovaultContract::to_shares(e, assets, Rounding::Ceil);
        Vault::withdraw_internal(e, &receiver, &owner, assets, shares, &operator);
        emit_withdraw(e, &operator, &receiver, &owner, assets, shares);
        shares
    }

    /// Burn `shares` for their USDC value. The lock is waived during a revocation grace window.
    ///
    /// # Errors
    ///
    /// `QueueNotEmpty`, `ExitWindowClosed`, `SharesLocked`, `ExceedsMaxRedeem`, `InsufficientLiquidity`.
    #[when_not_paused]
    fn redeem(e: &Env, shares: i128, receiver: Address, owner: Address, operator: Address) -> i128 {
        MicrovaultContract::require_queue_empty(e);
        if MicrovaultContract::check_exit(e, &owner)
            && MicrovaultContract::is_locked(e, owner.clone())
        {
            panic_with_error!(e, MicrovaultError::SharesLocked)
        }

        let assets = MicrovaultContract::to_assets(e, shares, Rounding::Floor);

        if assets > MicrovaultContract::get_max_withdraw(e) {
            panic_with_error!(e, MicrovaultError::ExceedsMaxRedeem)
        }

        let available = MicrovaultContract::available_liquidity(e);
        if assets > available {
            panic_with_error!(e, MicrovaultError::InsufficientLiquidity)
        }

        operator.require_auth();
        if shares > Base::balance(e, &owner) {
            panic_with_error!(e, VaultTokenError::VaultExceededMaxRedeem)
        }
        if assets == 0 && shares > 0 {
            panic_with_error!(e, VaultTokenError::VaultZeroAssets)
        }

        Vault::withdraw_internal(e, &receiver, &owner, assets, shares, &operator);
        emit_withdraw(e, &operator, &receiver, &owner, assets, shares);
        assets
    }

    fn convert_to_shares(e: &Env, assets: i128) -> i128 {
        MicrovaultContract::to_shares(e, assets, Rounding::Floor)
    }

    fn convert_to_assets(e: &Env, shares: i128) -> i128 {
        MicrovaultContract::to_assets(e, shares, Rounding::Floor)
    }

    fn query_asset(e: &Env) -> Address {
        Vault::query_asset(e)
    }

    /// Returns total assets including outstanding borrows so that share
    /// holders benefit from accrued interest.
    fn total_assets(e: &Env) -> i128 {
        MicrovaultContract::total_managed_assets(e)
    }

    fn max_deposit(e: &Env, receiver: Address) -> i128 {
        if pausable_mod::paused(e) || !MicrovaultContract::address_allowed(e, &receiver) {
            return 0;
        }
        MicrovaultContract::get_max_deposit(e).min(Vault::max_deposit(e, receiver))
    }

    fn max_mint(e: &Env, receiver: Address) -> i128 {
        if pausable_mod::paused(e) || !MicrovaultContract::address_allowed(e, &receiver) {
            return 0;
        }
        MicrovaultContract::to_shares(e, MicrovaultContract::get_max_deposit(e), Rounding::Floor)
            .min(Vault::max_mint(e, receiver))
    }

    fn max_withdraw(e: &Env, owner: Address) -> i128 {
        if pausable_mod::paused(e)
            || MicrovaultContract::queued_shares(e) > 0
            || MicrovaultContract::exit_blocked(e, &owner)
        {
            return 0;
        }
        MicrovaultContract::to_assets(e, Base::balance(e, &owner), Rounding::Floor)
            .min(MicrovaultContract::get_max_withdraw(e))
            .min(MicrovaultContract::available_liquidity(e))
    }

    fn max_redeem(e: &Env, owner: Address) -> i128 {
        if pausable_mod::paused(e)
            || MicrovaultContract::queued_shares(e) > 0
            || MicrovaultContract::exit_blocked(e, &owner)
        {
            return 0;
        }
        let cap_shares = MicrovaultContract::to_shares(
            e,
            MicrovaultContract::get_max_withdraw(e),
            Rounding::Floor,
        );
        let liquidity_shares = MicrovaultContract::to_shares(
            e,
            MicrovaultContract::available_liquidity(e),
            Rounding::Floor,
        );
        Base::balance(e, &owner)
            .min(cap_shares)
            .min(liquidity_shares)
    }

    fn preview_deposit(e: &Env, assets: i128) -> i128 {
        MicrovaultContract::to_shares(e, assets, Rounding::Floor)
    }

    fn preview_mint(e: &Env, shares: i128) -> i128 {
        MicrovaultContract::to_assets(e, shares, Rounding::Ceil)
    }

    fn preview_withdraw(e: &Env, assets: i128) -> i128 {
        MicrovaultContract::to_shares(e, assets, Rounding::Ceil)
    }

    fn preview_redeem(e: &Env, shares: i128) -> i128 {
        MicrovaultContract::to_assets(e, shares, Rounding::Floor)
    }
}

#[contractimpl(contracttrait)]
impl Ownable for MicrovaultContract {}

#[contractimpl]
impl Pausable for MicrovaultContract {
    fn paused(e: &Env) -> bool {
        pausable_mod::paused(e)
    }

    /// Pause the vault. Callable by the owner **or** the guardian.
    ///
    /// This is intentionally not gated by `#[only_owner]` so that the
    /// guardian can trigger an immediate emergency pause without going
    /// through the TimelockController delay.
    ///
    /// # Errors
    ///
    /// `Unauthorized`.
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
            panic_with_error!(e, MicrovaultError::Unauthorized)
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
