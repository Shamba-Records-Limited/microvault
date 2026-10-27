#!/usr/bin/env bash
#
# Timelocked vault upgrade to the share-pricing release (fair pricing, receiver
# consent, write_off, rate ceiling, withdrawal queue): upgrade → set_max_borrow_rate
# → unpause, chained through the TimelockController with predecessors.
#
# The vault is paused BEFORE the ops are scheduled: the upgrade reprices every
# share, and a scheduled op is public, so an open vault would let anyone buy
# shares at the old idle-only price during the delay and redeem at the new one.
#
# Run one phase at a time; values carry between phases in $STATE.
#
#   ./scripts/upgrade-vault-share-pricing.sh snapshot    # pre-upgrade wasm, supply, debt, holder redeem
#   ./scripts/upgrade-vault-share-pricing.sh upload      # build + upload new wasm → H
#   ./scripts/upgrade-vault-share-pricing.sh pause       # guardian pause, before anything is scheduled
#   ./scripts/upgrade-vault-share-pricing.sh schedule    # schedule ops A (upgrade), B (set_max_borrow_rate), C (unpause)
#   ./scripts/upgrade-vault-share-pricing.sh status      # operation states
#   ./scripts/upgrade-vault-share-pricing.sh execute     # execute A then B once Ready
#   ./scripts/upgrade-vault-share-pricing.sh verify      # smoke test + repricing, ceiling and queue checks
#   ./scripts/upgrade-vault-share-pricing.sh unpause     # execute C, only if verify passed
#   ./scripts/upgrade-vault-share-pricing.sh rollback    # schedule upgrade(OLD_HASH); only while still paused
#   ./scripts/upgrade-vault-share-pricing.sh rollback-exec

set -euo pipefail

# ---- edit these -------------------------------------------------------------
NETWORK="${NETWORK:-testnet}"
TIMELOCK_ID="${TIMELOCK_ID:-CAL3RYRW6MJ2BMKP2J7G47BPBFWKZ7K2BMRG2EXZWQI5ZZAHFPM7NO7B}"
VAULT_ID="${VAULT_ID:-CDZVKARLUCAYYIV2TPSR6PLWLETPS4TYE2QXVXSQT27QNFYE3GEE5IS5}"
DEPLOYER="${DEPLOYER:-GCUUFSK3P7YMFMYVA3L7LVQ7TCWJK7WUOFMXLX2HYSRCFB37ZGZD4CCX}"
DEPLOYER_ALIAS="${DEPLOYER_ALIAS:-deployer}"
GUARDIAN_ALIAS="${GUARDIAN_ALIAS:-guardian}"
HOLDER="${HOLDER:-GCU7KVTYEPCUZOQPJ6RLNWW64FS3IBHGZTEFWERO7ZZVDQYHMG7W5B6M}"
# Borrow rate ceiling, WAD-scaled APR (1e18 = 100%). No default: it is the
# disclosed maximum and must be chosen deliberately. 36% = 360000000000000000.
MAX_BORROW_RATE="${MAX_BORROW_RATE:-}"
STATE="${STATE:-$HOME/.microvault-vault-share-pricing-$NETWORK.state}"
# Delay for every op this run schedules, in ledgers (~5s each). Empty = the timelock's min_delay.
# e.g. 120 ≈ 10 min, 720 ≈ 1 h, 17280 ≈ 24 h, 34560 ≈ 48 h. Must be >= min_delay.
DELAY="${DELAY:-}"
# -----------------------------------------------------------------------------

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SOROBAN_DIR="$SCRIPT_DIR/../soroban"
WASM="${WASM:-$SOROBAN_DIR/target/wasm32v1-none/release/microvault_sep56.wasm}"
ZERO=0000000000000000000000000000000000000000000000000000000000000000
DECIMALS_OFFSET_POW=1000000
MIN_QUEUE_ASSETS_DEFAULT=100000000

die() { echo "error: $*" >&2; exit 1; }
[[ "$NETWORK" =~ ^[A-Za-z0-9_-]+$ ]] || die "NETWORK must be a stellar CLI network alias like 'testnet', not a passphrase (got '$NETWORK'); run: unset NETWORK"
command -v bc >/dev/null || die "bc is required for 128-bit arithmetic"

# The stellar CLI loads .env from the working directory. Run it from an empty one.
NEUTRAL_DIR="$(mktemp -d)"
trap 'rmdir "$NEUTRAL_DIR"' EXIT
stellar() { (cd "$NEUTRAL_DIR" && command stellar "$@"); }
unquote() { tr -d '"\n'; }
is_hash() { [[ "$1" =~ ^[0-9a-f]{64}$ ]]; }
is_int() { [[ "$1" =~ ^[0-9]+$ ]]; }

save() { touch "$STATE"; sed -i "/^$1=/d" "$STATE"; echo "$1=$2" >> "$STATE"; printf -v "$1" '%s' "$2"; echo "$1=$2"; }
capture() { # NAME cmd... → runs cmd, stops on failure or empty output, saves NAME
	local out
	out=$("${@:2}") || die "$1: '${*:2}' failed"
	[[ -n "$out" ]] || die "$1: '${*:2}' returned nothing"
	save "$1" "$out"
}
DELAY_CFG="$DELAY"
MAX_BORROW_RATE_CFG="$MAX_BORROW_RATE"
[[ -f "$STATE" ]] && source "$STATE"
[[ -n "$MAX_BORROW_RATE_CFG" ]] && MAX_BORROW_RATE="$MAX_BORROW_RATE_CFG"

# Read-only calls are retried: testnet RPC times out often. Writes are never
# blindly retried; ensure_op / ensure_done check on-chain state instead.
retry() { local i; for i in 1 2 3 4; do "$@" && return 0; sleep 5; done; return 1; }
vault_view() { retry stellar contract invoke --id "$VAULT_ID" --source "$DEPLOYER_ALIAS" --network "$NETWORK" --send=no -- "$@"; }
timelock()   { stellar contract invoke --id "$TIMELOCK_ID" --source "$DEPLOYER_ALIAS" --network "$NETWORK" -- "$@"; }
tl_view()    { retry stellar contract invoke --id "$TIMELOCK_ID" --source "$DEPLOYER_ALIAS" --network "$NETWORK" --send=no -- "$@"; }
vv() { vault_view "$@" | unquote; }

args_upgrade() { echo "[{\"bytes\":\"$1\"}]"; }
args_rate() { echo "[{\"i128\":\"$1\"}]"; }
ARGS_UNPAUSE="[{\"address\":\"$TIMELOCK_ID\"}]"

state_of() { tl_view get_operation_state --operation_id "$1" | unquote; }
require_ready() { for op in "$@"; do [[ "$(state_of "$op")" == *Ready* ]] || die "op $op is $(state_of "$op"), not Ready"; done; }
need() { for v in "$@"; do [[ -n "${!v:-}" ]] || die "$v is not set (run the earlier phase or export it)"; done; }
require_paused() { [[ "$(vv paused)" == true ]] || die "vault is not paused; run 'pause' first"; }

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
ensure_done() {
	local id=$1 fn=$2 args=$3 pred=$4 salt=$5
	if [[ "$(state_of "$id")" == *Done* ]]; then
		echo "$fn: op already Done on-chain, not executing again" >&2
		return 0
	fi
	timelock execute_op --target "$VAULT_ID" --function "$fn" --args "$args" \
		--predecessor "$pred" --salt "$salt" --executor "\"$DEPLOYER\"" >/dev/null ||
		echo "execute_op $fn did not confirm; checking chain" >&2
	wait_state "$id" Done reach || die "$fn is not Done; re-run this phase"
}

resolve_delay() {
	local min
	min=$(tl_view get_min_delay | unquote)
	DELAY="${DELAY_CFG:-$min}"
	is_int "$DELAY" || die "DELAY must be a whole number of ledgers, got '$DELAY'"
	((DELAY >= min)) || die "DELAY $DELAY is below the timelock min_delay $min"
	save DELAY "$DELAY"
	echo "delay $DELAY ledgers (~$((DELAY * 5 / 60)) min; min_delay $min)"
}

current_wasm_hash() {
	retry stellar ledger entry fetch contract-data --contract "$VAULT_ID" --instance --network "$NETWORK" --output json |
		jq -r '.entries[0].val.contract_data.val.contract_instance.executable.wasm'
}

case "${1:-}" in
snapshot)
	need HOLDER
	capture OLD_HASH current_wasm_hash
	capture PRE_SUPPLY vv total_supply
	capture PRE_BORROWED vv total_borrowed
	capture PRE_IDLE vv available_liquidity
	capture BAL vv balance --account "$HOLDER"
	capture PRE_REDEEM vv preview_redeem --shares "$BAL"
	echo "guardian: $(vv guardian)"
	echo "old pricing values $HOLDER's $BAL shares at $PRE_REDEEM; idle $PRE_IDLE, debt $PRE_BORROWED"
	;;
upload)
	stellar contract build --manifest-path "$SOROBAN_DIR/Cargo.toml" --package microvault-sep56
	H=$(stellar contract upload --wasm "$WASM" --source-account "$DEPLOYER_ALIAS" --network "$NETWORK" | unquote)
	is_hash "$H" || die "upload returned '$H'"
	save H "$H"
	;;
pause)
	need OLD_HASH
	[[ -z "${A:-}" ]] || echo "warning: ops were already scheduled before this pause"
	GUARDIAN=$(vv guardian)
	[[ "$GUARDIAN" == "$(stellar keys address "$GUARDIAN_ALIAS")" ]] || die "on-chain guardian $GUARDIAN is not key '$GUARDIAN_ALIAS'"
	if [[ "$(vv paused)" != true ]]; then
		stellar contract invoke --id "$VAULT_ID" --source "$GUARDIAN_ALIAS" --network "$NETWORK" -- pause --caller "$GUARDIAN" >/dev/null ||
			echo "pause did not confirm; checking chain"
	fi
	sleep 5
	require_paused
	echo "paused: true"
	;;
schedule)
	need H MAX_BORROW_RATE
	is_int "$MAX_BORROW_RATE" && ((${#MAX_BORROW_RATE} <= 19)) || die "MAX_BORROW_RATE must be a WAD integer, e.g. 360000000000000000 for 36%"
	save MAX_BORROW_RATE "$MAX_BORROW_RATE"
	require_paused
	resolve_delay
	ensure_op A SA upgrade "$(args_upgrade "$H")" "$ZERO"
	ensure_op B SB set_max_borrow_rate "$(args_rate "$MAX_BORROW_RATE")" "$A"
	ensure_op C SC unpause "$ARGS_UNPAUSE" "$B"
	;;
status)
	for v in A B C R; do [[ -n "${!v:-}" ]] && echo "$v ${!v}: $(state_of "${!v}")"; done
	echo "paused: $(vv paused)"
	echo "wasm:   $(current_wasm_hash)"
	;;
execute)
	need H A SA B SB MAX_BORROW_RATE
	require_paused
	[[ "$(state_of "$A")" == *Done* ]] || require_ready "$A"
	ensure_done "$A" upgrade "$(args_upgrade "$H")" "$ZERO" "$SA"
	[[ "$(current_wasm_hash)" == "$H" ]] || die "vault wasm is not $H after upgrade"
	echo "upgraded to $H"
	[[ "$(state_of "$B")" == *Done* ]] || require_ready "$B"
	ensure_done "$B" set_max_borrow_rate "$(args_rate "$MAX_BORROW_RATE")" "$A" "$SB"
	echo "max_borrow_rate: $(vv max_borrow_rate)"
	;;
verify)
	need H HOLDER BAL PRE_SUPPLY PRE_REDEEM MAX_BORROW_RATE
	save VERIFIED ""
	(cd "$NEUTRAL_DIR" && STELLAR_ACCOUNT="$DEPLOYER_ALIAS" CONTRACT_ID="$VAULT_ID" NETWORK="$NETWORK" HOLDER="$HOLDER" "$SCRIPT_DIR/smoke-test-vault.sh")
	fail=0
	check() { if [[ "$2" == "$3" ]]; then printf '  %-22s OK  %s\n' "$1" "$2"; else printf '  %-22s FAIL got %s, want %s\n' "$1" "$2" "$3"; fail=1; fi; }
	echo "upgrade"
	check "vault wasm" "$(current_wasm_hash)" "$H"
	check "paused" "$(vv paused)" "true"
	check "total_supply" "$(vv total_supply)" "$PRE_SUPPLY"
	check "max_borrow_rate" "$(vv max_borrow_rate)" "$MAX_BORROW_RATE"
	check "queued_shares" "$(vv queued_shares)" "0"
	check "min_queue_assets" "$(vv min_queue_assets)" "$MIN_QUEUE_ASSETS_DEFAULT"
	APR=$(vv borrow_apr)
	if [[ "$(echo "$APR <= $MAX_BORROW_RATE" | bc)" == 1 ]]; then echo "  borrow_apr             OK  $APR"; else echo "  borrow_apr             FAIL $APR above the ceiling"; fail=1; fi

	echo "repricing"
	ASSETS=$(vv total_assets)
	SUPPLY=$(vv total_supply)
	REDEEM=$(vv preview_redeem --shares "$BAL")
	# OZ's formula with total managed assets: shares × (assets + 1) / (supply + 10^offset), floored.
	WANT=$(echo "$BAL * ($ASSETS + 1) / ($SUPPLY + $DECIMALS_OFFSET_POW)" | bc)
	echo "  total_assets           $ASSETS (idle $(vv available_liquidity) + debt $(vv total_borrowed))"
	echo "  holder redeem          $REDEEM (old pricing $PRE_REDEEM, expected $WANT)"
	# The two reads simulate at successive ledgers, so interest can move the
	# figure by a few stroops; allow 0.01%.
	if [[ "$(echo "d = $REDEEM - $WANT; if (d < 0) d = -d; d * 10000 <= $WANT + 10000" | bc)" == 1 ]]; then
		echo "  redeem matches total_assets pricing"
	else
		echo "  redeem does not match total_assets pricing"; fail=1
	fi
	if [[ "$(echo "$REDEEM >= $PRE_REDEEM" | bc)" != 1 ]]; then echo "  holder redeem fell below the old pricing"; fail=1; fi

	((fail == 0)) || die "verify failed — stay paused; roll back or fix forward"
	save VERIFIED "$H"
	echo "verify OK"
	;;
unpause)
	need B C SC H
	[[ "${VERIFIED:-}" == "$H" ]] || die "verify has not passed for wasm $H — run verify first; stay paused"
	[[ "$(state_of "$C")" == *Done* ]] || require_ready "$C"
	ensure_done "$C" unpause "$ARGS_UNPAUSE" "$B" "$SC"
	echo "paused: $(vv paused)"
	;;
rollback)
	need OLD_HASH
	[[ -n "${C:-}" && "$(state_of "$C")" == *Done* ]] && die "C already ran; the vault is live on the new pricing — fix forward instead"
	require_paused
	resolve_delay
	ensure_op R SR upgrade "$(args_upgrade "$OLD_HASH")" "$ZERO"
	;;
rollback-exec)
	need OLD_HASH R SR
	require_paused
	[[ "$(state_of "$R")" == *Done* ]] || require_ready "$R"
	ensure_done "$R" upgrade "$(args_upgrade "$OLD_HASH")" "$ZERO" "$SR"
	echo "vault wasm: $(current_wasm_hash)"
	;;
*)
	sed -n '3,23p' "$0" | sed 's/^# \{0,1\}//'
	exit 1
	;;
esac
