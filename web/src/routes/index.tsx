import { PoolCard } from "@/components/pools/PoolCard";
import { PoolStats } from "@/components/pools/PoolStats";
import { Skeleton } from "@/components/ui/skeleton";
import { useVaultStats } from "@/hooks/use-vault-stats";
import { useVaultAddresses } from "@/hooks/use-vault-addresses";
import { useVaultMetadata } from "@/hooks/use-vault-metadata";
import type { Pool } from "@/types/vault";

export default function IndexPage() {
  const { data: stats, isLoading: statsLoading, error: statsError } = useVaultStats();
  const { data: addresses, isLoading: addressesLoading } = useVaultAddresses();
  const { data: metadata, isLoading: metadataLoading } = useVaultMetadata();

  const isLoading = statsLoading || addressesLoading || metadataLoading;
  const pool: Pool | null =
    stats && addresses && metadata
      ? {
          id: "1",
          name: metadata.name,
          address: addresses.contractId,
          tvl: stats.totalManagedAssets,
          apy: stats.borrowApr,
          totalSupplied: stats.totalManagedAssets,
          totalBorrowed: stats.totalBorrowed,
          assets: [
            {
              symbol: metadata.symbol,
              supplied: stats.totalManagedAssets,
              borrowed: stats.totalBorrowed,
            },
          ],
          admin: addresses.owner,
          treasury: addresses.treasury,
          guardian: addresses.guardian,
          status: stats.isPaused ? "frozen" : "active",
        }
      : null;

  return (
    <main className="container py-8">
      {/* Hero Section */}
      <section className="mb-12">
        <h1 className="text-3xl md:text-4xl font-bold mb-3">
          Stellar SEP-56 Pools
        </h1>
        <p className="text-muted-foreground text-lg max-w-2xl">
          Explore microvault lending pools on the Stellar Network.
        </p>
      </section>

      {/* Protocol Stats */}
      <section className="mb-8">
        {isLoading ? (
          <div className="grid grid-cols-1 md:grid-cols-3 gap-4">
            {Array.from({ length: 3 }).map((_, i) => (
              <Skeleton key={i} className="h-28 rounded-xl" />
            ))}
          </div>
        ) : stats ? (
          <PoolStats
            totalTvl={stats.totalManagedAssets}
            poolCount={1}
            totalBorrowed={stats.totalBorrowed}
          />
        ) : null}
      </section>

      {/* Error State */}
      {statsError && (
        <div className="rounded-lg border border-destructive/50 bg-destructive/5 p-6 mb-8">
          <h3 className="font-semibold text-destructive mb-2">
            Failed to load vault data
          </h3>
          <p className="text-sm text-muted-foreground">
            {statsError.message}
          </p>
          <p className="text-sm text-muted-foreground mt-2">
            Check that VITE_VAULT_CONTRACT_ID is set to a valid Soroban contract
            address and the RPC endpoint is reachable.
          </p>
        </div>
      )}

      {/* Pools List */}
      <section>
        <div className="flex items-center justify-between mb-4">
          <h2 className="text-xl font-semibold">Pools</h2>
          <span className="text-sm text-muted-foreground">
            {pool ? "1 pool" : "—"}
          </span>
        </div>
        <div className="grid gap-4">
          {isLoading ? (
            <Skeleton className="h-24 rounded-xl" />
          ) : pool ? (
            <PoolCard pool={pool} />
          ) : null}
        </div>
      </section>
    </main>
  );
}
