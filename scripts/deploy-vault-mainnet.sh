#!/usr/bin/env bash
#
# Fresh vault + TimelockController deployment.
#
# The vault is deployed with the deployer as temporary owner so it can be
# configured without timelock delays, then ownership moves to a
# self-administered timelock (transfer_ownership + a timelocked accept_ownership).
#
# Run one phase at a time; values carry between phases in $STATE.
#
#   ./scripts/deploy-vault-mainnet.sh preflight       # network, keys, balances, USDC contract, clean tree
#   ./scripts/deploy-vault-mainnet.sh build           # build both wasms, record hashes
#   ./scripts/deploy-vault-mainnet.sh deploy-timelock # self-administered timelock
#   ./scripts/deploy-vault-mainnet.sh deploy-vault    # vault, owner = deployer (temporary)
#   ./scripts/deploy-vault-mainnet.sh configure       # compliance role, limits, lock, grace, allowlist
#   ./scripts/deploy-vault-mainnet.sh transfer        # transfer_ownership → timelock, schedule accept (op ACCEPT)
#   ./scripts/deploy-vault-mainnet.sh status          # accept op state, owner, pending
#   ./scripts/deploy-vault-mainnet.sh accept          # execute accept_ownership once Ready
#   ./scripts/deploy-vault-mainnet.sh verify          # smoke test + every governance setting
#   ./scripts/deploy-vault-mainnet.sh summary         # values for the mainnet Coolify resource

set -euo pipefail

# ---- edit these -------------------------------------------------------------
NETWORK="${NETWORK:-mainnet}"
DEPLOYER_ALIAS="${DEPLOYER_ALIAS:-deployer}"
GUARDIAN="${GUARDIAN:-}"                 # G... emergency pause key
TREASURY="${TREASURY:-}"                 # G... treasury account
COMPLIANCE_ROLE="${COMPLIANCE_ROLE:-}"   # G... signs allow/disallow/freeze
PROPOSERS="${PROPOSERS:-}"               # comma-separated G...; empty = deployer
EXECUTORS="${EXECUTORS:-}"               # comma-separated G...; empty = deployer
# Delays are in ledgers (~5s each): 17280 ≈ 24 h, 34560 ≈ 48 h.
MIN_DELAY="${MIN_DELAY:-34560}"
ACCEPT_DELAY="${ACCEPT_DELAY:-}"         # empty = MIN_DELAY
USDC_ISSUER="${USDC_ISSUER:-GA5ZSEJYB37JRC5AVCIA5MOP4RHTM335X2KGX3IHOJAPP5RE34K4KZVN}"
ASSET_ID="${ASSET_ID:-}"                 # empty = USDC:USDC_ISSUER Stellar Asset Contract
VAULT_NAME="${VAULT_NAME:-Microvault}"
VAULT_SYMBOL="${VAULT_SYMBOL:-mvUSDC}"
# Empty = keep the contract default.
MAX_DEPOSIT="${MAX_DEPOSIT:-}"           # asset units (7 decimals for USDC)
MAX_WITHDRAW="${MAX_WITHDRAW:-}"
LOCK_PERIOD="${LOCK_PERIOD:-}"           # seconds
WITHDRAW_GRACE="${WITHDRAW_GRACE:-}"     # seconds
ALLOWLIST_ENFORCED="${ALLOWLIST_ENFORCED:-true}"
STATE="${STATE:-$HOME/.microvault-vault-deploy-$NETWORK.state}"
# -----------------------------------------------------------------------------

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SOROBAN_DIR="$SCRIPT_DIR/../soroban"
RELEASE_DIR="$SOROBAN_DIR/target/wasm32v1-none/release"
VAULT_WASM="$RELEASE_DIR/microvault_sep56.wasm"
TIMELOCK_WASM="$RELEASE_DIR/microvault_timelock_controller.wasm"
ZERO=0000000000000000000000000000000000000000000000000000000000000000
MAINNET_PASSPHRASE="Public Global Stellar Network ; September 2015"

die() { echo "error: $*" >&2; exit 1; }
[[ "$NETWORK" =~ ^[A-Za-z0-9_-]+$ ]] || die "NETWORK must be a stellar CLI network alias like 'mainnet', not a passphrase (got '$NETWORK')"

# The stellar CLI loads .env from the working directory. Run it from an empty one.
NEUTRAL_DIR="$(mktemp -d)"
trap 'rmdir "$NEUTRAL_DIR"' EXIT
stellar() { (cd "$NEUTRAL_DIR" && command stellar "$@"); }
unquote() { tr -d '"\n'; }
is_hash() { [[ "$1" =~ ^[0-9a-f]{64}$ ]]; }
is_g() { [[ "$1" =~ ^G[A-Z2-7]{55}$ ]]; }
is_c() { [[ "$1" =~ ^C[A-Z2-7]{55}$ ]]; }

save() { touch "$STATE"; sed -i "/^$1=/d" "$STATE"; echo "$1=$2" >> "$STATE"; printf -v "$1" '%s' "$2"; echo "$1=$2"; }
capture() { # NAME cmd... → runs cmd, stops on failure or empty output, saves NAME
	local out
	out=$("${@:2}") || die "$1: '${*:2}' failed"
	[[ -n "$out" ]] || die "$1: '${*:2}' returned nothing"
	save "$1" "$out"
}
[[ -f "$STATE" ]] && source "$STATE"
need() { for v in "$@"; do [[ -n "${!v:-}" ]] || die "$v is not set (run the earlier phase or set it in the edit block)"; done; }

# Read-only calls are retried; writes are never blindly retried, each phase
# checks chain state first so a re-run after a timeout is safe.
retry() { local i; for i in 1 2 3 4; do "$@" && return 0; sleep 5; done; return 1; }
DEPLOYER_ADDR() { stellar keys address "$DEPLOYER_ALIAS"; }
vault_view() { retry stellar contract invoke --id "$VAULT_ID" --source "$DEPLOYER_ALIAS" --network "$NETWORK" --send=no -- "$@"; }
vault_call() { stellar contract invoke --id "$VAULT_ID" --source "$DEPLOYER_ALIAS" --network "$NETWORK" -- "$@"; }
tl_view() { retry stellar contract invoke --id "$TIMELOCK_ID" --source "$DEPLOYER_ALIAS" --network "$NETWORK" --send=no -- "$@"; }
timelock() { stellar contract invoke --id "$TIMELOCK_ID" --source "$DEPLOYER_ALIAS" --network "$NETWORK" -- "$@"; }
vv() { vault_view "$@" | unquote; }
tv() { tl_view "$@" | unquote; }

instance_exists() { retry stellar ledger entry fetch contract-data --contract "$1" --instance --network "$NETWORK" --output json 2>/dev/null | jq -e '.entries | length > 0' >/dev/null; }
wasm_of() { retry stellar ledger entry fetch contract-data --contract "$1" --instance --network "$NETWORK" --output json | jq -r '.entries[0].val.contract_data.val.contract_instance.executable.wasm'; }
latest_ledger() { retry stellar ledger latest --network "$NETWORK" --output json | jq -r .sequence; }

addr_list() { # "G1,G2" or "" → JSON array, defaulting to the deployer
	local raw=${1:-$(DEPLOYER_ADDR)} out="" a
	IFS=',' read -ra parts <<<"$raw"
	for a in "${parts[@]}"; do
		a=$(echo "$a" | tr -d ' ')
		is_g "$a" || die "'$a' is not a G... address"
		out+="${out:+,}\"$a\""
	done
	echo "[$out]"
}

confirm_writes() {
	[[ "$NETWORK" == mainnet ]] || return 0
	[[ "${YES:-}" == mainnet ]] && return 0
	local answer
	read -r -p "This sends transactions on MAINNET. Type 'mainnet' to continue: " answer
	[[ "$answer" == mainnet ]] || die "aborted"
}

state_of() { tv get_operation_state --operation_id "$1"; }
wait_state() { # op state [reach] — wait up to ~1 min
	local i st
	for i in $(seq 1 12); do
		st=$(state_of "$1" || true)
		if [[ "${3:-}" == reach ]]; then [[ "$st" == *"$2"* ]] && return 0; else [[ -n "$st" && "$st" != *"$2"* ]] && return 0; fi
		sleep 5
	done
	return 1
}

# deploy_once NAME SALT_VAR WASM ctor-args... — the contract id is derived from
# the saved salt, so a re-run after a timeout finds the contract instead of
# deploying a second one.
deploy_once() {
	local name=$1 saltvar=$2 wasm=$3 id
	shift 3
	[[ -n "${!saltvar:-}" ]] || capture "$saltvar" openssl rand -hex 32
	id=$(stellar contract id wasm --salt "${!saltvar}" --source-account "$DEPLOYER_ALIAS" --network "$NETWORK" | unquote) ||
		die "cannot derive $name contract id"
	is_c "$id" || die "derived $name id '$id' is not a contract address"
	if instance_exists "$id"; then
		echo "$name already deployed at $id"
	else
		stellar contract deploy --wasm "$wasm" --salt "${!saltvar}" --source-account "$DEPLOYER_ALIAS" \
			--network "$NETWORK" -- "$@" >/dev/null || echo "deploy $name did not confirm; checking chain"
		local i
		for i in $(seq 1 12); do instance_exists "$id" && break; sleep 5; done
		instance_exists "$id" || die "$name is not on chain at $id; re-run this phase"
	fi
	save "$name" "$id"
}

# set_if NAME getter setter value [arg-name] — calls the owner setter only if
# the on-chain value differs.
set_if() {
	local label=$1 getter=$2 setter=$3 want=$4 arg=${5:-new_limit} have
	[[ -z "$want" ]] && { echo "$label: keeping contract default ($(vv "$getter"))"; return 0; }
	have=$(vv "$getter")
	if [[ "$have" == "$want" ]]; then
		echo "$label: already $want"
		return 0
	fi
	vault_call "$setter" "--$arg" "$want" >/dev/null || echo "$setter did not confirm; checking chain"
	sleep 5
	have=$(vv "$getter")
	[[ "$have" == "$want" ]] || die "$label is $have, wanted $want; re-run configure"
	echo "$label: $want"
}

ARGS_NONE='[]'

case "${1:-}" in
preflight)
	if [[ "$NETWORK" == mainnet ]]; then
		[[ "$(stellar network ls --long 2>/dev/null | grep -A3 '^Name: mainnet' || true)" == *"$MAINNET_PASSPHRASE"* ]] ||
			echo "warning: could not confirm the 'mainnet' alias uses the public passphrase; check 'stellar network ls --long'"
	fi
	latest_ledger >/dev/null || die "cannot reach the $NETWORK RPC; add one with: stellar network add $NETWORK --rpc-url <provider url> --network-passphrase \"$MAINNET_PASSPHRASE\""
	echo "network $NETWORK reachable, ledger $(latest_ledger)"
	DEP=$(DEPLOYER_ADDR) || die "no key '$DEPLOYER_ALIAS'"
	echo "deployer $DEP"
	for v in GUARDIAN TREASURY COMPLIANCE_ROLE; do
		need "$v"
		is_g "${!v}" || die "$v '${!v}' is not a G... address"
		[[ "${!v}" != "$DEP" ]] || echo "warning: $v is the deployer key"
	done
	[[ "$GUARDIAN" != "$COMPLIANCE_ROLE" ]] || echo "warning: GUARDIAN and COMPLIANCE_ROLE are the same key"
	addr_list "$PROPOSERS" >/dev/null && addr_list "$EXECUTORS" >/dev/null
	[[ "$MIN_DELAY" =~ ^[0-9]+$ ]] || die "MIN_DELAY must be ledgers"
	if [[ -z "$ASSET_ID" ]]; then
		capture ASSET_ID stellar contract id asset --asset "USDC:$USDC_ISSUER" --network "$NETWORK"
	fi
	instance_exists "$ASSET_ID" || die "asset contract $ASSET_ID is not deployed on $NETWORK"
	echo "asset $ASSET_ID ($(retry stellar contract invoke --id "$ASSET_ID" --source "$DEPLOYER_ALIAS" --network "$NETWORK" --send=no -- symbol | unquote))"
	if [[ -n "$(git -C "$SOROBAN_DIR" status --porcelain -- . 2>/dev/null)" ]]; then
		[[ "${ALLOW_DIRTY:-}" == 1 ]] || die "soroban/ has uncommitted changes; commit first or set ALLOW_DIRTY=1"
		echo "warning: soroban/ has uncommitted changes (ALLOW_DIRTY=1)"
	fi
	save SOURCE_COMMIT "$(git -C "$SOROBAN_DIR" rev-parse HEAD)"
	echo "preflight OK"
	;;
build)
	stellar contract build --manifest-path "$SOROBAN_DIR/Cargo.toml"
	hash=$(sha256sum "$VAULT_WASM" | cut -d' ' -f1)
	save VAULT_WASM_HASH "$hash"
	hash=$(sha256sum "$TIMELOCK_WASM" | cut -d' ' -f1)
	save TIMELOCK_WASM_HASH "$hash"
	;;
deploy-timelock)
	need VAULT_WASM_HASH TIMELOCK_WASM_HASH
	[[ "$(sha256sum "$TIMELOCK_WASM" | cut -d' ' -f1)" == "$TIMELOCK_WASM_HASH" ]] || die "timelock wasm changed since build; re-run build"
	confirm_writes
	# No --admin: the constructor makes the timelock its own admin.
	deploy_once TIMELOCK_ID TIMELOCK_SALT "$TIMELOCK_WASM" \
		--min_delay "$MIN_DELAY" --proposers "$(addr_list "$PROPOSERS")" --executors "$(addr_list "$EXECUTORS")"
	echo "min_delay: $(tv get_min_delay)"
	echo "admin: $(tv get_admin)"
	;;
deploy-vault)
	need ASSET_ID VAULT_WASM_HASH TIMELOCK_ID GUARDIAN TREASURY
	[[ "$(sha256sum "$VAULT_WASM" | cut -d' ' -f1)" == "$VAULT_WASM_HASH" ]] || die "vault wasm changed since build; re-run build"
	confirm_writes
	deploy_once VAULT_ID VAULT_SALT "$VAULT_WASM" \
		--owner "$(DEPLOYER_ADDR)" --guardian "$GUARDIAN" --asset "$ASSET_ID" --treasury "$TREASURY" \
		--name "$VAULT_NAME" --symbol "$VAULT_SYMBOL"
	[[ "$(wasm_of "$VAULT_ID")" == "$VAULT_WASM_HASH" ]] || die "deployed vault wasm differs from the build"
	;;
configure)
	need VAULT_ID COMPLIANCE_ROLE
	[[ "$(vv get_owner)" == "$(DEPLOYER_ADDR)" ]] || die "deployer is no longer the vault owner; configure through the timelock instead"
	confirm_writes
	if [[ "$(vv compliance_role)" != "$COMPLIANCE_ROLE" ]]; then
		vault_call set_compliance_role --new_role "$COMPLIANCE_ROLE" >/dev/null || echo "set_compliance_role did not confirm; checking chain"
		sleep 5
	fi
	[[ "$(vv compliance_role)" == "$COMPLIANCE_ROLE" ]] || die "compliance role not set; re-run configure"
	echo "compliance_role: $COMPLIANCE_ROLE"
	set_if max_deposit get_max_deposit set_max_deposit "$MAX_DEPOSIT"
	set_if max_withdraw get_max_withdraw set_max_withdraw "$MAX_WITHDRAW"
	set_if lock_period get_lock_period set_lock_period "$LOCK_PERIOD" new_period
	set_if withdraw_grace withdraw_grace_period set_withdraw_grace_period "$WITHDRAW_GRACE" new_period
	set_if allowlist_enforced allowlist_enforced set_allowlist_enforced "$ALLOWLIST_ENFORCED" enforced
	save CONFIGURED 1
	;;
transfer)
	need VAULT_ID TIMELOCK_ID CONFIGURED
	DELAY="${ACCEPT_DELAY:-$MIN_DELAY}"
	MIN=$(tv get_min_delay)
	((DELAY >= MIN)) || die "ACCEPT_DELAY $DELAY is below min_delay $MIN"
	confirm_writes
	OWNER=$(vv get_owner)
	if [[ "$OWNER" == "$TIMELOCK_ID" ]]; then
		echo "timelock already owns the vault"
	else
		[[ "$OWNER" == "$(DEPLOYER_ADDR)" ]] || die "vault owner is $OWNER, expected the deployer"
		if [[ -z "${LIVE_UNTIL:-}" ]]; then
			# The pending transfer must outlive the accept delay; leave a week of slack.
			save LIVE_UNTIL "$(($(latest_ledger) + DELAY + 120960))"
			vault_call transfer_ownership --new_owner "$TIMELOCK_ID" --live_until_ledger "$LIVE_UNTIL" >/dev/null ||
				echo "transfer_ownership did not confirm; it is checked when accept executes"
		else
			echo "transfer_ownership already sent (live until ledger $LIVE_UNTIL)"
		fi
	fi
	[[ -n "${ACCEPT_SALT:-}" ]] || capture ACCEPT_SALT openssl rand -hex 32
	capture ACCEPT tv hash_operation --target "$VAULT_ID" --function accept_ownership --args "$ARGS_NONE" \
		--predecessor "$ZERO" --salt "$ACCEPT_SALT"
	is_hash "$ACCEPT" || die "hash_operation returned '$ACCEPT'"
	if [[ "$(state_of "$ACCEPT")" == *Unset* ]]; then
		timelock schedule_op --target "$VAULT_ID" --function accept_ownership --args "$ARGS_NONE" \
			--predecessor "$ZERO" --salt "$ACCEPT_SALT" --delay "$DELAY" --proposer "$(DEPLOYER_ADDR)" >/dev/null ||
			echo "schedule_op did not confirm; checking chain"
		wait_state "$ACCEPT" Unset || die "accept op not scheduled; re-run transfer"
	fi
	echo "accept_ownership scheduled: $(state_of "$ACCEPT") (delay $DELAY ledgers, ~$((DELAY * 5 / 3600)) h)"
	;;
status)
	[[ -n "${ACCEPT:-}" ]] && echo "ACCEPT $ACCEPT: $(state_of "$ACCEPT")"
	[[ -n "${VAULT_ID:-}" ]] && echo "vault owner: $(vv get_owner)  paused: $(vv paused)"
	[[ -n "${LIVE_UNTIL:-}" ]] && echo "pending transfer valid until ledger $LIVE_UNTIL (now $(latest_ledger))"
	true
	;;
accept)
	need VAULT_ID TIMELOCK_ID ACCEPT ACCEPT_SALT LIVE_UNTIL
	if [[ "$(vv get_owner)" != "$TIMELOCK_ID" ]]; then
		[[ "$(state_of "$ACCEPT")" == *Ready* ]] || die "accept op is $(state_of "$ACCEPT"), not Ready"
		(($(latest_ledger) < LIVE_UNTIL)) || die "pending transfer expired at ledger $LIVE_UNTIL; clear LIVE_UNTIL/ACCEPT* from $STATE and re-run transfer"
		confirm_writes
		timelock execute_op --target "$VAULT_ID" --function accept_ownership --args "$ARGS_NONE" \
			--predecessor "$ZERO" --salt "$ACCEPT_SALT" --executor "\"$(DEPLOYER_ADDR)\"" >/dev/null ||
			echo "execute_op did not confirm; checking chain"
		wait_state "$ACCEPT" Done reach || die "accept op is not Done; re-run accept"
	fi
	[[ "$(vv get_owner)" == "$TIMELOCK_ID" ]] || die "vault owner is $(vv get_owner), not the timelock"
	echo "vault owner: $TIMELOCK_ID (timelock)"
	;;
verify)
	need VAULT_ID TIMELOCK_ID ASSET_ID GUARDIAN TREASURY COMPLIANCE_ROLE VAULT_WASM_HASH TIMELOCK_WASM_HASH
	(cd "$NEUTRAL_DIR" && STELLAR_ACCOUNT="$DEPLOYER_ALIAS" CONTRACT_ID="$VAULT_ID" NETWORK="$NETWORK" "$SCRIPT_DIR/smoke-test-vault.sh")
	fail=0
	check() { if [[ "$2" == "$3" ]]; then printf '  %-22s OK  %s\n' "$1" "$2"; else printf '  %-22s FAIL got %s, want %s\n' "$1" "$2" "$3"; fail=1; fi; }
	echo "governance"
	check "vault wasm" "$(wasm_of "$VAULT_ID")" "$VAULT_WASM_HASH"
	check "timelock wasm" "$(wasm_of "$TIMELOCK_ID")" "$TIMELOCK_WASM_HASH"
	check "vault owner" "$(vv get_owner)" "$TIMELOCK_ID"
	check "timelock admin" "$(tv get_admin)" "$TIMELOCK_ID"
	check "timelock min_delay" "$(tv get_min_delay)" "$MIN_DELAY"
	check "guardian" "$(vv guardian)" "$GUARDIAN"
	check "treasury" "$(vv treasury)" "$TREASURY"
	check "compliance_role" "$(vv compliance_role)" "$COMPLIANCE_ROLE"
	check "asset" "$(vv query_asset)" "$ASSET_ID"
	check "allowlist_enforced" "$(vv allowlist_enforced)" "$ALLOWLIST_ENFORCED"
	check "paused" "$(vv paused)" "false"
	[[ -n "$MAX_DEPOSIT" ]] && check "max_deposit" "$(vv get_max_deposit)" "$MAX_DEPOSIT"
	[[ -n "$MAX_WITHDRAW" ]] && check "max_withdraw" "$(vv get_max_withdraw)" "$MAX_WITHDRAW"
	[[ -n "$LOCK_PERIOD" ]] && check "lock_period" "$(vv get_lock_period)" "$LOCK_PERIOD"
	[[ -n "$WITHDRAW_GRACE" ]] && check "withdraw_grace" "$(vv withdraw_grace_period)" "$WITHDRAW_GRACE"
	for role in proposer executor; do
		list=$PROPOSERS; [[ "$role" == executor ]] && list=$EXECUTORS
		for acct in $(addr_list "$list" | tr -d '[]"' | tr ',' ' '); do
			if [[ "$(tv has_role --account "$acct" --role "$role")" =~ ^[0-9]+$ ]]; then
				echo "  $role $acct OK"
			else
				echo "  $role $acct FAIL"; fail=1
			fi
		done
	done
	((fail == 0)) || die "verify failed"
	save VERIFIED 1
	echo "verify OK"
	;;
summary)
	need VAULT_ID TIMELOCK_ID
	echo "CONTRACT_ID=$VAULT_ID"
	echo "TIMELOCK_ID=$TIMELOCK_ID"
	echo "ASSET_ID=$ASSET_ID"
	echo "VAULT_WASM_HASH=$VAULT_WASM_HASH"
	echo "TIMELOCK_WASM_HASH=$TIMELOCK_WASM_HASH"
	echo "SOURCE_COMMIT=${SOURCE_COMMIT:-}"
	echo "VERIFIED=${VERIFIED:-0}"
	;;
*)
	sed -n '3,21p' "$0" | sed 's/^# \{0,1\}//'
	exit 1
	;;
esac
