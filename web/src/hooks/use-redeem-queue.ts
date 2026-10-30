/**
 * Queue mutations: request a withdrawal, cancel a request. Each builds the
 * call, signs it in the wallet, submits it, then refreshes the position.
 * @module hooks/use-redeem-queue
 */
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import {
  buildRequestRedeemTx,
  buildCancelRedeemTx,
  fetchSharesForAssets,
  submitSignedTx,
  networkPassphrase,
} from "@/lib/soroban";
import { USDC_SCALE } from "@/lib/constants";
import { StellarWalletsKit } from "@/lib/wallet-kit";
import { parseContractError } from "@/lib/errors";
import { formatNumber } from "@/lib/format";
import type { Transaction } from "@stellar/stellar-sdk";

async function signAndSubmit(tx: Transaction, address: string) {
  const { signedTxXdr } = await StellarWalletsKit.signTransaction(tx.toXdr(), {
    networkPassphrase,
    address,
  });
  return submitSignedTx(signedTxXdr);
}

function useRefresh() {
  const queryClient = useQueryClient();
  return () => {
    queryClient.invalidateQueries({ queryKey: ["vault"] });
    queryClient.invalidateQueries({ queryKey: ["user"] });
  };
}

function onFailure(title: string) {
  return (error: unknown) => {
    const message = parseContractError(error);
    if (message === "Transaction was cancelled") {
      toast.info(message);
    } else {
      toast.error(title, { description: message });
    }
  };
}

/**
 * Queues a withdrawal of a USDC amount, or of every redeemable share.
 * @param address - Connected wallet G-address; mutation throws if undefined
 * @returns Mutation whose input is `{ amount, maxShares }`: the USDC amount in
 * major units, and the most shares the vault will escrow for this address
 * @remarks The amount is converted to shares as a withdrawal would round it,
 * capped at `maxShares`, so asking for the whole position never overshoots.
 */
export function useRequestRedeem(address: string | undefined) {
  const refresh = useRefresh();
  return useMutation({
    mutationFn: async ({ amount, maxShares }: { amount: number; maxShares: bigint }) => {
      if (!address) throw new Error("Wallet not connected");
      const assets = BigInt(Math.round(amount * Number(USDC_SCALE)));
      const wanted = await fetchSharesForAssets(assets);
      const shares = wanted > maxShares ? maxShares : wanted;
      if (shares <= 0n) throw new Error("Nothing to withdraw yet");
      return signAndSubmit(await buildRequestRedeemTx(address, shares), address);
    },
    onSuccess: (_data, { amount }) => {
      toast.success("Withdrawal requested", {
        description: `${formatNumber(amount, 2)} USDC joined the queue; it is paid oldest first as borrowers repay`,
      });
      refresh();
    },
    onError: onFailure("Request failed"),
  });
}

/**
 * Cancels one of the lender's requests; unfilled shares return to the wallet.
 * @param address - Connected wallet G-address; mutation throws if undefined
 */
export function useCancelRedeem(address: string | undefined) {
  const refresh = useRefresh();
  return useMutation({
    mutationFn: async (id: bigint) => {
      if (!address) throw new Error("Wallet not connected");
      return signAndSubmit(await buildCancelRedeemTx(address, id), address);
    },
    onSuccess: () => {
      toast.success("Request cancelled", {
        description: "The unpaid shares are back in your wallet",
      });
      refresh();
    },
    onError: onFailure("Cancel failed"),
  });
}
