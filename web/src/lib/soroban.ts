import {
  Contract,
  TransactionBuilder,
  Account,
  scValToNative,
  nativeToScVal,
  Address,
  type xdr,
  type Transaction,
  rpc,
} from "@stellar/stellar-sdk";
import {
  WAD_SCALE,
  USDC_SCALE,
  SHARE_SCALE,
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

/**
 * Fetch all numeric vault metrics (TVL, borrowed, utilization, APR, paused) in a single batch.
 *
 * Each view is tolerated to fail individually so a single trapping function
 * (e.g. after an OZ storage-layout migration that strands one entry) cannot
 * blank the entire pool card. Failing fields fall back to 0 / false. See
 * docs/soroban-contract-upgrade-procedure.md for the failure mode this guards
 * against.
 */
export async function fetchVaultStats(): Promise<VaultStats> {
  const [
    totalManaged,
    totalBorrowed,
    availableLiquidity,
    utilizationRate,
    borrowApr,
    paused,
  ] = await Promise.all([
    callViewFunction("total_managed_assets").catch(() => null),
    callViewFunction("total_borrowed").catch(() => null),
    callViewFunction("available_liquidity").catch(() => null),
    callViewFunction("utilization_rate").catch(() => null),
    callViewFunction("borrow_apr").catch(() => null),
    callViewFunction("paused").catch(() => null),
  ]);

  return {
    totalManagedAssets: totalManaged
      ? i128ToNumber(totalManaged, USDC_SCALE)
      : 0,
    totalBorrowed: totalBorrowed ? i128ToNumber(totalBorrowed, USDC_SCALE) : 0,
    availableLiquidity: availableLiquidity
      ? i128ToNumber(availableLiquidity, USDC_SCALE)
      : 0,
    utilizationRate: utilizationRate ? wadToPercent(utilizationRate) : 0,
    borrowApr: borrowApr ? wadToPercent(borrowApr) : 0,
    isPaused: paused ? (scValToNative(paused) as boolean) : false,
  };
}

/**
 * Fetch vault name and asset symbol from on-chain view functions.
 *
 * Both calls are tolerated to fail individually: a contract upgraded across
 * an OZ storage-layout change can trap with `UnsetMetadata` (error 105) on
 * `name`/`symbol` even though every other view still works. We fall back to
 * a generic label so the pool card renders instead of vanishing.
 */
export async function fetchVaultMetadata(): Promise<VaultMetadata> {
  const [name, symbol] = await Promise.all([
    callViewFunction("name").catch(() => null),
    callViewFunction("symbol").catch(() => null),
  ]);

  const vaultName = name ? (scValToNative(name) as string) : "Stellar";
  const assetSymbol = symbol ? (scValToNative(symbol) as string) : "USDC";

  return {
    name: `${vaultName} Pool`,
    symbol: assetSymbol,
  };
}

/**
 * Fetch vault governance addresses (treasury, owner, guardian).
 *
 * All three calls are tolerated to fail individually. Any field that traps —
 * the most likely cause being an OZ storage-layout migration that stranded
 * the entry — falls back to `null` so the pool card still renders. See
 * docs/soroban-contract-upgrade-procedure.md.
 */
export async function fetchVaultAddresses(): Promise<VaultAddresses> {
  const [treasury, owner, guardian] = await Promise.all([
    callViewFunction("treasury").catch(() => null),
    callViewFunction("get_owner").catch(() => null),
    callViewFunction("guardian").catch(() => null),
  ]);

  return {
    contractId,
    treasury: treasury ? (scValToNative(treasury) as string) : null,
    owner: owner ? (scValToNative(owner) as string | null) : null,
    guardian: guardian ? (scValToNative(guardian) as string | null) : null,
  };
}

// ---------------------------------------------------------------------------
// Transaction builders (deposit / withdraw)
// ---------------------------------------------------------------------------

/** Build and simulate a contract call transaction using the user's real account. */
async function buildContractTx(
  senderAddress: string,
  functionName: string,
  args: xdr.ScVal[],
): Promise<Transaction> {
  const account = await server.getAccount(senderAddress);
  const tx = new TransactionBuilder(account, {
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

  return rpc
    .assembleTransaction(tx, simResponse)
    .build() as unknown as Transaction;
}

function addressScVal(address: string): xdr.ScVal {
  return new Address(address).toScVal();
}

function i128ScVal(amount: bigint): xdr.ScVal {
  return nativeToScVal(amount, { type: "i128" });
}

/** Build a deposit transaction: deposit(assets, receiver, from, operator). */
export async function buildDepositTx(
  senderAddress: string,
  amount: bigint,
): Promise<Transaction> {
  const addr = addressScVal(senderAddress);
  return buildContractTx(senderAddress, "deposit", [
    i128ScVal(amount),
    addr,
    addr,
    addr,
  ]);
}

/** Build a withdraw transaction: withdraw(assets, receiver, owner, operator). */
export async function buildWithdrawTx(
  senderAddress: string,
  amount: bigint,
): Promise<Transaction> {
  const addr = addressScVal(senderAddress);
  return buildContractTx(senderAddress, "withdraw", [
    i128ScVal(amount),
    addr,
    addr,
    addr,
  ]);
}

/** Submit a signed transaction XDR and poll until it resolves. */
export async function submitSignedTx(
  signedXdr: string,
): Promise<rpc.Api.GetTransactionResponse> {
  const tx = TransactionBuilder.fromXDR(signedXdr, networkPassphrase);
  const sendResponse = await server.sendTransaction(tx);

  if (sendResponse.status === "ERROR") {
    throw new Error(
      `Transaction send failed: ${sendResponse.errorResult?.toXDR("base64")}`,
    );
  }

  let getResponse = await server.getTransaction(sendResponse.hash);
  while (getResponse.status === "NOT_FOUND") {
    await new Promise((r) => setTimeout(r, 1000));
    getResponse = await server.getTransaction(sendResponse.hash);
  }

  if (getResponse.status === "FAILED") {
    throw new Error("Transaction failed on-chain");
  }

  return getResponse;
}

// ---------------------------------------------------------------------------
// User position queries
// ---------------------------------------------------------------------------

/** Fetch the user's vault share token balance (raw i128 → number scaled by SHARE_SCALE 10^13). */
export async function fetchUserShares(address: string): Promise<number> {
  const result = await callViewFunction("balance", [addressScVal(address)]);
  return i128ToNumber(result, SHARE_SCALE);
}

/** Fetch the USDC-equivalent value of the user's shares via convert_to_assets. */
export async function fetchUserAssetsValue(address: string): Promise<number> {
  const sharesResult = await callViewFunction("balance", [
    addressScVal(address),
  ]);
  const sharesRaw = scValToNative(sharesResult) as bigint;
  if (sharesRaw === 0n) return 0;
  const assetsResult = await callViewFunction("convert_to_assets", [
    i128ScVal(sharesRaw),
  ]);
  return i128ToNumber(assetsResult, USDC_SCALE);
}

/** Preview how many shares a deposit of `assets` would yield. */
export async function fetchPreviewDeposit(assets: bigint): Promise<bigint> {
  const result = await callViewFunction("preview_deposit", [i128ScVal(assets)]);
  return scValToNative(result) as bigint;
}

/** Preview how many shares will be burned for a withdrawal of `assets`. */
export async function fetchPreviewWithdraw(assets: bigint): Promise<bigint> {
  const result = await callViewFunction("preview_withdraw", [
    i128ScVal(assets),
  ]);
  return scValToNative(result) as bigint;
}

/** Get the maximum amount of assets the user can withdraw (based on their share balance). */
export async function fetchMaxWithdraw(address: string): Promise<number> {
  const result = await callViewFunction("max_withdraw", [
    addressScVal(address),
  ]);
  return i128ToNumber(result, USDC_SCALE);
}

/** Get the maximum amount of assets the user can deposit into the vault. */
export async function fetchMaxDeposit(address: string): Promise<number> {
  const result = await callViewFunction("max_deposit", [addressScVal(address)]);
  return i128ToNumber(result, USDC_SCALE);
}

/** Fetch the user's underlying asset (USDC) balance by querying the vault's asset contract. */
export async function fetchUserAssetBalance(address: string): Promise<number> {
  // Get the underlying asset contract address from the vault
  const assetResult = await callViewFunction("query_asset");
  const assetAddress = scValToNative(assetResult) as string;

  // Query the asset contract's balance for this user
  const assetContract = new Contract(assetAddress);
  const tx = new TransactionBuilder(SOURCE_ACCOUNT, {
    fee: "100",
    networkPassphrase,
  })
    .addOperation(assetContract.call("balance", new Address(address).toScVal()))
    .setTimeout(30)
    .build();

  const simResponse = await server.simulateTransaction(tx);

  if (
    !rpc.Api.isSimulationSuccess(simResponse) ||
    !simResponse.result?.retval
  ) {
    return 0;
  }

  return i128ToNumber(simResponse.result.retval, USDC_SCALE);
}

export { server, networkPassphrase };
