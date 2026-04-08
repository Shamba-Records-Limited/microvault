/**
 * App-wide constants: scaling factors, default endpoint URLs, and pagination
 * sizes. Endpoint defaults are read from `import.meta.env` at build time.
 * @module lib/constants
 */

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

// We're still in the testnet phase, so don't branch on Vite's MODE — a prod
// build (`vite build`) would otherwise silently flip every default to mainnet
// even when the app is meant to talk to testnet. Read each value from the env
// at build time and fall back to the testnet endpoint.
//
// /** Testnet when Vite mode is not "production". */
// const isTestnet = import.meta.env.MODE !== "production";
//
// export const DEFAULT_RPC_URL = isTestnet
//   ? "https://soroban-testnet.stellar.org"
//   : "https://soroban-rpc.mainnet.stellar.gateway.fm";
//
// export const DEFAULT_NETWORK_PASSPHRASE = isTestnet
//   ? "Test SDF Network ; September 2015"
//   : "Public Global Stellar Network ; September 2015";
//
// export const DEFAULT_EXPLORER_URL = isTestnet
//   ? "https://stellar.expert/explorer/testnet"
//   : "https://stellar.expert/explorer/public";
//
// export const DEFAULT_HORIZON_URL = isTestnet
//   ? "https://horizon-testnet.stellar.org"
//   : "https://horizon.stellar.org";

/** Soroban RPC endpoint. Falls back to public testnet RPC. */
export const DEFAULT_RPC_URL =
  import.meta.env.VITE_RPC_URL || "https://soroban-testnet.stellar.org";

/** Stellar network passphrase used to sign transactions. Defaults to testnet. */
export const DEFAULT_NETWORK_PASSPHRASE =
  import.meta.env.VITE_NETWORK_PASSPHRASE ||
  "Test SDF Network ; September 2015";

/** Stellar Expert explorer base URL used to build outbound deep links. */
export const DEFAULT_EXPLORER_URL =
  import.meta.env.VITE_STELLAR_EXPLORER_URL ||
  "https://stellar.expert/explorer/testnet";

/** Horizon REST endpoint used for treasury operation history and SSE streams. */
export const DEFAULT_HORIZON_URL =
  import.meta.env.VITE_HORIZON_URL || "https://horizon-testnet.stellar.org";

/** Number of events per page when fetching contract events. */
export const EVENTS_PAGE_SIZE = 20;
