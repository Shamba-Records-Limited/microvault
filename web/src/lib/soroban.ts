/**
 * Soroban RPC client for the Microvault contract: read-only view calls,
 * deposit/withdraw transaction builders, share/asset preview helpers, and
 * user position queries.
 * @module lib/soroban
 * @remarks All view functions go through `simulateTransaction` against a
 * zero-balance source account so they require no signing. Each public fetcher
 * tolerates individual view traps so a single broken entry (e.g. after an OZ
 * storage-layout migration) cannot blank the entire pool card — see
 * `docs/soroban-contract-upgrade-procedure.md`.
 */
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
 * Invokes a Soroban contract view function via `simulateTransaction`.
 * @param functionName - Contract function symbol to call
 * @param args - Pre-built ScVal arguments
 * @returns The function's raw return ScVal, ready for `scValToNative`
 * @remarks No signing or fees — the call is read-only. Throws on simulation
 * error or empty retval; callers wrap in `.catch(() => null)` when they want
 * to swallow individual failures rather than blanking the whole UI.
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

/**
 * Converts an on-chain i128 ScVal to a JS number using the given scale.
 * @param scVal - i128 value returned from a contract call
 * @param scale - Scaling factor in stroops (`USDC_SCALE`, `SHARE_SCALE`, …)
 * @returns Human-readable number in major units
 * @remarks Lossy for amounts above `Number.MAX_SAFE_INTEGER * scale`. Fine for
 * UI display where TVL is bounded; do not use for chain-side math.
 */
function i128ToNumber(scVal: xdr.ScVal, scale: bigint): number {
  const raw = scValToNative(scVal) as bigint;
  return Number(raw) / Number(scale);
}

/**
 * Converts a WAD-scaled (1e18) ScVal to a 0–100 percentage.
 * @param scVal - WAD-scaled rate (utilization, APR, …) returned from chain
 * @returns Percentage in 0–100 range
 */
function wadToPercent(scVal: xdr.ScVal): number {
  const raw = scValToNative(scVal) as bigint;
  return (Number(raw) / Number(WAD_SCALE)) * 100;
}

/**
 * Fetches all numeric vault metrics (TVL, borrowed, utilization, APR, paused)
 * in one parallel batch.
 * @returns A fully-populated `VaultStats` object; failing fields fall back to
 * `0` / `false` rather than throwing
 * @remarks Each view is tolerated to fail individually so a single trapping
 * function (e.g. after an OZ storage-layout migration that strands one entry)
 * cannot blank the entire pool card. See `docs/soroban-contract-upgrade-procedure.md`.
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
 * Fetches the vault display name and underlying asset symbol.
 * @returns `{ name, symbol }`; either falls back to a generic label on failure
 * @remarks A contract upgraded across an OZ storage-layout change can trap
 * with `UnsetMetadata` (error 105) on `name`/`symbol` even though every other
 * view still works. We fall back to "Microvault" / "mvUSDC" so the pool card
 * renders instead of vanishing.
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
 * Fetches vault governance addresses (treasury, owner, guardian) in parallel.
 * @returns A `VaultAddresses` object; trapping fields fall back to `null`
 * @remarks Most common cause of `null` is a stranded entry after an OZ
 * storage-layout migration. See `docs/soroban-contract-upgrade-procedure.md`.
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

/**
 * Builds, simulates, and assembles a contract-call transaction for the user.
 * @param senderAddress - User's G-account; sourced via `getAccount` for the seqnum
 * @param functionName - Vault contract function symbol
 * @param args - Pre-built ScVal arguments matching the function signature
 * @returns A simulation-assembled transaction ready to sign and submit
 * @remarks Throws on simulation failure with a contextualised error message
 * so callers can surface a useful toast via `parseContractError`.
 */
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

/**
 * Builds a `deposit(assets, receiver, from, operator)` transaction.
 * @param senderAddress - User's G-account used for all four address arguments
 * @param amount - Asset amount in stroops (USDC 10^7)
 * @returns Assembled transaction ready for signing
 * @remarks `receiver`, `from`, and `operator` are intentionally all the sender —
 * the UI only supports self-deposits. Revisit if we add delegated flows.
 */
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

/**
 * Builds a `withdraw(assets, receiver, owner, operator)` transaction.
 * @param senderAddress - User's G-account used for all four address arguments
 * @param amount - Asset amount in stroops (USDC 10^7)
 * @returns Assembled transaction ready for signing
 */
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

/**
 * Submits a signed transaction XDR and polls until it resolves.
 * @param signedXdr - Base64-encoded signed transaction envelope
 * @returns The final `getTransaction` response (only on success)
 * @remarks Polls every 1s while the transaction is `NOT_FOUND`. Throws on
 * `ERROR` send status or `FAILED` post-inclusion, which `parseContractError`
 * then translates into a user-facing message. There is no cancellation — if
 * the network stalls, the promise will pend indefinitely.
 */
export async function submitSignedTx(
  signedXdr: string,
): Promise<rpc.Api.GetTransactionResponse> {
  const tx = TransactionBuilder.fromXdr(signedXdr, networkPassphrase);
  const sendResponse = await server.sendTransaction(tx);

  if (sendResponse.status === "ERROR") {
    throw new Error(
      `Transaction send failed: ${sendResponse.errorResult?.toXdr("base64")}`,
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

/**
 * Fetches the user's vault share token balance.
 * @param address - G-account to query
 * @returns Share balance in major units (already divided by `SHARE_SCALE`)
 */
export async function fetchUserShares(address: string): Promise<number> {
  const result = await callViewFunction("balance", [addressScVal(address)]);
  return i128ToNumber(result, SHARE_SCALE);
}

/**
 * Fetches the USDC-equivalent value of the user's vault shares.
 * @param address - G-account to query
 * @returns USDC value in major units, or 0 if the user holds no shares
 * @remarks Short-circuits with 0 on an empty balance to avoid a second
 * `convert_to_assets` RPC call when the answer is trivially zero.
 */
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

/**
 * Previews how many shares a deposit of `assets` would mint.
 * @param assets - Deposit amount in stroops
 * @returns Minted shares in raw i128 stroops
 */
export async function fetchPreviewDeposit(assets: bigint): Promise<bigint> {
  const result = await callViewFunction("preview_deposit", [i128ScVal(assets)]);
  return scValToNative(result) as bigint;
}

/**
 * Previews how many shares will be burned for a withdrawal of `assets`.
 * @param assets - Withdrawal amount in stroops
 * @returns Burned shares in raw i128 stroops
 */
export async function fetchPreviewWithdraw(assets: bigint): Promise<bigint> {
  const result = await callViewFunction("preview_withdraw", [
    i128ScVal(assets),
  ]);
  return scValToNative(result) as bigint;
}

/**
 * Fetches the maximum assets the user can withdraw right now.
 * @param address - G-account to query
 * @returns Withdrawable amount in USDC major units
 * @remarks Bounded by the user's share balance *and* current vault liquidity;
 * the UI uses this for client-side validation before prompting for signing.
 */
export async function fetchMaxWithdraw(address: string): Promise<number> {
  const result = await callViewFunction("max_withdraw", [
    addressScVal(address),
  ]);
  return i128ToNumber(result, USDC_SCALE);
}

/**
 * Fetches the maximum assets the user can deposit into the vault.
 * @param address - G-account to query
 * @returns Depositable amount in USDC major units
 */
export async function fetchMaxDeposit(address: string): Promise<number> {
  const result = await callViewFunction("max_deposit", [addressScVal(address)]);
  return i128ToNumber(result, USDC_SCALE);
}

/**
 * Fetches the user's underlying asset (USDC) wallet balance.
 * @param address - G-account to query
 * @returns USDC balance in major units; 0 on simulation failure
 * @remarks Resolves the underlying asset contract dynamically via
 * `query_asset` so the same code works if the vault is later pointed at a
 * different asset. Cross-contract call — costs two simulations.
 */
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
