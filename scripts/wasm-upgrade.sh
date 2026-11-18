#!/usr/bin/env bash
#
# Timelocked vault wasm upgrade: a single upgrade op, no pause and no migration.
# For releases that reprice shares or need a migration, use a dedicated script.
#
# Run one phase at a time; values carry between phases in $STATE.
#
#   ./scripts/wasm-upgrade.sh snapshot       # record the deployed wasm hash for rollback
#   ./scripts/wasm-upgrade.sh upload         # build + upload new wasm → H
#   ./scripts/wasm-upgrade.sh schedule       # schedule op A (upgrade to H)
#   ./scripts/wasm-upgrade.sh status         # op states and deployed wasm
#   ./scripts/wasm-upgrade.sh execute        # execute A once Ready
#   ./scripts/wasm-upgrade.sh verify         # deployed wasm == H + smoke test
#   ./scripts/wasm-upgrade.sh rollback       # schedule upgrade(OLD_HASH) → op R
#   ./scripts/wasm-upgrade.sh rollback-exec  # execute R once Ready

set -euo pipefail

NETWORK="${NETWORK:-testnet}"
TIMELOCK_ID="${TIMELOCK_ID:-CAL3RYRW6MJ2BMKP2J7G47BPBFWKZ7K2BMRG2EXZWQI5ZZAHFPM7NO7B}"
VAULT_ID="${VAULT_ID:-CDZVKARLUCAYYIV2TPSR6PLWLETPS4TYE2QXVXSQT27QNFYE3GEE5IS5}"
DEPLOYER="${DEPLOYER:-GCUUFSK3P7YMFMYVA3L7LVQ7TCWJK7WUOFMXLX2HYSRCFB37ZGZD4CCX}"
DEPLOYER_ALIAS="${DEPLOYER_ALIAS:-deployer}"
HOLDER="${HOLDER:-GCU7KVTYEPCUZOQPJ6RLNWW64FS3IBHGZTEFWERO7ZZVDQYHMG7W5B6M}"
STATE="${STATE:-$HOME/.microvault-wasm-upgrade-$NETWORK.state}"
# Delay in ledgers (~5s each). Empty = the timelock's min_delay.
DELAY="${DELAY:-}"

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SOROBAN_DIR="$SCRIPT_DIR/../soroban"
WASM="$SOROBAN_DIR/target/wasm32v1-none/release/microvault_sep56.wasm"
ZERO=0000000000000000000000000000000000000000000000000000000000000000

die() { echo "error: $*" >&2; exit 1; }
[[ "$NETWORK" =~ ^[A-Za-z0-9_-]+$ ]] || die "NETWORK must be a stellar CLI network alias like 'testnet' (got '$NETWORK')"

NEUTRAL_DIR="$(mktemp -d)"
trap 'rmdir "$NEUTRAL_DIR"' EXIT
stellar() { (cd "$NEUTRAL_DIR" && command stellar "$@"); }
unquote() { tr -d '"\n'; }
is_hash() { [[ "$1" =~ ^[0-9a-f]{64}$ ]]; }

save() { touch "$STATE"; sed -i "/^$1=/d" "$STATE"; echo "$1=$2" >> "$STATE"; printf -v "$1" '%s' "$2"; echo "$1=$2"; }
DELAY_CFG="$DELAY"
[[ -f "$STATE" ]] && source "$STATE"

retry() { local i; for i in 1 2 3 4; do "$@" && return 0; sleep 5; done; return 1; }
timelock() { stellar contract invoke --id "$TIMELOCK_ID" --source "$DEPLOYER_ALIAS" --network "$NETWORK" -- "$@"; }
tl_view()  { retry stellar contract invoke --id "$TIMELOCK_ID" --source "$DEPLOYER_ALIAS" --network "$NETWORK" --send=no -- "$@"; }
args_upgrade() { echo "[{\"bytes\":\"$1\"}]"; }
state_of() { tl_view get_operation_state --operation_id "$1" | unquote; }
need() { for v in "$@"; do [[ -n "${!v:-}" ]] || die "$v is not set (run the earlier phase or export it)"; done; }

current_wasm_hash() {
	retry stellar ledger entry fetch contract-data --contract "$VAULT_ID" --instance --network "$NETWORK" --output json |
		jq -r '.entries[0].val.contract_data.val.contract_instance.executable.wasm'
}

wait_state() {
	local i st
	for i in $(seq 1 12); do
		st=$(state_of "$1" || true)
		if [[ "${3:-}" == reach ]]; then [[ "$st" == *"$2"* ]] && return 0; else [[ -n "$st" && "$st" != *"$2"* ]] && return 0; fi
		sleep 5
	done
	return 1
}

resolve_delay() {
	local min
	min=$(tl_view get_min_delay | unquote)
	DELAY="${DELAY_CFG:-$min}"
	[[ "$DELAY" =~ ^[0-9]+$ ]] || die "DELAY must be a whole number of ledgers, got '$DELAY'"
	((DELAY >= min)) || die "DELAY $DELAY is below the timelock min_delay $min"
	echo "delay $DELAY ledgers (~$((DELAY * 5 / 60)) min; min_delay $min)"
}

# ensure_op NAME SALT_VAR hash — idempotent: the op id comes from the saved salt.
ensure_op() {
	local name=$1 saltvar=$2 args id st
	args=$(args_upgrade "$3")
	[[ -n "${!saltvar:-}" ]] || save "$saltvar" "$(openssl rand -hex 32)"
	id=$(tl_view hash_operation --target "$VAULT_ID" --function upgrade --args "$args" \
		--predecessor "$ZERO" --salt "${!saltvar}" | unquote) || die "hash_operation failed"
	is_hash "$id" || die "hash_operation returned '$id'"
	st=$(state_of "$id") || die "cannot read state of $id"
	if [[ "$st" == *Unset* ]]; then
		timelock schedule_op --target "$VAULT_ID" --function upgrade --args "$args" \
			--predecessor "$ZERO" --salt "${!saltvar}" --delay "$DELAY" --proposer "$DEPLOYER" >/dev/null ||
			echo "schedule_op did not confirm; checking chain"
		wait_state "$id" Unset || die "$name is not scheduled; re-run this phase"
	else
		echo "$name already scheduled: $st"
	fi
	save "$name" "$id"
}

# ensure_done OP_ID SALT hash — executes unless already Done.
ensure_done() {
	local id=$1 salt=$2 args st
	args=$(args_upgrade "$3")
	st=$(state_of "$id")
	[[ "$st" == *Done* ]] && { echo "op already Done on-chain"; return 0; }
	[[ "$st" == *Ready* ]] || die "op $id is $st, not Ready"
	timelock execute_op --target "$VAULT_ID" --function upgrade --args "$args" \
		--predecessor "$ZERO" --salt "$salt" --executor "\"$DEPLOYER\"" >/dev/null ||
		echo "execute_op did not confirm; checking chain"
	wait_state "$id" Done reach || die "op $id is not Done; re-run this phase"
}

case "${1:-}" in
snapshot)
	OLD=$(current_wasm_hash)
	is_hash "$OLD" || die "could not read the deployed wasm hash"
	save OLD_HASH "$OLD"
	;;
upload)
	stellar contract build --manifest-path "$SOROBAN_DIR/Cargo.toml" --package microvault-sep56
	echo "sha256 $(sha256sum "$WASM" | cut -d' ' -f1)"
	H=$(stellar contract upload --wasm "$WASM" --source-account "$DEPLOYER_ALIAS" --network "$NETWORK" | unquote)
	is_hash "$H" || die "upload returned '$H'"
	save H "$H"
	;;
schedule)
	need H OLD_HASH
	[[ "$H" != "$OLD_HASH" ]] || die "H equals the deployed wasm; nothing to upgrade"
	resolve_delay
	ensure_op A SA "$H"
	;;
status)
	for v in A R; do [[ -n "${!v:-}" ]] && echo "$v ${!v}: $(state_of "${!v}")"; done
	echo "deployed wasm: $(current_wasm_hash)"
	echo "target H:      ${H:-unset}"
	;;
execute)
	need H A SA
	ensure_done "$A" "$SA" "$H"
	[[ "$(current_wasm_hash)" == "$H" ]] || die "vault wasm is not $H after upgrade"
	echo "vault wasm: $H"
	;;
verify)
	need H
	[[ "$(current_wasm_hash)" == "$H" ]] || die "vault wasm is not $H"
	(cd "$NEUTRAL_DIR" && STELLAR_ACCOUNT="$DEPLOYER_ALIAS" CONTRACT_ID="$VAULT_ID" NETWORK="$NETWORK" HOLDER="$HOLDER" "$SCRIPT_DIR/smoke-test-vault.sh")
	echo "verify OK"
	;;
rollback)
	need OLD_HASH
	resolve_delay
	ensure_op R SR "$OLD_HASH"
	;;
rollback-exec)
	need OLD_HASH R SR
	ensure_done "$R" "$SR" "$OLD_HASH"
	echo "vault wasm: $(current_wasm_hash)"
	;;
*)
	sed -n '3,15p' "$0" | sed 's/^# \{0,1\}//'
	exit 1
	;;
esac
