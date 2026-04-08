/**
 * Vault governance addresses query (owner, treasury, guardian).
 * @module hooks/use-vault-addresses
 */
import { useQuery } from "@tanstack/react-query";
import { fetchVaultAddresses } from "@/lib/soroban";

/**
 * Fetches the vault's governance addresses.
 * @returns React Query result whose `data` is a `VaultAddresses` object
 * @remarks 5 min stale time — governance transfers are rare, so polling more
 * often would be pure waste.
 */
export function useVaultAddresses() {
  return useQuery({
    queryKey: ["vault", "addresses"],
    queryFn: fetchVaultAddresses,
    staleTime: 300_000,
  });
}
