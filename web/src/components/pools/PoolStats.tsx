/**
 * Protocol-level summary metrics.
 * @module components/pools/PoolStats
 */
import { formatCurrency } from "@/lib/format";

interface PoolStatsProps {
  totalTvl: number;
  poolCount: number;
  totalBorrowed: number;
}

/**
 * Renders the three protocol-level metrics in a clean, card-less layout with typography and borders.
 * @param props.totalTvl - Total value locked across all pools, in major units
 * @param props.poolCount - Number of deployed vaults
 * @param props.totalBorrowed - Outstanding loan principal across all pools
 */
export function PoolStats({ totalTvl, poolCount, totalBorrowed }: PoolStatsProps) {
  return (
    <div className="space-y-4 pt-1">
      <div className="group">
        <p className="text-[10px] font-mono uppercase tracking-widest text-muted-foreground mb-1">
          Total Value Locked
        </p>
        <p className="text-2xl font-bold tracking-tight tabular-nums text-foreground">
          {formatCurrency(totalTvl)}
        </p>
      </div>

      <div className="border-t border-border/50 pt-3">
        <p className="text-[10px] font-mono uppercase tracking-widest text-muted-foreground mb-1">
          Total Borrowed
        </p>
        <p className="text-2xl font-bold tracking-tight tabular-nums text-foreground">
          {formatCurrency(totalBorrowed)}
        </p>
      </div>

      <div className="border-t border-border/50 pt-3">
        <p className="text-[10px] font-mono uppercase tracking-widest text-muted-foreground mb-1">
          Active Vaults
        </p>
        <p className="text-2xl font-bold tracking-tight tabular-nums text-foreground">
          {poolCount.toString()}
        </p>
      </div>
    </div>
  );
}
