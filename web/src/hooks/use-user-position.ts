import { useQuery } from "@tanstack/react-query";
import {
  fetchUserShares,
  fetchUserAssetsValue,
  fetchMaxWithdraw,
  fetchMaxDeposit,
  fetchUserAssetBalance,
} from "@/lib/soroban";

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
