import { useState } from "react";
import { Card, CardContent, CardHeader } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import {
  ChevronDown,
  ChevronUp,
  Coins,
  Users,
  Landmark,
  Shield,
  Loader2,
} from "lucide-react";
import { toast } from "sonner";
import { cn } from "@/lib/utils";
import {
  formatCurrency,
  formatPercent,
  formatNumber,
  formatInputValue,
} from "@/lib/format";
import { DEFAULT_EXPLORER_URL, USDC_SCALE, SHARE_SCALE } from "@/lib/constants";
import { fetchPreviewDeposit, fetchPreviewWithdraw } from "@/lib/soroban";
import { parseContractError } from "@/lib/errors";
import { useWallet } from "@/hooks/use-wallet";
import { useUserPosition } from "@/hooks/use-user-position";
import { useDeposit } from "@/hooks/use-deposit";
import { useWithdraw } from "@/hooks/use-withdraw";
import type { Pool } from "@/types/vault";

const explorerUrl =
  import.meta.env.VITE_STELLAR_EXPLORER_URL || DEFAULT_EXPLORER_URL;

type ActiveTab = "deposit" | "withdraw";

interface PoolCardProps {
  pool: Pool;
}

/** Expandable card displaying pool metrics, governance addresses, and asset breakdown. */
export function PoolCard({ pool }: PoolCardProps) {
  const [isExpanded, setIsExpanded] = useState(false);
  const [activeTab, setActiveTab] = useState<ActiveTab>("deposit");
  const [displayAmount, setDisplayAmount] = useState("");
  const [rawAmount, setRawAmount] = useState("");
  const [preview, setPreview] = useState<string | null>(null);
  const [previewLoading, setPreviewLoading] = useState(false);

  const { address, isConnected, connect } = useWallet();
  const position = useUserPosition(address);
  const deposit = useDeposit(address);
  const withdraw = useWithdraw(address);

  const parsedAmount = parseFloat(rawAmount);
  const isValidAmount = !isNaN(parsedAmount) && parsedAmount > 0;
  const isPending = deposit.isPending || withdraw.isPending;

  async function handleAmountChange(value: string) {
    const { display, raw } = formatInputValue(value);
    setDisplayAmount(display);
    setRawAmount(raw);
    setPreview(null);

    const num = parseFloat(raw);
    if (isNaN(num) || num <= 0) return;

    setPreviewLoading(true);
    try {
      const scaled = BigInt(Math.round(num * Number(USDC_SCALE)));
      const sharesRaw =
        activeTab === "deposit"
          ? await fetchPreviewDeposit(scaled)
          : await fetchPreviewWithdraw(scaled);
      const sharesFormatted = formatNumber(
        Number(sharesRaw) / Number(SHARE_SCALE),
      );
      setPreview(`~${sharesFormatted} ${pool.assets[0].symbol} shares`);
    } catch (error) {
      setPreview(null);
      toast.warning("Preview failed", {
        description: parseContractError(error),
      });
    } finally {
      setPreviewLoading(false);
    }
  }

  function handleTabSwitch(tab: ActiveTab) {
    setActiveTab(tab);
    setDisplayAmount("");
    setRawAmount("");
    setPreview(null);
    deposit.reset();
    withdraw.reset();
  }

  async function handleSubmit() {
    if (!isValidAmount) return;

    // Client-side validation
    if (position.data) {
      if (activeTab === "deposit") {
        if (parsedAmount > position.data.walletBalance) {
          toast.warning("Insufficient balance", {
            description: `You only have ${formatNumber(position.data.walletBalance, 2)} USDC in your wallet`,
          });
          return;
        }
        if (parsedAmount > position.data.maxDeposit) {
          toast.warning("Exceeds max deposit", {
            description: `Maximum deposit is ${formatNumber(position.data.maxDeposit, 2)} USDC`,
          });
          return;
        }
      } else {
        if (parsedAmount > position.data.maxWithdraw) {
          toast.warning("Exceeds max withdraw", {
            description: `Maximum withdrawal is ${formatNumber(position.data.maxWithdraw, 2)} USDC`,
          });
          return;
        }
      }
    }

    try {
      if (activeTab === "deposit") {
        await deposit.mutateAsync(parsedAmount);
      } else {
        await withdraw.mutateAsync(parsedAmount);
      }
      setDisplayAmount("");
      setRawAmount("");
      setPreview(null);
    } catch {
      // Handled by mutation onError toast
    }
  }

  return (
    <Card className="overflow-hidden transition-all duration-200 hover:shadow-md">
      <CardHeader
        className="cursor-pointer"
        onClick={() => setIsExpanded(!isExpanded)}
      >
        <div className="flex items-center justify-between">
          <div className="flex items-center gap-3">
            <div className="h-10 w-10 rounded-lg bg-primary/10 flex items-center justify-center shrink-0">
              <Coins className="h-5 w-5 text-primary" />
            </div>
            <div>
              <div className="flex items-center gap-2">
                <h3 className="font-semibold text-base">{pool.name}</h3>
                <div
                  className={cn(
                    "h-2.5 w-2.5 rounded-full",
                    pool.status === "active" &&
                      "bg-green-500 ring-2 ring-green-500/50 animate-caret-blink",
                    pool.status === "frozen" && "bg-gray-400",
                  )}
                />
              </div>
              <a
                href={`${explorerUrl}/contract/${pool.address}`}
                target="_blank"
                rel="noopener noreferrer"
                onClick={(e) => e.stopPropagation()}
                className="text-xs text-muted-foreground font-mono hover:text-foreground transition-colors"
              >
                {pool.address.slice(0, 8)}...{pool.address.slice(-6)}
              </a>
              {/* Mobile stats: below name */}
              <div className="flex items-center gap-4 mt-2 md:hidden">
                <div>
                  <p className="text-xs text-muted-foreground">TVL</p>
                  <p className="font-semibold text-sm">
                    {formatCurrency(pool.tvl)}
                  </p>
                </div>
                <div>
                  <p className="text-xs text-muted-foreground">APY</p>
                  <p className="font-semibold text-primary text-sm">
                    {formatPercent(pool.apy)}
                  </p>
                </div>
              </div>
            </div>
          </div>

          <div className="flex items-center gap-6">
            {/* Desktop stats: right side */}
            <div className="hidden md:flex items-center gap-8">
              <div className="text-right">
                <p className="text-xs text-muted-foreground">TVL</p>
                <p className="font-semibold">{formatCurrency(pool.tvl)}</p>
              </div>
              <div className="text-right">
                <p className="text-xs text-muted-foreground">APY</p>
                <p className="font-semibold text-primary">
                  {formatPercent(pool.apy)}
                </p>
              </div>
            </div>

            {isExpanded ? (
              <ChevronUp className="h-5 w-5 text-muted-foreground" />
            ) : (
              <ChevronDown className="h-5 w-5 text-muted-foreground" />
            )}
          </div>
        </div>
      </CardHeader>

      {isExpanded && (
        <CardContent className="border-t border-border bg-muted/30 pt-4">
          <div className="grid gap-6">
            {/* Owner, Treasury & Guardian */}
            <div className="flex flex-wrap gap-6">
              {pool.admin && (
                <div className="flex items-center gap-2">
                  <Users className="h-4 w-4 text-muted-foreground shrink-0" />
                  <div>
                    <p className="text-xs text-muted-foreground">Owner</p>
                    <a
                      href={`${explorerUrl}/contract/${pool.admin}`}
                      target="_blank"
                      rel="noopener noreferrer"
                      className="font-medium font-mono text-xs hover:underline"
                    >
                      {pool.admin.slice(0, 8)}...{pool.admin.slice(-6)}
                    </a>
                  </div>
                </div>
              )}
              <div className="flex items-center gap-2">
                <Landmark className="h-4 w-4 text-muted-foreground shrink-0" />
                <div>
                  <p className="text-xs text-muted-foreground">Treasury</p>
                  <a
                    href={`${explorerUrl}/account/${pool.treasury}`}
                    target="_blank"
                    rel="noopener noreferrer"
                    className="font-medium font-mono text-xs hover:underline"
                  >
                    {pool.treasury.slice(0, 8)}...{pool.treasury.slice(-6)}
                  </a>
                </div>
              </div>
              {pool.guardian && (
                <div className="flex items-center gap-2">
                  <Shield className="h-4 w-4 text-muted-foreground shrink-0" />
                  <div>
                    <p className="text-xs text-muted-foreground">Guardian</p>
                    <a
                      href={`${explorerUrl}/account/${pool.guardian}`}
                      target="_blank"
                      rel="noopener noreferrer"
                      className="font-medium font-mono text-xs hover:underline"
                    >
                      {pool.guardian.slice(0, 8)}...{pool.guardian.slice(-6)}
                    </a>
                  </div>
                </div>
              )}
            </div>

            {/* Asset Breakdown */}
            <div>
              <h4 className="text-sm font-medium mb-3">Asset Breakdown</h4>

              {/* Mobile: stacked cards */}
              <div className="md:hidden space-y-3">
                {pool.assets.map((asset) => (
                  <div
                    key={asset.symbol}
                    className="rounded-lg border border-border/50 p-3 space-y-2"
                  >
                    <p className="font-medium">
                      {asset.contractAddress ? (
                        <a
                          href={`${explorerUrl}/contract/${asset.contractAddress}`}
                          target="_blank"
                          rel="noopener noreferrer"
                          className="hover:underline"
                        >
                          {asset.symbol}
                        </a>
                      ) : (
                        asset.symbol
                      )}
                    </p>
                    <div className="grid grid-cols-2 gap-x-4 gap-y-1 text-sm">
                      <span className="text-muted-foreground">Supplied</span>
                      <span className="text-right">
                        {formatCurrency(asset.supplied)}
                      </span>
                      <span className="text-muted-foreground">Borrowed</span>
                      <span className="text-right">
                        {formatCurrency(asset.borrowed)}
                      </span>
                      <span className="text-muted-foreground">Liquidity</span>
                      <span className="text-right">
                        {formatCurrency(asset.supplied - asset.borrowed)}
                      </span>
                      <span className="text-muted-foreground">Utilization</span>
                      <span className="text-right">
                        {asset.supplied > 0
                          ? formatPercent(
                              (asset.borrowed / asset.supplied) * 100,
                            )
                          : "0.00%"}
                      </span>
                    </div>
                  </div>
                ))}
              </div>

              {/* Desktop: table */}
              <div className="hidden md:block overflow-x-auto">
                <table className="w-full text-sm">
                  <thead>
                    <tr className="border-b border-border">
                      <th className="text-left py-2 font-medium text-muted-foreground">
                        Asset
                      </th>
                      <th className="text-right py-2 font-medium text-muted-foreground">
                        Supplied
                      </th>
                      <th className="text-right py-2 font-medium text-muted-foreground">
                        Borrowed
                      </th>
                      <th className="text-right py-2 font-medium text-muted-foreground">
                        Liquidity
                      </th>
                      <th className="text-right py-2 font-medium text-muted-foreground">
                        Utilization
                      </th>
                    </tr>
                  </thead>
                  <tbody>
                    {pool.assets.map((asset) => (
                      <tr
                        key={asset.symbol}
                        className="border-b border-border/50 last:border-0"
                      >
                        <td className="py-2 font-medium">
                          {asset.contractAddress ? (
                            <a
                              href={`${explorerUrl}/contract/${asset.contractAddress}`}
                              target="_blank"
                              rel="noopener noreferrer"
                              className="hover:underline"
                            >
                              {asset.symbol}
                            </a>
                          ) : (
                            asset.symbol
                          )}
                        </td>
                        <td className="text-right py-2">
                          {formatCurrency(asset.supplied)}
                        </td>
                        <td className="text-right py-2">
                          {formatCurrency(asset.borrowed)}
                        </td>
                        <td className="text-right py-2">
                          {formatCurrency(asset.supplied - asset.borrowed)}
                        </td>
                        <td className="text-right py-2">
                          {asset.supplied > 0
                            ? formatPercent(
                                (asset.borrowed / asset.supplied) * 100,
                              )
                            : "0.00%"}
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            </div>

            {/* Deposit / Withdraw */}
            <div className="rounded-lg border border-border/50 p-4">
              {/* User position (when connected) */}
              {isConnected && position.data && (
                <div className="flex flex-wrap gap-6 mb-4 pb-4 border-b border-border/50">
                  <div>
                    <p className="text-xs text-muted-foreground">
                      Wallet Balance
                    </p>
                    <p className="font-semibold text-sm">
                      {formatNumber(position.data.walletBalance, 7)} USDC
                    </p>
                  </div>
                  <div>
                    <p className="text-xs text-muted-foreground">
                      Your Position
                    </p>
                    <p className="font-semibold text-sm">
                      {formatNumber(position.data.assetsValue, 7)} USDC
                    </p>
                  </div>
                  <div>
                    <p className="text-xs text-muted-foreground">Shares</p>
                    <p className="font-semibold text-sm">
                      {formatNumber(position.data.shares)}{" "}
                      {pool.assets[0].symbol}
                    </p>
                  </div>
                  <div>
                    <p className="text-xs text-muted-foreground">
                      Max Withdraw
                    </p>
                    <p className="font-semibold text-sm">
                      {formatNumber(position.data.maxWithdraw, 7)} USDC
                    </p>
                  </div>
                </div>
              )}

              {/* Tab toggle */}
              <div className="flex gap-1 mb-4 rounded-md bg-muted p-1">
                <button
                  type="button"
                  className={cn(
                    "flex-1 rounded-sm px-3 py-1.5 text-sm font-medium transition-colors",
                    activeTab === "deposit"
                      ? "bg-background text-foreground shadow-sm"
                      : "text-muted-foreground hover:text-foreground",
                  )}
                  onClick={() => handleTabSwitch("deposit")}
                >
                  Deposit
                </button>
                <button
                  type="button"
                  className={cn(
                    "flex-1 rounded-sm px-3 py-1.5 text-sm font-medium transition-colors",
                    activeTab === "withdraw"
                      ? "bg-background text-foreground shadow-sm"
                      : "text-muted-foreground hover:text-foreground",
                  )}
                  onClick={() => handleTabSwitch("withdraw")}
                >
                  Withdraw
                </button>
              </div>

              {/* Amount input */}
              <div className="space-y-3">
                <div className="relative">
                  <input
                    type="text"
                    inputMode="decimal"
                    placeholder={
                      activeTab === "deposit" ? "0.00 USDC" : "0.00 USDC"
                    }
                    value={displayAmount}
                    onChange={(e) => handleAmountChange(e.target.value)}
                    disabled={isPending}
                    className="w-full rounded-md border border-input bg-background px-3 py-2 pr-16 text-sm placeholder:text-muted-foreground focus:outline-none focus:ring-2 focus:ring-ring disabled:opacity-50"
                  />
                  {isConnected &&
                    position.data &&
                    activeTab === "deposit" &&
                    position.data.walletBalance > 0 && (
                      <button
                        type="button"
                        className="absolute right-2 top-1/2 -translate-y-1/2 rounded px-2 py-0.5 text-xs font-medium text-primary hover:bg-primary/10 transition-colors"
                        onClick={() => {
                          const max =
                            Math.floor(
                              Math.min(
                                position.data!.walletBalance,
                                position.data!.maxDeposit,
                              ) * 100,
                            ) / 100;
                          handleAmountChange(max.toString());
                        }}
                      >
                        MAX
                      </button>
                    )}
                  {isConnected &&
                    position.data &&
                    activeTab === "withdraw" &&
                    position.data.maxWithdraw > 0 && (
                      <button
                        type="button"
                        className="absolute right-2 top-1/2 -translate-y-1/2 rounded px-2 py-0.5 text-xs font-medium text-primary hover:bg-primary/10 transition-colors"
                        onClick={() => {
                          const max =
                            Math.floor(position.data!.maxWithdraw * 100) / 100;
                          handleAmountChange(max.toString());
                        }}
                      >
                        MAX
                      </button>
                    )}
                </div>

                {/* Preview */}
                {previewLoading && (
                  <p className="text-xs text-muted-foreground">
                    Calculating preview...
                  </p>
                )}
                {preview && !previewLoading && (
                  <p className="text-xs text-muted-foreground">
                    {activeTab === "deposit"
                      ? "You will receive"
                      : "This will burn"}{" "}
                    {preview}
                  </p>
                )}

                {/* Submit button */}
                {isConnected ? (
                  <Button
                    className="w-full"
                    disabled={!isValidAmount || isPending}
                    onClick={handleSubmit}
                  >
                    {isPending && <Loader2 className="h-4 w-4 animate-spin" />}
                    {isPending
                      ? "Confirming..."
                      : activeTab === "deposit"
                        ? "Deposit"
                        : "Withdraw"}
                  </Button>
                ) : (
                  <Button className="w-full" onClick={connect}>
                    Connect Wallet
                  </Button>
                )}
              </div>
            </div>
          </div>
        </CardContent>
      )}
    </Card>
  );
}
