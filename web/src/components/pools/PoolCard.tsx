import { useState } from "react";
import { Card, CardContent, CardHeader } from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import { ChevronDown, ChevronUp, Coins, Users, Landmark, Shield } from "lucide-react";
import { cn } from "@/lib/utils";
import { formatCurrency, formatPercent } from "@/lib/format";
import { DEFAULT_EXPLORER_URL } from "@/lib/constants";
import type { Pool } from "@/types/vault";

const explorerUrl = import.meta.env.VITE_STELLAR_EXPLORER_URL || DEFAULT_EXPLORER_URL;

interface PoolCardProps {
  pool: Pool;
}

export function PoolCard({ pool }: PoolCardProps) {
  const [isExpanded, setIsExpanded] = useState(false);

  return (
    <Card className="overflow-hidden transition-all duration-200 hover:shadow-md">
      <CardHeader
        className="cursor-pointer"
        onClick={() => setIsExpanded(!isExpanded)}
      >
        <div className="flex items-center justify-between">
          <div className="flex items-center gap-3">
            <div className="h-10 w-10 rounded-lg bg-primary/10 flex items-center justify-center">
              <Coins className="h-5 w-5 text-primary" />
            </div>
            <div>
              <h3 className="font-semibold text-base">{pool.name}</h3>
              <a
                href={`${explorerUrl}/contract/${pool.address}`}
                target="_blank"
                rel="noopener noreferrer"
                onClick={(e) => e.stopPropagation()}
                className="text-xs text-muted-foreground font-mono hover:text-foreground transition-colors"
              >
                {pool.address.slice(0, 8)}...{pool.address.slice(-6)}
              </a>
            </div>
          </div>

          <div className="flex items-center gap-6">
            <Badge
              variant={pool.status === "active" ? "default" : "secondary"}
              className={cn(
                pool.status === "active" &&
                  "bg-primary/10 text-primary hover:bg-primary/20",
                pool.status === "frozen" &&
                  "bg-destructive/10 text-destructive hover:bg-destructive/20",
              )}
            >
              {pool.status}
            </Badge>

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
              <div className="overflow-x-auto">
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
          </div>
        </CardContent>
      )}
    </Card>
  );
}
