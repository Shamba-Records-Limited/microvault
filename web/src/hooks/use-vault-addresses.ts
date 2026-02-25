import { useQuery } from "@tanstack/react-query";
import { fetchVaultAddresses } from "@/lib/soroban";

/** Fetches vault governance addresses (owner, treasury, guardian) with 5min stale time. */
export function useVaultAddresses() {
  return useQuery({
    queryKey: ["vault", "addresses"],
    queryFn: fetchVaultAddresses,
    staleTime: 300_000,
  });
}
