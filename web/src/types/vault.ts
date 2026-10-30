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

/**
 * The vault's withdrawal queue as a whole.
 * @remarks `length` counts ids from head to tail, cancelled holes included,
 * so it is an upper bound on open requests.
 */
export interface QueueState {
  head: bigint;
  length: bigint;
  /** USDC currently owed to queued requests, in major units. */
  queuedAssets: number;
  /** Smallest request the vault accepts, in USDC major units. */
  minRequest: number;
}

/** One of the connected lender's open withdrawal requests. */
export interface RedeemRequest {
  id: bigint;
  /** Shares still escrowed (raw i128). */
  shares: bigint;
  /** Their current USDC value, in major units. */
  value: number;
  /** When the request was made. */
  requestedAt: Date;
  /** 1-based place among open requests; 1 is paid next. */
  position: number;
  /** USDC owed to open requests ahead of this one, in major units. */
  aheadValue: number;
}

/**
 * What the vault lets the connected address do right now.
 * @remarks `exitDeadline` is set once an address is revoked: it can still
 * withdraw until then, after which it is frozen.
 */
export interface LenderStatus {
  allowed: boolean;
  enforced: boolean;
  frozen: boolean;
  exitDeadline: Date | null;
  /** Seconds until deposited shares unlock; 0 when unlocked. */
  lockRemaining: number;
  /** Share balance, the most a queue request can escrow (raw i128). */
  shareBalance: bigint;
  /** Largest single withdrawal or request the vault accepts, in USDC. */
  maxPerRequest: number;
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
  /** Estimated annual lender yield: the borrow rate compounded per second, times utilisation, as a percentage. */
  estimatedYield: number;
  /** Rate borrowers pay, as a percentage. */
  borrowApr: number;
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
