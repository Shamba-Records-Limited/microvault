/** Soroban WAD precision (1e18) used for rates like utilization and APR. */
export const WAD_SCALE = 1_000_000_000_000_000_000n;

/** Number of decimal places for USDC on Stellar. */
export const USDC_DECIMALS = 7;

/** USDC scaling factor (10^7) for converting on-chain i128 amounts to human-readable values. */
export const USDC_SCALE = 10_000_000n;

/** Vault share token decimals (USDC 7 decimals + 6 virtual offset). */
export const SHARE_DECIMALS = 13;

/** Share token scaling factor (10^13) for converting on-chain share amounts to human-readable values. */
export const SHARE_SCALE = 10_000_000_000_000n;

/** Testnet when Vite mode is not "production". */
const isTestnet = import.meta.env.MODE !== "production";

export const DEFAULT_RPC_URL = isTestnet
  ? "https://soroban-testnet.stellar.org"
  : "https://soroban-rpc.mainnet.stellar.gateway.fm";

export const DEFAULT_NETWORK_PASSPHRASE = isTestnet
  ? "Test SDF Network ; September 2015"
  : "Public Global Stellar Network ; September 2015";

export const DEFAULT_EXPLORER_URL = isTestnet
  ? "https://stellar.expert/explorer/testnet"
  : "https://stellar.expert/explorer/public";

export const DEFAULT_HORIZON_URL = isTestnet
  ? "https://horizon-testnet.stellar.org"
  : "https://horizon.stellar.org";

/** Number of events per page when fetching contract events. */
export const EVENTS_PAGE_SIZE = 20;

/** Stellar Expert API base URL for contract event queries. */
// We're still in the testnet phase, so don't branch on isTestnet — always use
// the URL from the environment (falling back to the testnet endpoint).
// export const STELLAR_EXPERT_API_URL = isTestnet
//   ? "https://api.stellar.expert/explorer/testnet"
//   : "https://api.stellar.expert/explorer/public";
export const STELLAR_EXPERT_API_URL =
  import.meta.env.VITE_STELLAR_EXPERT_API_URL ||
  "https://api.stellar.expert/explorer/testnet";
