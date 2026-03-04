import { useState, useEffect, useCallback } from "react";
import { toast } from "sonner";
import { StellarWalletsKit, KitEventType } from "@/lib/wallet-kit";

export function useWallet() {
  const [address, setAddress] = useState<string | undefined>();
  const isConnected = !!address;

  useEffect(() => {
    const unsub1 = StellarWalletsKit.on(KitEventType.STATE_UPDATED, (event) => {
      setAddress(event.payload.address);
    });

    const unsub2 = StellarWalletsKit.on(KitEventType.DISCONNECT, () => {
      setAddress(undefined);
    });

    return () => {
      unsub1();
      unsub2();
    };
  }, []);

  const connect = useCallback(async () => {
    try {
      const { address } = await StellarWalletsKit.authModal();
      setAddress(address);
      toast.success("Wallet connected", {
        description: `${address.slice(0, 4)}...${address.slice(-4)}`,
      });
    } catch {
      // User closed the modal
    }
  }, []);

  const disconnect = useCallback(async () => {
    await StellarWalletsKit.disconnect();
    setAddress(undefined);
    toast.info("Wallet disconnected");
  }, []);

  return { address, isConnected, connect, disconnect };
}
