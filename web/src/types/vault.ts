/**
 * Domain types for the vault: on-chain stats, metadata, governance addresses,
 * and the aggregated `Pool` shape consumed by the UI.
 * @module types/vault
 */

/**
 * Numeric metrics returned by the vault contract view functions.
 * @remarks All amount fields are unscaled JS numbers (already converted from
 * the contract's i128 stroops). Percentages are 0–100, not 0–1.
 */
export interface VaultStats {
  /** Total value locked (liquidity + borrowed), scaled from USDC. */
  totalManagedAssets: number;
  /** Outstanding borrow amount, scaled from USDC. */
  totalBorrowed: number;
  /** Available liquidity for new borrows, scaled from USDC. */
  availableLiquidity: number;
  /** Utilization rate as a percentage (0–100). */
  utilizationRate: number;
  /** Borrow APR as a percentage. */
  borrowApr: number;
  /** Whether the vault is currently paused (frozen). */
  isPaused: boolean;
}

/** On-chain vault name and asset symbol from the `name()` and `symbol()` view functions. */
export interface VaultMetadata {
  /** Vault name with "Pool" suffix (e.g. "Microvault USDC Pool"). */
  name: string;
  /** Underlying asset symbol (e.g. "USDC"). */
  symbol: string;
}

/**
 * Governance addresses associated with the vault contract.
 * @remarks Any field can be `null` if its on-chain view traps — most commonly
 * after an OZ storage-layout migration that strands the entry. Consumers must
 * handle nulls gracefully so the pool card still renders.
 */
export interface VaultAddresses {
  contractId: string;
  /** Governance/owner contract address. Null when the Ownable trait is absent. */
  owner: string | null;
  /** Treasury address. Null when the on-chain view traps (e.g. post-migration). */
  treasury: string | null;
  /** Guardian address. Null when not configured. */
  guardian: string | null;
}

/**
 * Aggregated pool data assembled from vault stats, metadata, and addresses.
 * @remarks This is the UI-facing shape; transforms happen in `routes/index.tsx`.
 */
export interface Pool {
  id: string;
  name: string;
  /** Soroban vault contract address. */
  address: string;
  tvl: number;
  apy: number;
  totalSupplied: number;
  totalBorrowed: number;
  assets: PoolAsset[];
  admin: string | null;
  treasury: string | null;
  guardian: string | null;
  status: "active" | "frozen";
}

/** Individual asset within a lending pool. */
export interface PoolAsset {
  symbol: string;
  contractAddress?: string;
  supplied: number;
  borrowed: number;
}
