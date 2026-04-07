/** Numeric metrics returned by the vault contract view functions. */
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

/** Governance addresses associated with the vault contract. */
export interface VaultAddresses {
  contractId: string;
  /** Governance/owner contract address, null if Ownable trait is absent. */
  owner: string | null;
  /** Treasury address, null if the on-chain view traps (e.g. post-migration). */
  treasury: string | null;
  /** Guardian address, null if not configured. */
  guardian: string | null;
}

/** Aggregated pool data constructed from vault stats and addresses. */
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
