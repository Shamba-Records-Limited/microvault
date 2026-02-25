import { useQuery } from "@tanstack/react-query";
import { fetchVaultStats } from "@/lib/soroban";

export function useVaultStats() {
  return useQuery({
    queryKey: ["vault", "stats"],
    queryFn: fetchVaultStats,
    staleTime: 30_000,
    refetchInterval: 60_000,
  });
}
