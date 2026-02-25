import { Card, CardContent } from "@/components/ui/card";
import { TrendingUp, Coins, Activity } from "lucide-react";
import { formatCurrency } from "@/lib/format";

interface PoolStatsProps {
  totalTvl: number;
  poolCount: number;
  totalBorrowed: number;
}

/** Protocol-level stats cards showing TVL, pool count, and total borrowed. */
export function PoolStats({ totalTvl, poolCount, totalBorrowed }: PoolStatsProps) {
  const stats = [
    {
      label: "Total Value Locked",
      value: formatCurrency(totalTvl),
      icon: TrendingUp,
      description: "Across all pools",
    },
    {
      label: "Pools",
      value: poolCount.toString(),
      icon: Activity,
      description: "Deployed vaults",
    },
    {
      label: "Total Borrowed",
      value: formatCurrency(totalBorrowed),
      icon: Coins,
      description: "Outstanding loans",
    },
  ];

  return (
    <div className="grid grid-cols-1 md:grid-cols-3 gap-4">
      {stats.map((stat) => (
        <Card key={stat.label}>
          <CardContent className="pt-6">
            <div className="flex items-start justify-between">
              <div>
                <p className="text-sm text-muted-foreground">{stat.label}</p>
                <p className="text-2xl font-bold mt-1">{stat.value}</p>
                <p className="text-xs text-muted-foreground mt-1">
                  {stat.description}
                </p>
              </div>
              <div className="h-10 w-10 rounded-lg bg-primary/10 flex items-center justify-center">
                <stat.icon className="h-5 w-5 text-primary" />
              </div>
            </div>
          </CardContent>
        </Card>
      ))}
    </div>
  );
}
