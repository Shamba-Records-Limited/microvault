/**
 * Transactions query with live Horizon SSE streaming for the Treasury tab.
 * @module hooks/use-transactions
 */
import { useRef } from "react";
import { useQuery, useQueryClient, keepPreviousData } from "@tanstack/react-query";
import { fetchTransactions, streamTransactions } from "@/lib/stellar";
import { useMountEffect } from "./use-mount-effect";
import type { EntrySource, TransactionsPage } from "@/types/transactions";

/**
 * Fetches transactions for a Stellar account or contract and keeps the first
 * page live via Horizon SSE.
 * @param accountId - G- or C-address to query; query is disabled when undefined
 * @param source - Tab tag attached to each row
 * @param cursor - Horizon paging token for subsequent pages; omit for the first page
 * @returns React Query result whose `data` is a `TransactionsPage`
 * @remarks Only the first page (no cursor) opens an SSE stream — paged history
 * is immutable, so streaming it would just burn connections. New records are
 * prepended directly into the query cache and deduped by op id because Horizon
 * may replay the cursor edge on reconnect.
 */
export function useTransactions(
  accountId: string | undefined,
  source: EntrySource,
  cursor?: string,
) {
  const queryClient = useQueryClient();
  const closeRef = useRef<(() => void) | null>(null);

  const query = useQuery({
    queryKey: ["transactions", source, cursor],
    queryFn: () => fetchTransactions(accountId!, source, cursor),
    enabled: !!accountId,
    staleTime: 15_000,
    placeholderData: keepPreviousData,
  });

  useMountEffect(() => {
    if (!accountId || cursor) return;

    closeRef.current = streamTransactions(accountId, source, (tx) => {
      queryClient.setQueryData<TransactionsPage>(
        ["transactions", source, undefined],
        (old) => {
          if (!old) return { transactions: [tx], cursor: undefined };
          // Dedupe by op id — Horizon may replay the cursor edge on reconnect.
          if (old.transactions.some((t) => t.id === tx.id)) return old;
          return {
            ...old,
            transactions: [tx, ...old.transactions],
          };
        },
      );
    });

    return () => closeRef.current?.();
  });

  return query;
}
