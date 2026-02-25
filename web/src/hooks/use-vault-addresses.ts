import { useQuery } from "@tanstack/react-query";
import { fetchVaultAddresses } from "@/lib/soroban";

export function useVaultAddresses() {
  return useQuery({
    queryKey: ["vault", "addresses"],
    queryFn: fetchVaultAddresses,
    staleTime: 300_000,
  });
}
