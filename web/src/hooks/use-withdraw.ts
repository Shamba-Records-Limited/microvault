import { useMutation, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import {
  buildWithdrawTx,
  submitSignedTx,
  networkPassphrase,
} from "@/lib/soroban";
import { USDC_SCALE } from "@/lib/constants";
import { StellarWalletsKit } from "@/lib/wallet-kit";
import { parseContractError } from "@/lib/errors";
import { formatNumber } from "@/lib/format";

export function useWithdraw(address: string | undefined) {
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: async (amount: number) => {
      if (!address) throw new Error("Wallet not connected");
      const rawAmount = BigInt(Math.round(amount * Number(USDC_SCALE)));
      const tx = await buildWithdrawTx(address, rawAmount);
      const { signedTxXdr } = await StellarWalletsKit.signTransaction(
        tx.toXDR(),
        { networkPassphrase, address },
      );
      return submitSignedTx(signedTxXdr);
    },
    onSuccess: (_data, amount) => {
      toast.success("Withdrawal successful", {
        description: `Withdrew ${formatNumber(amount, 2)} USDC from the vault`,
      });
      queryClient.invalidateQueries({ queryKey: ["vault", "stats"] });
      queryClient.invalidateQueries({ queryKey: ["user", "position"] });
    },
    onError: (error) => {
      const message = parseContractError(error);
      if (message === "Transaction was cancelled") {
        toast.info(message);
      } else {
        toast.error("Withdrawal failed", { description: message });
      }
    },
  });
}
