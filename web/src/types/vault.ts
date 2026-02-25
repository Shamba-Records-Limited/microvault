export interface VaultStats {
  totalManagedAssets: number;
  totalBorrowed: number;
  availableLiquidity: number;
  utilizationRate: number;
  borrowApr: number;
  isPaused: boolean;
}

export interface VaultAddresses {
  contractId: string;
  owner: string | null;
  treasury: string;
  guardian: string | null;
}

export interface Pool {
  id: string;
  name: string;
  address: string;
  tvl: number;
  apy: number;
  totalSupplied: number;
  totalBorrowed: number;
  assets: PoolAsset[];
  admin: string | null;
  treasury: string;
  guardian: string | null;
  status: "active" | "frozen";
}

export interface PoolAsset {
  symbol: string;
  contractAddress?: string;
  supplied: number;
  borrowed: number;
}
