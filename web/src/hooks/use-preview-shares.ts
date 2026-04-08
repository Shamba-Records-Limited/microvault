/**
 * Race-safe preview of share amounts for a deposit or withdrawal, backed by
 * React Query.
 * @module hooks/use-preview-shares
 */
import { keepPreviousData, useQuery } from "@tanstack/react-query";
import { fetchPreviewDeposit, fetchPreviewWithdraw } from "@/lib/soroban";

type PreviewMode = "deposit" | "withdraw";

/**
 * Previews the share delta for a given asset amount in a race-safe, cached way.
 * @param mode - `"deposit"` previews shares minted, `"withdraw"` previews shares burned
 * @param scaledAmount - i128 USDC amount already debounced and scaled upstream; `null` disables the query
 * @returns A React Query result whose `data` is the preview in raw i128 stroops
 * @remarks React Query keys the cache by `[mode, scaledAmount]`, so re-typing
 * the same value is an instant cache hit and late-arriving responses for old
 * values are dropped instead of overwriting the display. `keepPreviousData`
 * keeps the last good preview visible while the next one resolves so the UI
 * doesn't flicker. Previews are pure functions of (vault state, amount) and
 * vault state changes only on deposits/withdraws, so 30s staleTime is a very
 * safe trade for dramatically fewer RPC calls.
 */
export function usePreviewShares(
  mode: PreviewMode,
  scaledAmount: bigint | null,
) {
  return useQuery({
    queryKey: ["preview-shares", mode, scaledAmount?.toString() ?? null],
    queryFn: () => {
      // queryFn only runs when `enabled` is true, so the bang is safe.
      const amount = scaledAmount as bigint;
      return mode === "deposit"
        ? fetchPreviewDeposit(amount)
        : fetchPreviewWithdraw(amount);
    },
    enabled: scaledAmount !== null && scaledAmount > 0n,
    // Previews are pure functions of (vault state, amount). Vault state
    // changes only on deposits/withdraws, which are rare relative to typing.
    // 30s of cache freshness is plenty and saves a huge number of RPC hits.
    staleTime: 30_000,
    gcTime: 5 * 60_000,
    placeholderData: keepPreviousData,
    retry: false,
  });
}
