import { useQuery } from "@tanstack/react-query";
import { fetchVaultStats } from "@/lib/soroban";

/** Fetches vault metrics (TVL, borrowed, APR, etc.) with 30s stale time and 60s auto-refresh. */
export function useVaultStats() {
  return useQuery({
    queryKey: ["vault", "stats"],
    queryFn: fetchVaultStats,
    staleTime: 30_000,
    refetchInterval: 60_000,
  });
}
