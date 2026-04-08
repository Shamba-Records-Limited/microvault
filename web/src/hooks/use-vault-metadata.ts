/**
 * Vault metadata query (display name and asset symbol).
 * @module hooks/use-vault-metadata
 */
import { useQuery } from "@tanstack/react-query";
import { fetchVaultMetadata } from "@/lib/soroban";

/**
 * Fetches the vault's display name and underlying asset symbol.
 * @returns React Query result whose `data` is a `VaultMetadata` object
 * @remarks 5 min stale time — these fields only change on a contract upgrade,
 * so refetching more often is pure waste.
 */
export function useVaultMetadata() {
  return useQuery({
    queryKey: ["vault", "metadata"],
    queryFn: fetchVaultMetadata,
    staleTime: 300_000,
  });
}
