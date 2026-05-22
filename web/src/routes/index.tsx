/**
 * Landing page: hero, protocol-level stats, and the list of vault pools.
 * @module routes/index
 */
import { PoolCard } from "@/components/pools/PoolCard";
import { PoolStats } from "@/components/pools/PoolStats";
import { Skeleton } from "@/components/ui/skeleton";
import { Button } from "@/components/ui/button";
import { useVaultStats } from "@/hooks/use-vault-stats";
import { useVaultAddresses } from "@/hooks/use-vault-addresses";
import { useVaultMetadata } from "@/hooks/use-vault-metadata";
import type { Pool } from "@/types/vault";

/**
 * Assembles a single `Pool` view model from three independent vault queries
 * (stats, addresses, metadata) and renders the home dashboard.
 * @remarks Each query's error is surfaced individually via the combined
 * `loadError` banner so the user can tell which slice of the contract state
 * is actually failing — important after upgrades that can strand just one
 * OZ storage entry while leaving everything else healthy.
 */
export default function IndexPage() {
  const {
    data: stats,
    isLoading: statsLoading,
    error: statsError,
    refetch: refetchStats,
  } = useVaultStats();
  const {
    data: addresses,
    isLoading: addressesLoading,
    error: addressesError,
    refetch: refetchAddresses,
  } = useVaultAddresses();
  const {
    data: metadata,
    isLoading: metadataLoading,
    error: metadataError,
    refetch: refetchMetadata,
  } = useVaultMetadata();

  const loadError = statsError ?? addressesError ?? metadataError;

  const handleRetry = () => {
    if (statsError) refetchStats();
    if (addressesError) refetchAddresses();
    if (metadataError) refetchMetadata();
  };

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
    <main className="container max-w-4xl py-12">
      {/* Integrated Hero & Stats Header */}
      <section className="grid grid-cols-1 md:grid-cols-5 gap-8 mb-16 items-start">
        <div className="md:col-span-3 space-y-4">
          <p className="text-[10px] font-mono uppercase tracking-[0.2em] font-bold text-muted-foreground">
            Lending Infrastructure
          </p>
          <h1 className="text-3xl md:text-4xl font-extrabold leading-tight tracking-tighter text-foreground">
            Stellar SEP-56 Pools
          </h1>
          <p className="text-muted-foreground text-base md:text-lg leading-relaxed max-w-prose">
            Explore and interact with decentralized lending pools built on Stellar&apos;s Soroban smart contract platform. Securely powering last-mile agricultural credit with transparent, audited capital pools.
          </p>
        </div>

        <div className="md:col-span-2 border-t md:border-t-0 md:border-l border-border pt-6 md:pt-0 md:pl-8 space-y-5">
          <p className="text-xs font-mono uppercase tracking-widest text-muted-foreground">
            Protocol Status
          </p>
          {isLoading ? (
            <div className="space-y-4" role="status" aria-live="polite" aria-label="Loading protocol stats...">
              <Skeleton className="h-10 w-32" />
              <Skeleton className="h-10 w-24" />
              <Skeleton className="h-10 w-28" />
            </div>
          ) : stats ? (
            <PoolStats
              totalTvl={stats.totalManagedAssets}
              poolCount={1}
              totalBorrowed={stats.totalBorrowed}
            />
          ) : null}
        </div>
      </section>

      {/* Error State */}
      {loadError && (
        <div className="rounded-lg border border-destructive/50 bg-destructive/5 p-6 mb-12">
          <h3 className="font-semibold text-destructive mb-2">
            Failed to load vault data
            {statsError
              ? " (stats)"
              : addressesError
                ? " (addresses)"
                : " (metadata)"}
          </h3>
          <p className="text-sm text-muted-foreground">{loadError.message}</p>
          <p className="text-sm text-muted-foreground mt-2">
            Check that VITE_VAULT_CONTRACT_ID is set to a valid Soroban contract
            address and the RPC endpoint is reachable.
          </p>
          <Button
            variant="outline"
            size="sm"
            onClick={handleRetry}
            className="mt-4 border-destructive/30 hover:border-destructive/60 hover:bg-destructive/10 text-destructive text-xs font-mono uppercase tracking-wider cursor-pointer"
          >
            Retry Connection
          </Button>
        </div>
      )}

      {/* Pools List */}
      <section className="border-t border-border pt-12">
        <div className="flex items-center justify-between mb-6">
          <h2 className="text-xs font-mono uppercase tracking-widest text-muted-foreground">Active Lending Pools</h2>
        </div>
        <div className="grid gap-4">
          {isLoading ? (
            <div role="status" aria-live="polite" aria-label="Loading pool records...">
              <Skeleton className="h-24 rounded-xl" />
            </div>
          ) : pool ? (
            <PoolCard pool={pool} />
          ) : null}
        </div>
      </section>
    </main>
  );
}
