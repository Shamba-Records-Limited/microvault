/**
 * Vault metrics query (TVL, borrowed, APR, utilization, paused).
 * @module hooks/use-vault-stats
 */
import { useQuery } from "@tanstack/react-query";
import { fetchVaultStats } from "@/lib/soroban";

/**
 * Fetches the vault's core metrics and auto-refreshes in the background.
 * @returns React Query result whose `data` is a `VaultStats` object
 * @remarks 30s stale time + 60s refetch interval — enough to feel live for a
 * dashboard without hammering RPC.
 */
export function useVaultStats() {
  return useQuery({
    queryKey: ["vault", "stats"],
    queryFn: fetchVaultStats,
    staleTime: 30_000,
    refetchInterval: 60_000,
  });
}
