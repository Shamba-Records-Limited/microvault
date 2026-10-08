use soroban_sdk::{Address, Bytes, Env, FromVal, Val};

#[allow(dead_code)] // kasmer_set_ledger_sequence is unused by these properties
extern "C" {
    fn kasmer_create_contract(addr_val: u64, hash_val: u64) -> u64;
    fn kasmer_address_from_bytes(addr_val: u64, is_contract: u64) -> u64;
    fn kasmer_set_ledger_sequence(x: u64);
    fn kasmer_set_ledger_timestamp(x: u64);
}

/// Deploy a child contract (already uploaded by Komet) at a chosen address.
pub fn create_contract(env: &Env, addr: &Bytes, hash: &Bytes) -> Address {
    unsafe {
        let res = kasmer_create_contract(addr.as_val().get_payload(), hash.as_val().get_payload());
        Address::from_val(env, &Val::from_payload(res))
    }
}

/// Derive an account (or contract) address from raw bytes — the Komet
/// replacement for `Address::generate` (testutils is unavailable in wasm).
pub fn address_from_bytes(env: &Env, bs: &Bytes, is_contract: bool) -> Address {
    unsafe {
        let res = kasmer_address_from_bytes(
            Val::from_val(env, bs).get_payload(),
            Val::from_val(env, &is_contract).get_payload(),
        );
        Address::from_val(env, &Val::from_payload(res))
    }
}

/// Cheat: move the ledger timestamp (grace-period / deadline tests).
///
/// The cheat reads its argument off `<hostStack>`, so it must carry the
/// Soroban value encoding (a U64 `Val` payload) — not the bare integer.
pub fn set_ledger_timestamp(env: &Env, x: u64) {
    unsafe { kasmer_set_ledger_timestamp(Val::from_val(env, &x).get_payload()) }
}

/// Cheat: move the ledger sequence (TTL tests — unused by the properties below).
///
/// Like the timestamp cheat, this one inspects the raw i64 local and requires
/// it to hold a U32 `Val` payload.
#[allow(dead_code)]
pub fn set_ledger_sequence(x: u32) {
    unsafe { kasmer_set_ledger_sequence(Val::from_u32(x).to_val().get_payload()) }
}
