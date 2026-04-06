import { useQuery, keepPreviousData } from "@tanstack/react-query";
import { fetchContractEvents } from "@/lib/stellar";
import type { EntrySource } from "@/types/transactions";

/**
 * Fetches contract events from the Stellar Expert API.
 *
 * - Uses polling (15s) on the first page for near-real-time updates.
 * - Stellar Expert has no SSE, so polling is the best we can do.
 */
export function useContractEvents(
  contractId: string | undefined,
  source: EntrySource,
  cursor?: string,
) {
  return useQuery({
    queryKey: ["contract-events", source, cursor],
    queryFn: () => fetchContractEvents(contractId!, source, cursor),
    enabled: !!contractId,
    staleTime: 15_000,
    refetchInterval: cursor ? false : 15_000,
    placeholderData: keepPreviousData,
  });
}
