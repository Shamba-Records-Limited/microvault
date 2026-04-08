/**
 * Contract events query for the Vault/Governance transactions tabs.
 * @module hooks/use-contract-events
 */
import { useQuery, keepPreviousData } from "@tanstack/react-query";
import { fetchContractEvents } from "@/lib/stellar";
import type { EntrySource } from "@/types/transactions";

/**
 * Fetches a page of contract events via Soroban RPC `getEvents`.
 * @param contractId - C-address of the contract to query; query is disabled when undefined
 * @param source - Tab tag attached to each returned event
 * @param cursor - RPC cursor from a previous page; omit for the first page
 * @returns React Query result whose `data` is a `ContractEventsPage`
 * @remarks First-page queries poll every 15s for near-real-time updates; paged
 * queries don't refetch because history is immutable. Soroban RPC has no SSE,
 * so polling is the best option here. `keepPreviousData` prevents a flash of
 * empty state while a new page resolves.
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
