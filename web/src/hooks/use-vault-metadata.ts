import { useQuery } from "@tanstack/react-query";
import { fetchVaultMetadata } from "@/lib/soroban";

/** Fetches vault name and asset symbol from on-chain view functions with 5min stale time. */
export function useVaultMetadata() {
  return useQuery({
    queryKey: ["vault", "metadata"],
    queryFn: fetchVaultMetadata,
    staleTime: 300_000,
  });
}
