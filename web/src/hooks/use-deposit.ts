/**
 * Deposit mutation: build → sign via wallet kit → submit → toast + invalidate.
 * @module hooks/use-deposit
 */
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import {
  buildDepositTx,
  submitSignedTx,
  networkPassphrase,
} from "@/lib/soroban";
import { USDC_SCALE } from "@/lib/constants";
import { StellarWalletsKit } from "@/lib/wallet-kit";
import { parseContractError } from "@/lib/errors";
import { formatNumber } from "@/lib/format";

/**
 * Deposit USDC into the vault from a connected wallet.
 * @param address - Connected wallet G-address; mutation throws if undefined
 * @returns React Query mutation whose input is the major-unit USDC amount
 * @remarks Invalidates vault stats and user position on success so the UI
 * re-renders against fresh on-chain data. Wallet-cancel errors are surfaced
 * as an `info` toast instead of `error` so they don't look like failures.
 */
export function useDeposit(address: string | undefined) {
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: async (amount: number) => {
      if (!address) throw new Error("Wallet not connected");
      const rawAmount = BigInt(Math.round(amount * Number(USDC_SCALE)));
      const tx = await buildDepositTx(address, rawAmount);
      const { signedTxXdr } = await StellarWalletsKit.signTransaction(
        tx.toXDR(),
        { networkPassphrase, address },
      );
      return submitSignedTx(signedTxXdr);
    },
    onSuccess: (_data, amount) => {
      toast.success("Deposit successful", {
        description: `Deposited ${formatNumber(amount, 2)} USDC into the vault`,
      });
      queryClient.invalidateQueries({ queryKey: ["vault", "stats"] });
      queryClient.invalidateQueries({ queryKey: ["user", "position"] });
    },
    onError: (error) => {
      const message = parseContractError(error);
      if (message === "Transaction was cancelled") {
        toast.info(message);
      } else {
        toast.error("Deposit failed", { description: message });
      }
    },
  });
}
