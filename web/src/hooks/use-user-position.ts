/**
 * Aggregated user position query (shares, value, limits, wallet balance).
 * @module hooks/use-user-position
 */
import { useQuery } from "@tanstack/react-query";
import {
  fetchUserShares,
  fetchUserAssetsValue,
  fetchMaxWithdraw,
  fetchMaxDeposit,
  fetchUserAssetBalance,
} from "@/lib/soroban";

/**
 * Fetches the user's full vault position in a single query.
 * @param address - Connected wallet G-address; query is disabled when undefined
 * @returns React Query result with `{ shares, assetsValue, maxWithdraw, maxDeposit, walletBalance }`
 * @remarks Split into two phases: core position data (shares / value /
 * maxWithdraw) must succeed, while `maxDeposit` and `walletBalance` are
 * supplementary — they degrade to `Infinity` and `0` on failure rather than
 * blanking the whole dashboard. Refetches every 30s to keep balances fresh.
 */
export function useUserPosition(address: string | undefined) {
  return useQuery({
    queryKey: ["user", "position", address],
    queryFn: async () => {
      // Core position data — these must succeed
      const [shares, assetsValue, maxWithdraw] = await Promise.all([
        fetchUserShares(address!),
        fetchUserAssetsValue(address!),
        fetchMaxWithdraw(address!),
      ]);

      // Supplementary data — gracefully degrade if these fail
      const [maxDeposit, walletBalance] = await Promise.all([
        fetchMaxDeposit(address!).catch(() => Infinity),
        fetchUserAssetBalance(address!).catch(() => 0),
      ]);

      return { shares, assetsValue, maxWithdraw, maxDeposit, walletBalance };
    },
    enabled: !!address,
    staleTime: 15_000,
    refetchInterval: 30_000,
  });
}
