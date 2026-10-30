/**
 * Withdrawal queue, the connected lender's open requests, and what the vault
 * lets that address do.
 * @module hooks/use-queue
 */
import { useQuery } from "@tanstack/react-query";
import { fetchQueueState, fetchUserRequests, fetchLenderStatus } from "@/lib/soroban";

/**
 * Fetches the queue state, refreshed every 30s.
 * @returns React Query result with the vault's `QueueState`
 */
export function useQueueState() {
  return useQuery({
    queryKey: ["vault", "queue"],
    queryFn: fetchQueueState,
    staleTime: 15_000,
    refetchInterval: 30_000,
  });
}

/**
 * Fetches the connected lender's open withdrawal requests.
 * @param address - Connected wallet G-address; disabled when undefined
 * @remarks Depends on the queue state for the ids to scan.
 */
export function useUserRequests(address: string | undefined) {
  const queue = useQueueState();
  return useQuery({
    queryKey: [
      "user",
      "requests",
      address,
      queue.data?.head.toString(),
      queue.data?.length.toString(),
    ],
    queryFn: () => fetchUserRequests(address!, queue.data!),
    enabled: !!address && !!queue.data,
    staleTime: 15_000,
    refetchInterval: 30_000,
  });
}

/**
 * Fetches the connected lender's allowlist, freeze, exit and lock status.
 * @param address - Connected wallet G-address; disabled when undefined
 */
export function useLenderStatus(address: string | undefined) {
  return useQuery({
    queryKey: ["user", "status", address],
    queryFn: () => fetchLenderStatus(address!),
    enabled: !!address,
    staleTime: 15_000,
    refetchInterval: 30_000,
  });
}
