#!/usr/bin/env bash
#
# Timelocked vault upgrade (SDK 28 / OZ 0.8.0): upgrade → migrate_total_supply → unpause,
# chained through the TimelockController with predecessors.
#
# Run one phase at a time; values carry between phases in $STATE.
#
#   ./scripts/upgrade-vault-testnet.sh snapshot    # pre-upgrade supply, holder redeem, rollback hash
#   ./scripts/upgrade-vault-testnet.sh upload      # build + upload new wasm → H
#   NEW_GUARDIAN=G... ./scripts/upgrade-vault-testnet.sh guardian  # optional: schedule set_guardian (op G)
#   ./scripts/upgrade-vault-testnet.sh schedule    # schedule ops A, B, C
#   ./scripts/upgrade-vault-testnet.sh status      # operation states
#   ./scripts/upgrade-vault-testnet.sh guardian-exec  # execute G once Ready, before pause
#   ./scripts/upgrade-vault-testnet.sh pause       # guardian pause, only once A–C are Ready
#   ./scripts/upgrade-vault-testnet.sh execute     # execute A then B, compare with snapshot
#   ./scripts/upgrade-vault-testnet.sh verify      # smoke test + supply/redeem checks
#   ./scripts/upgrade-vault-testnet.sh unpause     # execute C, only if execute + verify passed
#   ./scripts/upgrade-vault-testnet.sh rollback    # schedule upgrade(OLD_HASH); only valid before B ran
#   ./scripts/upgrade-vault-testnet.sh rollback-exec

set -euo pipefail

# ---- edit these -------------------------------------------------------------
NETWORK="${NETWORK:-testnet}"
TIMELOCK_ID="${TIMELOCK_ID:-CAL3RYRW6MJ2BMKP2J7G47BPBFWKZ7K2BMRG2EXZWQI5ZZAHFPM7NO7B}"
VAULT_ID="${VAULT_ID:-CDZVKARLUCAYYIV2TPSR6PLWLETPS4TYE2QXVXSQT27QNFYE3GEE5IS5}"
DEPLOYER="${DEPLOYER:-GCUUFSK3P7YMFMYVA3L7LVQ7TCWJK7WUOFMXLX2HYSRCFB37ZGZD4CCX}"
DEPLOYER_ALIAS="${DEPLOYER_ALIAS:-deployer}"
GUARDIAN_ALIAS="${GUARDIAN_ALIAS:-guardian}"
HOLDER="${HOLDER:-GCU7KVTYEPCUZOQPJ6RLNWW64FS3IBHGZTEFWERO7ZZVDQYHMG7W5B6M}"
OLD_HASH="${OLD_HASH:-e61f5928b4dd28c55db255847b2b2f6930b5cc5c71d17de0e24a7f03c3ab43eb}"
STATE="${STATE:-$HOME/.microvault-vault-upgrade-$NETWORK.state}"
# Delay for every op this run schedules, in ledgers (~5s each). Empty = the timelock's min_delay.
# e.g. 120 ≈ 10 min, 720 ≈ 1 h, 17280 ≈ 24 h, 34560 ≈ 48 h. Must be >= min_delay.
DELAY="${DELAY:-120}"
# -----------------------------------------------------------------------------

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SOROBAN_DIR="$SCRIPT_DIR/../soroban"
WASM="$SOROBAN_DIR/target/wasm32v1-none/release/microvault_sep56.wasm"
ZERO=0000000000000000000000000000000000000000000000000000000000000000

die() { echo "error: $*" >&2; exit 1; }
[[ "$NETWORK" =~ ^[A-Za-z0-9_-]+$ ]] || die "NETWORK must be a stellar CLI network alias like 'testnet', not a passphrase (got '$NETWORK'); run: unset NETWORK"

# The stellar CLI loads .env from the working directory, and the repo's .env
# points it at an RPC URL without a passphrase. Run it from an empty directory.
NEUTRAL_DIR="$(mktemp -d)"
trap 'rmdir "$NEUTRAL_DIR"' EXIT
stellar() { (cd "$NEUTRAL_DIR" && command stellar "$@"); }
unquote() { tr -d '"\n'; }
is_hash() { [[ "$1" =~ ^[0-9a-f]{64}$ ]]; }

save() { touch "$STATE"; sed -i "/^$1=/d" "$STATE"; echo "$1=$2" >> "$STATE"; printf -v "$1" '%s' "$2"; echo "$1=$2"; }
capture() { # NAME cmd... → runs cmd, stops on failure or empty output, saves NAME
	local out
	out=$("${@:2}") || die "$1: '${*:2}' failed"
	[[ -n "$out" ]] || die "$1: '${*:2}' returned nothing"
	save "$1" "$out"
}
DELAY_CFG="$DELAY"
[[ -f "$STATE" ]] && source "$STATE"

# Read-only calls are retried: testnet RPC times out often. Writes are never
# blindly retried; ensure_op / ensure_done check on-chain state instead.
retry() { local i; for i in 1 2 3 4; do "$@" && return 0; sleep 5; done; return 1; }
vault_view() { retry stellar contract invoke --id "$VAULT_ID" --source "$DEPLOYER_ALIAS" --network "$NETWORK" --send=no -- "$@"; }
timelock()   { stellar contract invoke --id "$TIMELOCK_ID" --source "$DEPLOYER_ALIAS" --network "$NETWORK" -- "$@"; }
tl_view()    { retry stellar contract invoke --id "$TIMELOCK_ID" --source "$DEPLOYER_ALIAS" --network "$NETWORK" --send=no -- "$@"; }
vv() { vault_view "$@" | unquote; }

args_upgrade() { echo "[{\"bytes\":\"$1\"}]"; }
ARGS_MIGRATE='[]'
ARGS_UNPAUSE="[{\"address\":\"$TIMELOCK_ID\"}]"

# Wait until op $1 leaves state $2 (or reaches it, with $3=reach), up to ~1 min.
wait_state() {
	local i st
	for i in $(seq 1 12); do
		st=$(state_of "$1" || true)
		if [[ "${3:-}" == reach ]]; then [[ "$st" == *"$2"* ]] && return 0; else [[ -n "$st" && "$st" != *"$2"* ]] && return 0; fi
		sleep 5
	done
	return 1
}

# ensure_op NAME SALT_VAR fn args predecessor — idempotent: the op id is derived
# from the saved salt, so a re-run after a timeout finds the op instead of
# scheduling a duplicate.
ensure_op() {
	local name=$1 saltvar=$2 fn=$3 args=$4 pred=$5 id st
	[[ -n "${!saltvar:-}" ]] || capture "$saltvar" openssl rand -hex 32
	id=$(tl_view hash_operation --target "$VAULT_ID" --function "$fn" --args "$args" \
		--predecessor "$pred" --salt "${!saltvar}" | unquote) || die "hash_operation $fn failed"
	is_hash "$id" || die "hash_operation $fn returned '$id'"
	st=$(state_of "$id") || die "cannot read state of $id"
	if [[ "$st" == *Unset* ]]; then
		timelock schedule_op --target "$VAULT_ID" --function "$fn" --args "$args" \
			--predecessor "$pred" --salt "${!saltvar}" --delay "$DELAY" --proposer "$DEPLOYER" >/dev/null ||
			echo "schedule_op $fn did not confirm; checking chain"
		wait_state "$id" Unset || die "$name ($fn) is not scheduled; re-run this phase"
	else
		echo "$name ($fn) already scheduled: $st"
	fi
	save "$name" "$id"
}

# ensure_done OP_ID fn args predecessor salt — executes unless already Done.
# Prints execute_op's return value when it confirms.
ensure_done() {
	local id=$1 fn=$2 args=$3 pred=$4 salt=$5
	if [[ "$(state_of "$id")" == *Done* ]]; then
		echo "$fn: op already Done on-chain, not executing again" >&2
		return 0
	fi
	timelock execute_op --target "$VAULT_ID" --function "$fn" --args "$args" \
		--predecessor "$pred" --salt "$salt" --executor "\"$DEPLOYER\"" ||
		echo "execute_op $fn did not confirm; checking chain" >&2
	wait_state "$id" Done reach || die "$fn is not Done; re-run this phase"
}

resolve_delay() {
	local min
	min=$(tl_view get_min_delay | unquote)
	DELAY="${DELAY_CFG:-$min}"
	[[ "$DELAY" =~ ^[0-9]+$ ]] || die "DELAY must be a whole number of ledgers, got '$DELAY'"
	((DELAY >= min)) || die "DELAY $DELAY is below the timelock min_delay $min"
	save DELAY "$DELAY"
	echo "delay $DELAY ledgers (~$((DELAY * 5 / 60)) min; min_delay $min)"
}

current_wasm_hash() {
	retry stellar ledger entry fetch contract-data --contract "$VAULT_ID" --instance --network "$NETWORK" --output json |
		jq -r '.entries[0].val.contract_data.val.contract_instance.executable.wasm'
}

state_of() { tl_view get_operation_state --operation_id "$1" | unquote; }
require_ready() { for op in "$@"; do [[ "$(state_of "$op")" == *Ready* ]] || die "op $op is $(state_of "$op"), not Ready"; done; }
need() { for v in "$@"; do [[ -n "${!v:-}" ]] || die "$v is not set (run the earlier phase or export it)"; done; }

case "${1:-}" in
snapshot)
	need HOLDER
	capture CURRENT_HASH current_wasm_hash
	[[ "$OLD_HASH" == "$CURRENT_HASH" ]] || echo "warning: OLD_HASH $OLD_HASH differs from the deployed wasm; rollback will use OLD_HASH"
	save OLD_HASH "$OLD_HASH"
	capture PRE_SUPPLY vv total_supply
	capture BAL vv balance --account "$HOLDER"
	capture PRE_REDEEM vv preview_redeem --shares "$BAL"
	echo "guardian: $(vv guardian)"
	;;
upload)
	stellar contract build --manifest-path "$SOROBAN_DIR/Cargo.toml" --package microvault-sep56
	H=$(stellar contract upload --wasm "$WASM" --source-account "$DEPLOYER_ALIAS" --network "$NETWORK" | unquote)
	is_hash "$H" || die "upload returned '$H'"
	save H "$H"
	;;
guardian)
	need NEW_GUARDIAN
	[[ "$NEW_GUARDIAN" =~ ^G[A-Z2-7]{55}$ ]] || die "NEW_GUARDIAN must be a G... account address"
	save NEW_GUARDIAN "$NEW_GUARDIAN"
	echo "current guardian: $(vault_view guardian | unquote)"
	resolve_delay
	ensure_op G SG set_guardian "[{\"address\":\"$NEW_GUARDIAN\"}]" "$ZERO"
	;;
guardian-exec)
	need G SG NEW_GUARDIAN
	[[ "$(state_of "$G")" == *Done* ]] || require_ready "$G"
	ensure_done "$G" set_guardian "[{\"address\":\"$NEW_GUARDIAN\"}]" "$ZERO" "$SG" >/dev/null
	echo "guardian: $(vault_view guardian | unquote)"
	;;
schedule)
	need H
	resolve_delay
	ensure_op A SA upgrade "$(args_upgrade "$H")" "$ZERO"
	ensure_op B SB migrate_total_supply "$ARGS_MIGRATE" "$A"
	ensure_op C SC unpause "$ARGS_UNPAUSE" "$B"
	;;
status)
	for v in G A B C R; do [[ -n "${!v:-}" ]] && echo "$v ${!v}: $(state_of "${!v}")"; done
	echo "paused: $(vault_view paused | unquote)"
	;;
pause)
	need A B C
	require_ready "$A" "$B" "$C"
	GUARDIAN=$(vault_view guardian | unquote)
	[[ "$GUARDIAN" == "$(stellar keys address "$GUARDIAN_ALIAS")" ]] || die "on-chain guardian $GUARDIAN is not key '$GUARDIAN_ALIAS'; run guardian-exec first"
	if [[ "$(vv paused)" != true ]]; then
		stellar contract invoke --id "$VAULT_ID" --source "$GUARDIAN_ALIAS" --network "$NETWORK" -- pause --caller "$GUARDIAN" ||
			echo "pause did not confirm; checking chain"
	fi
	sleep 5
	[[ "$(vv paused)" == true ]] || die "vault is not paused; re-run this phase"
	echo "paused: true"
	;;
execute)
	need H A SA SB PRE_SUPPLY
	[[ "$(vault_view paused | unquote)" == true ]] || die "vault is not paused; run 'pause' first"
	ensure_done "$A" upgrade "$(args_upgrade "$H")" "$ZERO" "$SA" >/dev/null
	[[ "$(current_wasm_hash)" == "$H" ]] || die "vault wasm is not $H after upgrade"
	MIGRATED=$(ensure_done "$B" migrate_total_supply "$ARGS_MIGRATE" "$A" "$SB" | unquote)
	if [[ -z "$MIGRATED" ]]; then
		echo "warning: migrate_total_supply's return value was not captured (already done or RPC timeout); verify checks total_supply instead"
	else
		save MIGRATED "$MIGRATED"
		[[ "$MIGRATED" == "$PRE_SUPPLY" ]] || die "migrate_total_supply returned $MIGRATED, expected $PRE_SUPPLY — stay paused"
	fi
	echo "second call (simulated, recorded auth) should be 0: $(vault_view migrate_total_supply | unquote || echo 'simulation refused; skip')"
	;;
verify)
	need HOLDER BAL PRE_SUPPLY PRE_REDEEM
	save VERIFIED ""
	(cd "$NEUTRAL_DIR" && STELLAR_ACCOUNT="$DEPLOYER_ALIAS" CONTRACT_ID="$VAULT_ID" NETWORK="$NETWORK" HOLDER="$HOLDER" "$SCRIPT_DIR/smoke-test-vault.sh")
	SUPPLY=$(vault_view total_supply | unquote)
	REDEEM=$(vault_view preview_redeem --shares "$BAL" | unquote)
	echo "total_supply   $SUPPLY (pre $PRE_SUPPLY)"
	echo "preview_redeem $REDEEM (pre $PRE_REDEEM)"
	[[ "$SUPPLY" == "$PRE_SUPPLY" ]] || die "total_supply changed — stay paused, fix forward"
	# Interest accrues while the timelock waits, so redeem may only have grown, and by little.
	((REDEEM >= PRE_REDEEM && REDEEM * 100 <= PRE_REDEEM * 101)) || die "preview_redeem outside [pre, pre+1%] — stay paused, fix forward"
	save VERIFIED "$(current_wasm_hash)"
	echo "verify OK"
	;;
unpause)
	need B SC
	need C H
	[[ "${VERIFIED:-}" == "$H" ]] || die "verify has not passed for wasm $H — run verify first; stay paused"
	ensure_done "$C" unpause "$ARGS_UNPAUSE" "$B" "$SC" >/dev/null
	echo "paused: $(vault_view paused | unquote)"
	;;
rollback)
	need OLD_HASH
	[[ -n "${B:-}" && "$(state_of "$B")" == *Done* ]] && die "B already ran; a 0.7.2 rollback would read supply 0 — fix forward instead"
	resolve_delay
	ensure_op R SR upgrade "$(args_upgrade "$OLD_HASH")" "$ZERO"
	;;
rollback-exec)
	need OLD_HASH R SR
	[[ "$(state_of "$R")" == *Done* ]] || require_ready "$R"
	ensure_done "$R" upgrade "$(args_upgrade "$OLD_HASH")" "$ZERO" "$SR" >/dev/null
	echo "vault wasm: $(current_wasm_hash)"
	;;
*)
	sed -n '3,22p' "$0" | sed 's/^# \{0,1\}//'
	exit 1
	;;
esac
