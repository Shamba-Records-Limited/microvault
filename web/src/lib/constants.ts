/** Soroban WAD precision (1e18) used for rates like utilization and APR. */
export const WAD_SCALE = 1_000_000_000_000_000_000n;

/** Number of decimal places for USDC on Stellar. */
export const USDC_DECIMALS = 7;

/** USDC scaling factor (10^7) for converting on-chain i128 amounts to human-readable values. */
export const USDC_SCALE = 10_000_000n;

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
