/**
 * Expandable pool card: metrics, governance addresses, asset breakdown, and
 * deposit/withdraw form wired to the vault contract.
 * @module components/pools/PoolCard
 */
import { useState, useMemo } from "react";
import { Card, CardContent, CardHeader } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { ArrowSquareOut, CaretDown, CaretUp, CircleNotch } from "@phosphor-icons/react";
import { toast } from "sonner";
import { cn } from "@/lib/utils";
import {
  formatCurrency,
  formatPercent,
  formatNumber,
  formatInputValue,
} from "@/lib/format";
import { DEFAULT_EXPLORER_URL, USDC_SCALE, SHARE_SCALE } from "@/lib/constants";
import { useWallet } from "@/hooks/use-wallet";
import { useUserPosition } from "@/hooks/use-user-position";
import { useDeposit } from "@/hooks/use-deposit";
import { useWithdraw } from "@/hooks/use-withdraw";
import { useDebouncedValue } from "@/hooks/use-debounced-value";
import { usePreviewShares } from "@/hooks/use-preview-shares";
import { useQueueState, useUserRequests, useLenderStatus } from "@/hooks/use-queue";
import { useRequestRedeem, useCancelRedeem } from "@/hooks/use-redeem-queue";
import type { LenderStatus } from "@/types/vault";
import type { Pool } from "@/types/vault";

const yieldNote =
  "Estimated yearly return at today's rates: the borrow rate, compounded as the vault accrues it, times the share of the pool that is lent out. It moves as lending changes.";

const explorerUrl =
  import.meta.env.VITE_STELLAR_EXPLORER_URL || DEFAULT_EXPLORER_URL;

type ActiveTab = "deposit" | "withdraw";

interface PoolCardProps {
  pool: Pool;
}

/**
 * Expandable card displaying pool metrics, governance addresses, asset
 * breakdown, and an integrated deposit/withdraw form in an asymmetric split.
 * @param props.pool - Pool record assembled from vault metadata + stats
 */
export function PoolCard({ pool }: PoolCardProps) {
  const [isExpanded, setIsExpanded] = useState(false);
  const [activeTab, setActiveTab] = useState<ActiveTab>("deposit");
  const [displayAmount, setDisplayAmount] = useState("");
  const [rawAmount, setRawAmount] = useState("");

  const { address, isConnected, connect } = useWallet();
  const position = useUserPosition(address);
  const deposit = useDeposit(address);
  const withdraw = useWithdraw(address);
  const queue = useQueueState();
  const requests = useUserRequests(address);
  const status = useLenderStatus(address);
  const requestRedeem = useRequestRedeem(address);
  const cancelRedeem = useCancelRedeem(address);

  const parsedAmount = parseFloat(rawAmount);
  const isValidAmount = !isNaN(parsedAmount) && parsedAmount > 0;
  const isPending = deposit.isPending || withdraw.isPending || requestRedeem.isPending;

  // With anything queued the vault refuses instant withdrawals, and above
  // what idle liquidity can pay a withdrawal has to wait its turn too.
  const queueBusy = (queue.data?.queuedAssets ?? 0) > 0;
  const queueMode =
    activeTab === "withdraw" &&
    (queueBusy || (isValidAmount && !!position.data && parsedAmount > position.data.maxWithdraw));

  // Immediate inline reactive validation error
  const validationError = useMemo(() => {
    if (!rawAmount || !isValidAmount) return null;
    if (!position.data) return null;

    if (activeTab === "deposit") {
      if (parsedAmount > position.data.walletBalance) {
        return `Insufficient balance: You only have ${formatNumber(position.data.walletBalance, 4)} USDC`;
      }
      if (parsedAmount > position.data.maxDeposit) {
        return `Exceeds max deposit: Limit is ${formatNumber(position.data.maxDeposit, 4)} USDC`;
      }
    } else if (queueMode) {
      if (status.data?.frozen) {
        return "This position is frozen and cannot be withdrawn";
      }
      if (parsedAmount > position.data.assetsValue) {
        return `More than your position: ${formatNumber(position.data.assetsValue, 4)} USDC`;
      }
      if (queue.data && parsedAmount < queue.data.minRequest) {
        return `Below the minimum request of ${formatNumber(queue.data.minRequest, 2)} USDC`;
      }
      if (status.data && parsedAmount > status.data.maxPerRequest) {
        return `Above the per-request maximum of ${formatNumber(status.data.maxPerRequest, 2)} USDC; split it into several requests`;
      }
    } else {
      if (parsedAmount > position.data.maxWithdraw) {
        return `Exceeds max withdraw: Limit is ${formatNumber(position.data.maxWithdraw, 4)} USDC`;
      }
    }
    return null;
  }, [rawAmount, isValidAmount, activeTab, position.data, parsedAmount, queueMode, queue.data, status.data]);

  // Debounce the typed value before firing the preview RPC.
  const debouncedRaw = useDebouncedValue(rawAmount, 200);
  const debouncedScaled = useMemo<bigint | null>(() => {
    const num = parseFloat(debouncedRaw);
    if (isNaN(num) || num <= 0) return null;
    return BigInt(Math.round(num * Number(USDC_SCALE)));
  }, [debouncedRaw]);

  const previewQuery = usePreviewShares(activeTab, debouncedScaled);

  // Suppress preview results that are out of sync with current typing.
  const isPreviewStale = rawAmount !== debouncedRaw;
  const preview =
    isValidAmount && previewQuery.data && !isPreviewStale
      ? `~${formatNumber(Number(previewQuery.data) / Number(SHARE_SCALE))} ${pool.assets[0].symbol} shares`
      : null;
  const previewLoading =
    isValidAmount && (isPreviewStale || previewQuery.isFetching);

  function handleAmountChange(value: string) {
    const { display, raw } = formatInputValue(value);
    setDisplayAmount(display);
    setRawAmount(raw);
  }

  function handleTabSwitch(tab: ActiveTab) {
    setActiveTab(tab);
    setDisplayAmount("");
    setRawAmount("");
    deposit.reset();
    withdraw.reset();
    requestRedeem.reset();
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
      } else if (!queueMode) {
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
      } else if (queueMode) {
        if (validationError) return;
        await requestRedeem.mutateAsync({
          amount: parsedAmount,
          maxShares: status.data?.shareBalance ?? 0n,
        });
      } else {
        await withdraw.mutateAsync(parsedAmount);
      }
      setDisplayAmount("");
      setRawAmount("");
    } catch {
      // Handled by mutation onError toast
    }
  }

  return (
    <Card className="overflow-hidden border border-border/85 bg-card/45 rounded-xl transition-all duration-300 hover:shadow-sm">
      <CardHeader
        className="cursor-pointer select-none p-6 md:p-8"
        onClick={() => setIsExpanded(!isExpanded)}
      >
        <div className="flex flex-col sm:flex-row sm:items-center justify-between gap-4">
          <div className="space-y-1">
            <div className="flex items-center gap-2.5">
              <h3 className="text-lg font-bold tracking-tight text-foreground">{pool.name}</h3>
              <span
                className={cn(
                  "h-2 w-2 rounded-full inline-block",
                  pool.status === "active" && "bg-green-500 ring-2 ring-green-500/20 animate-caret-blink",
                  pool.status === "frozen" && "bg-gray-400",
                )}
                aria-label={pool.status === "active" ? "Active" : "Frozen"}
              />
            </div>
            <div className="flex items-center gap-1.5 text-xs text-muted-foreground font-mono">
              <span>Vault ID:</span>
              <a
                href={`${explorerUrl}/contract/${pool.address}`}
                target="_blank"
                rel="noopener noreferrer"
                onClick={(e) => e.stopPropagation()}
                className="inline-flex items-center gap-1 hover:text-foreground hover:underline decoration-border transition-colors"
              >
                {pool.address.slice(0, 8)}...{pool.address.slice(-6)}
                <ArrowSquareOut className="h-3 w-3 shrink-0" />
              </a>
            </div>

            {/* Mobile stats: rendered below name */}
            <div className="flex items-center gap-6 mt-3 sm:hidden border-t border-border/40 pt-3">
              <div>
                <p className="text-[10px] font-mono uppercase tracking-wider text-muted-foreground">TVL</p>
                <p className="font-bold text-sm text-foreground tabular-nums mt-0.5">
                  {formatCurrency(pool.tvl)}
                </p>
              </div>
              <div>
                <p className="text-[10px] font-mono uppercase tracking-wider text-muted-foreground" title={yieldNote}>Yield</p>
                <p className="font-bold text-sm text-foreground tabular-nums mt-0.5">
                  {formatPercent(pool.estimatedYield)}
                </p>
              </div>
            </div>
          </div>

          <div className="flex items-center gap-8">
            {/* Desktop stats: rendered on the right */}
            <div className="hidden sm:flex items-center gap-10">
              <div className="text-right">
                <p className="text-[10px] font-mono uppercase tracking-widest text-muted-foreground mb-0.5">TVL</p>
                <p className="font-bold text-base text-foreground tabular-nums">{formatCurrency(pool.tvl)}</p>
              </div>
              <div className="text-right">
                <p className="text-[10px] font-mono uppercase tracking-widest text-muted-foreground mb-0.5" title={yieldNote}>Yield</p>
                <p className="font-bold text-base text-foreground tabular-nums">{formatPercent(pool.estimatedYield)}</p>
              </div>
            </div>

            <div className="flex h-8 w-8 items-center justify-center rounded-lg border border-border bg-background hover:bg-muted/10 transition-colors">
              {isExpanded ? (
                <CaretUp className="h-4 w-4 text-muted-foreground" />
              ) : (
                <CaretDown className="h-4 w-4 text-muted-foreground" />
              )}
            </div>
          </div>
        </div>
      </CardHeader>

      <div
        className={cn(
          "grid transition-all duration-300 ease-in-out border-t border-border/80",
          isExpanded
            ? "grid-rows-[1fr] opacity-100"
            : "grid-rows-[0fr] opacity-0 pointer-events-none"
        )}
      >
        <div className="overflow-hidden">
          <CardContent className="bg-muted/10 p-6 md:p-8">
            <div className="grid grid-cols-1 md:grid-cols-5 gap-8 items-start">

            {/* Left columns: Asset Allocation & Contract Authority (3/5 width) */}
            <div className="md:col-span-3 space-y-8">
              {/* Asset Allocation */}
              <div>
                <h4 className="text-[10px] font-mono uppercase tracking-wider text-muted-foreground mb-4">
                  Asset Allocation
                </h4>

                {/* Mobile stacked asset card view */}
                <div className="md:hidden space-y-3">
                  {pool.assets.map((asset) => (
                    <div
                      key={asset.symbol}
                      className="rounded-lg border border-border/60 bg-background/50 p-4 space-y-2.5"
                    >
                      <div className="flex justify-between items-center border-b border-border/40 pb-2">
                        <span className="font-bold text-foreground">
                          {asset.contractAddress ? (
                            <a
                              href={`${explorerUrl}/contract/${asset.contractAddress}`}
                              target="_blank"
                              rel="noopener noreferrer"
                              className="hover:underline hover:text-primary transition-colors"
                            >
                              {asset.symbol}
                            </a>
                          ) : (
                            asset.symbol
                          )}
                        </span>
                        <span className="text-[10px] font-mono text-muted-foreground uppercase">
                          Util:{" "}
                          <span className="text-foreground font-semibold">
                            {asset.supplied > 0
                              ? formatPercent((asset.borrowed / asset.supplied) * 100)
                              : "0.00%"}
                          </span>
                        </span>
                      </div>
                      <div className="grid grid-cols-2 gap-y-1.5 text-xs tabular-nums">
                        <span className="text-muted-foreground">Supplied</span>
                        <span className="text-right text-foreground font-medium">
                          {formatCurrency(asset.supplied)}
                        </span>
                        <span className="text-muted-foreground">Borrowed</span>
                        <span className="text-right text-foreground font-medium">
                          {formatCurrency(asset.borrowed)}
                        </span>
                        <span className="text-muted-foreground">Available Liquidity</span>
                        <span className="text-right text-foreground font-medium">
                          {formatCurrency(asset.supplied - asset.borrowed)}
                        </span>
                      </div>
                    </div>
                  ))}
                </div>

                {/* Desktop tabular asset view */}
                <div className="hidden md:block">
                  <table className="w-full text-sm">
                    <thead>
                      <tr className="border-b border-border/80 pb-2">
                        <th className="text-left pb-3 font-mono text-[10px] uppercase tracking-wider text-muted-foreground">
                          Asset
                        </th>
                        <th className="text-right pb-3 font-mono text-[10px] uppercase tracking-wider text-muted-foreground">
                          Supplied
                        </th>
                        <th className="text-right pb-3 font-mono text-[10px] uppercase tracking-wider text-muted-foreground">
                          Borrowed
                        </th>
                        <th className="text-right pb-3 font-mono text-[10px] uppercase tracking-wider text-muted-foreground">
                          Available
                        </th>
                        <th className="text-right pb-3 font-mono text-[10px] uppercase tracking-wider text-muted-foreground">
                          Utilization
                        </th>
                      </tr>
                    </thead>
                    <tbody className="divide-y divide-border/40">
                      {pool.assets.map((asset) => (
                        <tr
                          key={asset.symbol}
                          className="hover:bg-muted/10 transition-colors"
                        >
                          <td className="py-3.5 font-bold text-foreground">
                            {asset.contractAddress ? (
                              <a
                                href={`${explorerUrl}/contract/${asset.contractAddress}`}
                                target="_blank"
                                rel="noopener noreferrer"
                                className="hover:underline transition-colors"
                              >
                                {asset.symbol}
                              </a>
                            ) : (
                              asset.symbol
                            )}
                          </td>
                          <td className="text-right py-3.5 tabular-nums text-foreground/90">
                            {formatCurrency(asset.supplied)}
                          </td>
                          <td className="text-right py-3.5 tabular-nums text-foreground/90">
                            {formatCurrency(asset.borrowed)}
                          </td>
                          <td className="text-right py-3.5 tabular-nums text-foreground/90">
                            {formatCurrency(asset.supplied - asset.borrowed)}
                          </td>
                          <td className="text-right py-3.5 tabular-nums font-medium text-foreground">
                            {asset.supplied > 0
                              ? formatPercent((asset.borrowed / asset.supplied) * 100)
                              : "0.00%"}
                          </td>
                        </tr>
                      ))}
                    </tbody>
                  </table>
                </div>
              </div>

              {/* Withdrawal queue */}
              <div className="border-t border-border/60 pt-6">
                <h4 className="text-[10px] font-mono uppercase tracking-wider text-muted-foreground mb-3">
                  Withdrawal Queue
                </h4>
                <div className="grid grid-cols-2 gap-y-2 text-xs tabular-nums">
                  <span className="text-muted-foreground">Owed to queued requests</span>
                  <span className="text-right text-foreground font-semibold">
                    {formatNumber(queue.data?.queuedAssets ?? 0, 2)} USDC
                  </span>
                  <span className="text-muted-foreground">Smallest request</span>
                  <span className="text-right text-foreground font-semibold">
                    {formatNumber(queue.data?.minRequest ?? 0, 2)} USDC
                  </span>
                  <span className="text-muted-foreground">Borrow APR</span>
                  <span className="text-right text-foreground font-semibold">
                    {formatPercent(pool.borrowApr)}
                  </span>
                </div>
                <p className="mt-3 text-xs text-muted-foreground leading-relaxed">
                  When the vault&apos;s cash is lent out, withdrawals wait in a queue and are paid oldest first as borrowers repay. A request can be part-paid, and can be cancelled until it is paid.
                </p>
              </div>

              {/* Contract Authority */}
              <div className="border-t border-border/60 pt-6">
                <h4 className="text-[10px] font-mono uppercase tracking-wider text-muted-foreground mb-4">
                  Contract Authority
                </h4>
                <div className="grid grid-cols-1 sm:grid-cols-3 gap-6">
                  {pool.admin && (
                    <div className="space-y-1">
                      <p className="text-[9px] font-mono uppercase tracking-widest text-muted-foreground">Owner</p>
                      <a
                        href={`${explorerUrl}/contract/${pool.admin}`}
                        target="_blank"
                        rel="noopener noreferrer"
                        className="inline-flex items-center gap-1 max-w-full font-mono text-xs text-foreground hover:text-primary hover:underline transition-colors"
                        title={pool.admin}
                      >
                        <span className="truncate">
                          {pool.admin.slice(0, 8)}...{pool.admin.slice(-6)}
                        </span>
                        <ArrowSquareOut className="h-3 w-3 shrink-0 text-muted-foreground" />
                      </a>
                    </div>
                  )}
                  {pool.treasury && (
                    <div className="space-y-1">
                      <p className="text-[9px] font-mono uppercase tracking-widest text-muted-foreground">Treasury</p>
                      <a
                        href={`${explorerUrl}/account/${pool.treasury}`}
                        target="_blank"
                        rel="noopener noreferrer"
                        className="inline-flex items-center gap-1 max-w-full font-mono text-xs text-foreground hover:text-primary hover:underline transition-colors"
                        title={pool.treasury}
                      >
                        <span className="truncate">
                          {pool.treasury.slice(0, 8)}...{pool.treasury.slice(-6)}
                        </span>
                        <ArrowSquareOut className="h-3 w-3 shrink-0 text-muted-foreground" />
                      </a>
                    </div>
                  )}
                  {pool.guardian && (
                    <div className="space-y-1">
                      <p className="text-[9px] font-mono uppercase tracking-widest text-muted-foreground">Guardian</p>
                      <a
                        href={`${explorerUrl}/account/${pool.guardian}`}
                        target="_blank"
                        rel="noopener noreferrer"
                        className="inline-flex items-center gap-1 max-w-full font-mono text-xs text-foreground hover:text-primary hover:underline transition-colors"
                        title={pool.guardian}
                      >
                        <span className="truncate">
                          {pool.guardian.slice(0, 8)}...{pool.guardian.slice(-6)}
                        </span>
                        <ArrowSquareOut className="h-3 w-3 shrink-0 text-muted-foreground" />
                      </a>
                    </div>
                  )}
                </div>
              </div>
            </div>

            {/* Right column: Transaction Panel (2/5 width) */}
            <div className="md:col-span-2 space-y-6 border-t md:border-t-0 md:border-l border-border/70 pt-8 md:pt-0 md:pl-8">
              <div>
                <h4 className="text-[10px] font-mono uppercase tracking-wider text-muted-foreground mb-4">
                  Transaction Control
                </h4>

                {/* Tab toggle */}
                <div className="flex gap-1 rounded-lg bg-muted p-1 mb-5">
                  <button
                    type="button"
                    className={cn(
                      "flex-1 rounded-md py-1.5 text-xs font-semibold uppercase tracking-wider transition-all duration-200 cursor-pointer",
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
                      "flex-1 rounded-md py-1.5 text-xs font-semibold uppercase tracking-wider transition-all duration-200 cursor-pointer",
                      activeTab === "withdraw"
                        ? "bg-background text-foreground shadow-sm"
                        : "text-muted-foreground hover:text-foreground",
                    )}
                    onClick={() => handleTabSwitch("withdraw")}
                  >
                    Withdraw
                  </button>
                </div>

                {/* User account details (when connected) */}
                {isConnected && position.data ? (
                  <div className="rounded-lg border border-border/60 bg-background/55 p-4 space-y-3 mb-5">
                    <p className="text-[9px] font-mono uppercase tracking-widest text-muted-foreground border-b border-border/30 pb-1.5 mb-2">
                      Your Position
                    </p>
                    <div className="grid grid-cols-2 gap-y-2 text-xs tabular-nums">
                      <span className="text-muted-foreground">Wallet Balance</span>
                      <span className="text-right text-foreground font-semibold">
                        {formatNumber(position.data.walletBalance, 4)} USDC
                      </span>

                      <span className="text-muted-foreground">Active Deposit</span>
                      <span className="text-right text-foreground font-semibold">
                        {formatNumber(position.data.assetsValue, 4)} USDC
                      </span>

                      <span className="text-muted-foreground">Shares Held</span>
                      <span className="text-right text-foreground font-semibold">
                        {formatNumber(position.data.shares, 4)} mvUSDC
                      </span>

                      <span className="text-muted-foreground">
                        {activeTab === "deposit" ? "Max Allowed" : "Withdraw Now"}
                      </span>
                      <span className="text-right text-foreground font-semibold">
                        {activeTab === "deposit"
                          ? `${formatNumber(position.data.maxDeposit, 4)} USDC`
                          : `${formatNumber(position.data.maxWithdraw, 4)} USDC`}
                      </span>
                    </div>
                  </div>
                ) : null}

                {isConnected && status.data && (
                  <LenderNotices status={status.data} tab={activeTab} queueBusy={queueBusy} />
                )}

                {/* Amount input field */}
                <div className="space-y-4">
                  <div className="relative">
                    <input
                      type="text"
                      inputMode="decimal"
                      placeholder="0.00 USDC"
                      maxLength={15}
                      value={displayAmount}
                      onChange={(e) => handleAmountChange(e.target.value)}
                      disabled={isPending}
                      className={cn(
                        "w-full rounded-lg border bg-background py-3 pl-4 pr-16 text-sm tabular-nums placeholder:text-muted-foreground focus:outline-none focus:ring-1 focus:ring-foreground focus:border-foreground disabled:opacity-50 transition-all",
                        validationError ? "border-destructive focus:ring-destructive focus:border-destructive" : "border-input"
                      )}
                    />
                    {isConnected && position.data && (
                      <button
                        type="button"
                        className="absolute right-3 top-1/2 -translate-y-1/2 rounded px-2 py-1 text-xs font-mono font-bold text-muted-foreground hover:text-foreground hover:bg-muted/10 transition-colors"
                        onClick={() => {
                          const max =
                            activeTab === "deposit"
                              ? Math.floor(
                                  Math.min(
                                    position.data!.walletBalance,
                                    position.data!.maxDeposit,
                                  ) * 100,
                                ) / 100
                              : Math.floor(
                                  (queueBusy
                                    ? Math.min(position.data!.assetsValue, status.data?.maxPerRequest ?? Infinity)
                                    : position.data!.maxWithdraw) * 100,
                                ) / 100;
                          handleAmountChange(max.toString());
                        }}
                      >
                        MAX
                      </button>
                    )}
                  </div>

                  {/* Immediate validation error notice */}
                  {validationError && (
                    <p className="text-xs text-destructive font-mono mt-1" role="alert">
                      {validationError}
                    </p>
                  )}

                  {/* Dynamic Preview panel */}
                  {(previewLoading || preview) && (
                    <div className="rounded-lg bg-muted/40 border border-border/30 p-4 transition-all duration-200">
                      {previewLoading ? (
                        <div className="flex items-center gap-2 text-xs text-muted-foreground">
                          <CircleNotch className="h-3 w-3 animate-spin text-muted-foreground" />
                          <span>Recalculating conversion...</span>
                        </div>
                      ) : preview ? (
                        <div className="text-xs space-y-1.5 text-muted-foreground">
                          <p className="font-semibold text-foreground uppercase text-[9px] font-mono tracking-wider">
                            {activeTab === "deposit" ? "Estimated Receipt" : "Estimated Settlement"}
                          </p>
                          <p className="font-mono text-xs font-medium text-foreground">{preview}</p>
                        </div>
                      ) : null}
                    </div>
                  )}

                  {/* Submit CTA */}
                  {isConnected ? (
                    <Button
                      className="w-full h-11 font-semibold uppercase tracking-wider text-xs cursor-pointer transition-all duration-200"
                      disabled={!isValidAmount || isPending || !!validationError}
                      onClick={handleSubmit}
                    >
                      {isPending && <CircleNotch className="h-3.5 w-3.5 animate-spin shrink-0 mr-1.5" />}
                      {isPending
                        ? "Confirming Transaction..."
                        : activeTab === "deposit"
                          ? "Deposit Capital"
                          : queueMode
                            ? "Request Withdrawal"
                            : "Withdraw Capital"}
                    </Button>
                  ) : (
                    <Button className="w-full h-11 font-semibold uppercase tracking-wider text-xs cursor-pointer transition-all duration-200" onClick={connect}>
                      Connect Wallet
                    </Button>
                  )}

                  {queueMode && isConnected && (
                    <p className="text-xs text-muted-foreground leading-relaxed">
                      {queueBusy
                        ? `Joins the queue behind ${formatNumber(queue.data?.queuedAssets ?? 0, 2)} USDC already requested.`
                        : "More than the vault can pay right now, so it joins the queue."}{" "}
                      Your shares are held by the vault until the request is paid; you can cancel any time before then.
                    </p>
                  )}
                </div>

                {isConnected && requests.data && requests.data.length > 0 && (
                  <div className="mt-6 rounded-lg border border-border/60 bg-background/55 p-4">
                    <p className="text-[9px] font-mono uppercase tracking-widest text-muted-foreground border-b border-border/30 pb-1.5 mb-3">
                      Your Withdrawal Requests
                    </p>
                    <ul className="space-y-3">
                      {requests.data.map((r) => (
                        <li key={r.id.toString()} className="flex items-start justify-between gap-3 text-xs">
                          <div className="space-y-0.5 tabular-nums">
                            <p className="font-semibold text-foreground">
                              {formatNumber(r.value, 2)} USDC
                              <span className="ml-2 font-mono font-normal text-muted-foreground">#{r.id.toString()}</span>
                            </p>
                            <p className="text-muted-foreground">
                              {r.position === 1
                                ? "Next to be paid"
                                : `Place ${r.position} · ${formatNumber(r.aheadValue, 2)} USDC ahead`}
                            </p>
                            <p className="text-muted-foreground">Requested {formatWhen(r.requestedAt)}</p>
                          </div>
                          <Button
                            variant="outline"
                            size="sm"
                            className="shrink-0 text-xs cursor-pointer"
                            disabled={cancelRedeem.isPending}
                            onClick={() => cancelRedeem.mutate(r.id)}
                          >
                            {cancelRedeem.isPending && cancelRedeem.variables === r.id ? (
                              <CircleNotch className="h-3 w-3 animate-spin" />
                            ) : (
                              "Cancel"
                            )}
                          </Button>
                        </li>
                      ))}
                    </ul>
                  </div>
                )}
              </div>
            </div>

          </div>
        </CardContent>
      </div>
    </div>
  </Card>
  );
}

function formatWhen(d: Date): string {
  return d.toLocaleString(undefined, { dateStyle: "medium", timeStyle: "short" });
}

function formatDuration(seconds: number): string {
  if (seconds >= 86_400) return `${Math.ceil(seconds / 86_400)} days`;
  if (seconds >= 3_600) return `${Math.ceil(seconds / 3_600)} hours`;
  return `${Math.max(1, Math.ceil(seconds / 60))} minutes`;
}

/**
 * Plain notes on what the vault allows this address to do on the open tab.
 * @param props.status - The address's allowlist, freeze, exit and lock state
 * @param props.tab - The tab the notes are for
 * @param props.queueBusy - Whether other withdrawals are already queued
 */
function LenderNotices({
  status,
  tab,
  queueBusy,
}: {
  status: LenderStatus;
  tab: ActiveTab;
  queueBusy: boolean;
}) {
  const notes: { tone: "warn" | "info"; text: string }[] = [];
  if (status.frozen) {
    notes.push({
      tone: "warn",
      text: "This address is frozen: deposits, withdrawals and transfers of its shares are blocked.",
    });
  } else if (status.exitDeadline) {
    notes.push({
      tone: "warn",
      text: `This address is no longer allowed to deposit. It can withdraw until ${formatWhen(status.exitDeadline)}, after which the position freezes.`,
    });
  } else if (tab === "deposit" && status.enforced && !status.allowed) {
    notes.push({
      tone: "warn",
      text: "This address is not on the vault's allowlist yet, so a deposit would be refused. Complete verification with Microvault first.",
    });
  }
  if (tab === "withdraw" && !status.frozen && status.lockRemaining > 0) {
    notes.push({
      tone: "info",
      text: `Your latest deposit is locked for about ${formatDuration(status.lockRemaining)} more; withdrawals and requests open after that.`,
    });
  }
  if (tab === "withdraw" && queueBusy && !status.frozen) {
    notes.push({
      tone: "info",
      text: "Other withdrawals are waiting, so new withdrawals join the queue.",
    });
  }
  if (notes.length === 0) return null;
  return (
    <div className="mb-5 space-y-2" role="status">
      {notes.map((n) => (
        <p
          key={n.text}
          className={cn(
            "rounded-lg border px-3 py-2 text-xs leading-relaxed",
            n.tone === "warn"
              ? "border-destructive/40 bg-destructive/5 text-foreground"
              : "border-border/60 bg-muted/30 text-muted-foreground",
          )}
        >
          {n.text}
        </p>
      ))}
    </div>
  );
}
