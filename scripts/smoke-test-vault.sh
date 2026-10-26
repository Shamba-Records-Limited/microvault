#!/usr/bin/env bash
# Post-deploy / post-upgrade smoke test for the microvault Soroban contract.
#
# Exits non-zero on the first view function that fails to simulate, so it
# can be wired into a deploy pipeline as a hard gate. See
# docs/soroban-contract-upgrade-procedure.md for the failure modes this
# guards against (notably OZ stellar-tokens v0.6 -> v0.7 storage breakage).
#
# Usage:
#   CONTRACT_ID=CCRTA... NETWORK=testnet HOLDER=GA... ./scripts/smoke-test-vault.sh
#
# Env vars:
#   CONTRACT_ID  required - the deployed vault contract id
#   NETWORK      required - stellar CLI network alias (testnet, mainnet, ...)
#   HOLDER       optional - strkey of an account to test balance() against;
#                if unset, the holder probes are skipped

set -euo pipefail

: "${CONTRACT_ID:?CONTRACT_ID is required}"
: "${NETWORK:?NETWORK is required}"

red()   { printf '\033[31m%s\033[0m\n' "$*"; }
green() { printf '\033[32m%s\033[0m\n' "$*"; }

probe() {
  local fn=$1
  shift || true
  printf '  %-32s ' "$fn"
  local attempt ok=1
  for attempt in 1 2 3; do
    if stellar contract invoke \
          --id "$CONTRACT_ID" \
          --network "$NETWORK" \
          --send=no \
          -- "$fn" "$@" >/tmp/smoke.out 2>/tmp/smoke.err
    then
      ok=0
      break
    fi
    sleep 3
  done
  if [[ $ok -eq 0 ]]
  then
    green "OK"
  else
    red "FAIL"
    echo "    stderr:"
    sed 's/^/    /' /tmp/smoke.err
    return 1
  fi
}

echo "Smoke test against $CONTRACT_ID on $NETWORK"

echo "  metadata"
probe name
probe symbol
probe decimals

echo "  vault state"
probe query_asset
probe total_managed_assets
probe total_borrowed
probe available_liquidity
probe utilization_rate
probe borrow_apr
probe paused

echo "  share supply"
probe total_assets
assets=$(tr -d '"' </tmp/smoke.out)
probe total_supply
supply=$(tr -d '"' </tmp/smoke.out)
if [[ "$assets" != "0" && "$supply" == "0" ]]; then
  red "  total_supply is 0 while total_assets is $assets: run migrate_total_supply (OZ 0.7.x -> 0.8.x)"
  exit 1
fi

echo "  governance"
probe treasury
probe get_owner
probe guardian

if [[ -n "${HOLDER:-}" ]]; then
  echo "  holder probe"
  probe balance --account "$HOLDER"
  probe max_withdraw --owner "$HOLDER"
fi

green "All vault views OK."
