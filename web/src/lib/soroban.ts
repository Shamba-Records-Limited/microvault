import {
  Contract,
  TransactionBuilder,
  Account,
  scValToNative,
  type xdr,
  rpc,
} from "@stellar/stellar-sdk";
import {
  WAD_SCALE,
  USDC_SCALE,
  DEFAULT_RPC_URL,
  DEFAULT_NETWORK_PASSPHRASE,
} from "./constants";
import type { VaultStats, VaultAddresses, VaultMetadata } from "@/types/vault";

const rpcUrl = import.meta.env.VITE_SOROBAN_RPC_URL || DEFAULT_RPC_URL;
const networkPassphrase =
  import.meta.env.VITE_NETWORK_PASSPHRASE || DEFAULT_NETWORK_PASSPHRASE;
const contractId = import.meta.env.VITE_VAULT_CONTRACT_ID;

const server = new rpc.Server(rpcUrl);
const contract = new Contract(contractId);

/** Zero-balance account used as the source for simulated (read-only) transactions. */
const SOURCE_ACCOUNT = new Account(
  "GAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAWHF",
  "0",
);

/**
 * Invoke a Soroban contract view function via `simulateTransaction`.
 * No signing or fees are required — the call is read-only.
 */
async function callViewFunction(
  functionName: string,
  args: xdr.ScVal[] = [],
): Promise<xdr.ScVal> {
  const tx = new TransactionBuilder(SOURCE_ACCOUNT, {
    fee: "100",
    networkPassphrase,
  })
    .addOperation(contract.call(functionName, ...args))
    .setTimeout(30)
    .build();

  const simResponse = await server.simulateTransaction(tx);

  if (rpc.Api.isSimulationError(simResponse)) {
    throw new Error(
      `Simulation error for ${functionName}: ${simResponse.error}`,
    );
  }

  if (!rpc.Api.isSimulationSuccess(simResponse)) {
    throw new Error(`Unexpected simulation response for ${functionName}`);
  }

  const retval = simResponse.result?.retval;
  if (!retval) {
    throw new Error(`No return value from ${functionName}`);
  }

  return retval;
}

/** Convert an on-chain i128 ScVal to a JS number, dividing by the given scale. */
function i128ToNumber(scVal: xdr.ScVal, scale: bigint): number {
  const raw = scValToNative(scVal) as bigint;
  return Number(raw) / Number(scale);
}

/** Convert a WAD-scaled (1e18) ScVal to a human-readable percentage. */
function wadToPercent(scVal: xdr.ScVal): number {
  const raw = scValToNative(scVal) as bigint;
  return (Number(raw) / Number(WAD_SCALE)) * 100;
}

/** Fetch all numeric vault metrics (Name, TVL, borrowed, utilization, APR, paused) in a single batch. */
export async function fetchVaultStats(): Promise<VaultStats> {
  const [
    totalManaged,
    totalBorrowed,
    availableLiquidity,
    utilizationRate,
    borrowApr,
    paused,
  ] = await Promise.all([
    callViewFunction("total_managed_assets"),
    callViewFunction("total_borrowed"),
    callViewFunction("available_liquidity"),
    callViewFunction("utilization_rate"),
    callViewFunction("borrow_apr"),
    callViewFunction("paused"),
  ]);

  return {
    totalManagedAssets: i128ToNumber(totalManaged, USDC_SCALE),
    totalBorrowed: i128ToNumber(totalBorrowed, USDC_SCALE),
    availableLiquidity: i128ToNumber(availableLiquidity, USDC_SCALE),
    utilizationRate: wadToPercent(utilizationRate),
    borrowApr: wadToPercent(borrowApr),
    isPaused: scValToNative(paused) as boolean,
  };
}

/** Fetch vault name and asset symbol from on-chain view functions. */
export async function fetchVaultMetadata(): Promise<VaultMetadata> {
  const [name, symbol] = await Promise.all([
    callViewFunction("name"),
    callViewFunction("symbol"),
  ]);

  const vaultName = scValToNative(name) as string;
  const assetSymbol = scValToNative(symbol) as string;

  return {
    name: `${vaultName} Pool`,
    symbol: assetSymbol,
  };
}

/** Fetch vault governance addresses (owner, treasury, guardian). Guardian/owner may be null. */
export async function fetchVaultAddresses(): Promise<VaultAddresses> {
  const [treasury, owner, guardian] = await Promise.all([
    callViewFunction("treasury"),
    callViewFunction("get_owner").catch(() => null),
    callViewFunction("guardian").catch(() => null),
  ]);

  return {
    contractId,
    treasury: scValToNative(treasury) as string,
    owner: owner ? (scValToNative(owner) as string | null) : null,
    guardian: guardian ? (scValToNative(guardian) as string | null) : null,
  };
}
