import { keepPreviousData, useQuery } from "@tanstack/react-query";
import { fetchPreviewDeposit, fetchPreviewWithdraw } from "@/lib/soroban";

type PreviewMode = "deposit" | "withdraw";

/**
 * Race-safe, cached preview of share amounts for a deposit or withdrawal.
 *
 * - `scaledAmount` is the i128 USDC amount (already debounced upstream).
 * - React Query keys the cache by `[mode, scaledAmount]`, so:
 *   - re-typing the same value is an instant cache hit (no RPC)
 *   - if the user keeps typing, only the response matching the *current*
 *     key is rendered — late-arriving stale responses for old values are
 *     dropped instead of overwriting the display.
 * - `keepPreviousData` keeps the last good preview visible while the next
 *   one resolves, so the number doesn't flicker to "—" between keystrokes.
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
