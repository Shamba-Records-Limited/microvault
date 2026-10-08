#![no_std]
//! Test-only fungible token for Komet properties. Not for deployment:
//! `mint` is intentionally unauthenticated.

use soroban_sdk::{
    contract, contractimpl, contracttype, token::TokenInterface, Address, Env, MuxedAddress, String,
};

#[contracttype]
#[derive(Clone)]
pub enum DataKey {
    Balance(Address),
    Allowance(Address, Address),
}

#[contract]
pub struct TestToken;

impl TestToken {
    fn bal(e: &Env, id: &Address) -> i128 {
        e.storage()
            .persistent()
            .get(&DataKey::Balance(id.clone()))
            .unwrap_or(0)
    }

    fn set_bal(e: &Env, id: &Address, amount: i128) {
        e.storage()
            .persistent()
            .set(&DataKey::Balance(id.clone()), &amount);
    }
}

#[contractimpl]
impl TestToken {
    /// Open mint — this contract is test scaffolding only.
    pub fn mint(e: &Env, to: Address, amount: i128) {
        Self::set_bal(e, &to, Self::bal(e, &to) + amount);
    }
}

#[contractimpl(contracttrait)]
impl TokenInterface for TestToken {
    fn decimals(_e: Env) -> u32 {
        7
    }

    fn name(e: Env) -> String {
        String::from_str(&e, "Test USDC")
    }

    fn symbol(e: Env) -> String {
        String::from_str(&e, "USDC")
    }

    fn balance(e: Env, id: Address) -> i128 {
        Self::bal(&e, &id)
    }

    fn allowance(e: Env, from: Address, spender: Address) -> i128 {
        e.storage()
            .persistent()
            .get(&DataKey::Allowance(from, spender))
            .unwrap_or(0)
    }

    fn approve(e: Env, from: Address, spender: Address, amount: i128, _expiration_ledger: u32) {
        from.require_auth();
        e.storage()
            .persistent()
            .set(&DataKey::Allowance(from, spender), &amount);
    }

    fn transfer(e: Env, from: Address, to: MuxedAddress, amount: i128) {
        from.require_auth();
        let to = to.address();
        Self::set_bal(&e, &from, Self::bal(&e, &from) - amount);
        Self::set_bal(&e, &to, Self::bal(&e, &to) + amount);
    }

    fn transfer_from(e: Env, spender: Address, from: Address, to: Address, amount: i128) {
        spender.require_auth();
        let allowed: i128 = e
            .storage()
            .persistent()
            .get(&DataKey::Allowance(from.clone(), spender.clone()))
            .unwrap_or(0);
        assert!(allowed >= amount, "insufficient allowance");
        e.storage()
            .persistent()
            .set(&DataKey::Allowance(from.clone(), spender), &(allowed - amount));
        Self::set_bal(&e, &from, Self::bal(&e, &from) - amount);
        Self::set_bal(&e, &to, Self::bal(&e, &to) + amount);
    }

    fn burn(e: Env, from: Address, amount: i128) {
        from.require_auth();
        Self::set_bal(&e, &from, Self::bal(&e, &from) - amount);
    }

    fn burn_from(e: Env, spender: Address, from: Address, amount: i128) {
        spender.require_auth();
        let allowed: i128 = e
            .storage()
            .persistent()
            .get(&DataKey::Allowance(from.clone(), spender.clone()))
            .unwrap_or(0);
        assert!(allowed >= amount, "insufficient allowance");
        e.storage()
            .persistent()
            .set(&DataKey::Allowance(from.clone(), spender), &(allowed - amount));
        Self::set_bal(&e, &from, Self::bal(&e, &from) - amount);
    }
}
