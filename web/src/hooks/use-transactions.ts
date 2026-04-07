import { useRef } from "react";
import { useQuery, useQueryClient, keepPreviousData } from "@tanstack/react-query";
import { fetchTransactions, streamTransactions } from "@/lib/stellar";
import { useMountEffect } from "./use-mount-effect";
import type { EntrySource, TransactionsPage } from "@/types/transactions";

/**
 * Fetches transactions for any Stellar account or contract via Horizon.
 *
 * - Uses `useQuery` for initial + paginated loads.
 * - On the first page (no cursor), opens an SSE stream via `useMountEffect`
 *   that prepends new transactions to the query cache in real-time.
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
