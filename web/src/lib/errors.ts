/**
 * Contract error mapping and message normalisation for Soroban vault calls.
 * @module lib/errors
 * @remarks The numeric code table mirrors the Go backend's
 * `pkg/stellar/types/errors.go`. Keep them in sync when contract errors are
 * added or renumbered, otherwise the UI will fall back to "Contract error #N"
 * which is much less helpful than a written explanation.
 */

/** Mapped Soroban contract error with a user-facing message and source crate. */
interface ContractError {
  code: number;
  name: string;
  message: string;
  source:
    | "microvault"
    | "FungibleToken"
    | "Vault"
    | "Pausable"
    | "Ownable"
    | "Unknown";
}

const CONTRACT_ERRORS: Record<number, ContractError> = {
  // MicroVaultError (1-12)
  1: {
    code: 1,
    name: "Unauthorized",
    message: "You are not authorized for this operation",
    source: "microvault",
  },
  2: {
    code: 2,
    name: "CannotSweepUnderlyingAsset",
    message: "Cannot sweep the underlying asset",
    source: "microvault",
  },
  3: {
    code: 3,
    name: "InvalidAmount",
    message: "Amount must be greater than zero",
    source: "microvault",
  },
  4: {
    code: 4,
    name: "ExceedsMaxDeposit",
    message: "Deposit exceeds the maximum allowed amount",
    source: "microvault",
  },
  5: {
    code: 5,
    name: "ExceedsMaxWithdraw",
    message: "Withdrawal exceeds the maximum allowed amount",
    source: "microvault",
  },
  6: {
    code: 6,
    name: "TreasuryNotSet",
    message: "Treasury address has not been configured",
    source: "microvault",
  },
  9: {
    code: 9,
    name: "ExceedsUtilizationCap",
    message: "Operation would exceed the utilization cap",
    source: "microvault",
  },
  10: {
    code: 10,
    name: "InsufficientLiquidity",
    message: "Insufficient liquidity available in the vault",
    source: "microvault",
  },
  11: {
    code: 11,
    name: "RepayExceedsDebt",
    message: "Repayment amount exceeds outstanding debt",
    source: "microvault",
  },
  12: {
    code: 12,
    name: "SharesLocked",
    message: "Your shares are currently locked and cannot be withdrawn",
    source: "microvault",
  },

  // FungibleTokenError (100-114)
  100: {
    code: 100,
    name: "InsufficientBalance",
    message: "Insufficient token balance for this transfer",
    source: "FungibleToken",
  },
  101: {
    code: 101,
    name: "InsufficientAllowance",
    message: "Insufficient allowance for this operation",
    source: "FungibleToken",
  },
  102: {
    code: 102,
    name: "InvalidLiveUntilLedger",
    message: "Invalid allowance expiration ledger",
    source: "FungibleToken",
  },
  103: {
    code: 103,
    name: "LessThanZero",
    message: "Amount must be non-negative",
    source: "FungibleToken",
  },
  104: {
    code: 104,
    name: "MathOverflow",
    message: "Arithmetic overflow during token calculation",
    source: "FungibleToken",
  },

  // VaultTokenError (400-410)
  400: {
    code: 400,
    name: "VaultAssetAddressNotSet",
    message: "Vault asset address is not initialized",
    source: "Vault",
  },
  403: {
    code: 403,
    name: "VaultInvalidAssetsAmount",
    message: "Invalid asset amount for this vault operation",
    source: "Vault",
  },
  404: {
    code: 404,
    name: "VaultInvalidSharesAmount",
    message: "Invalid shares amount for this vault operation",
    source: "Vault",
  },
  405: {
    code: 405,
    name: "VaultExceededMaxDeposit",
    message: "Deposit exceeds the vault's maximum deposit limit",
    source: "Vault",
  },
  406: {
    code: 406,
    name: "VaultExceededMaxMint",
    message: "Mint exceeds the vault's maximum mint limit",
    source: "Vault",
  },
  407: {
    code: 407,
    name: "VaultExceededMaxWithdraw",
    message: "Withdrawal exceeds your maximum withdrawable amount",
    source: "Vault",
  },
  408: {
    code: 408,
    name: "VaultExceededMaxRedeem",
    message: "Redeem exceeds your maximum redeemable amount",
    source: "Vault",
  },
  410: {
    code: 410,
    name: "MathOverflow",
    message: "Arithmetic overflow in vault calculation",
    source: "Vault",
  },

  // PausableError (1000-1001)
  1000: {
    code: 1000,
    name: "EnforcedPause",
    message: "The vault is currently paused",
    source: "Pausable",
  },
  1001: {
    code: 1001,
    name: "ExpectedPause",
    message: "The vault is not paused",
    source: "Pausable",
  },

  // OwnableError (2100-2102)
  2100: {
    code: 2100,
    name: "OwnerNotSet",
    message: "Contract owner has not been set",
    source: "Ownable",
  },
  2101: {
    code: 2101,
    name: "TransferInProgress",
    message: "Ownership transfer is already in progress",
    source: "Ownable",
  },
  2102: {
    code: 2102,
    name: "OwnerAlreadySet",
    message: "Contract owner has already been set",
    source: "Ownable",
  },
};

/**
 * Extracts a contract error code from a Soroban error string.
 * @param errorMessage - Raw simulation/host error such as `Error(Contract, #407)`
 * @returns The numeric code if present, otherwise `null`
 */
function extractErrorCode(errorMessage: string): number | null {
  const match = errorMessage.match(/#(\d+)/);
  return match ? parseInt(match[1], 10) : null;
}

/**
 * Normalises any thrown error into a single user-facing string for toasts.
 * @param error - Anything caught from a wallet/RPC/contract call
 * @returns A short message suitable for direct display to the user
 * @remarks Branches in priority order: wallet rejection → mapped contract code
 * → known VM-level errors → known transaction-pipeline errors → cleaned-up
 * fallback. The fallback strips simulation/host noise so users never see raw
 * "HostError: …" or trailing event logs.
 */
export function parseContractError(error: unknown): string {
  let message: string;
  if (error instanceof Error) {
    message = error.message;
  } else if (typeof error === "object" && error !== null && "message" in error) {
    message = String((error as { message: unknown }).message);
  } else {
    message = String(error);
  }

  // User rejected signing
  if (
    message.includes("User declined") ||
    message.includes("user closed the modal") ||
    message.includes("rejected") ||
    message.includes("cancelled")
  ) {
    return "Transaction was cancelled";
  }

  // Contract error code
  const code = extractErrorCode(message);
  if (code !== null) {
    const mapped = CONTRACT_ERRORS[code];
    if (mapped) return mapped.message;
    return `Contract error #${code}`;
  }

  // Soroban VM-level errors (no contract error code)
  if (message.includes("InvalidAction")) {
    return "Transaction rejected, you may have insufficient balance or the amount exceeds the allowed limit";
  }

  if (message.includes("InternalError")) {
    return "An internal error occurred in the contract";
  }

  // Transaction failures
  if (message.includes("Transaction failed on-chain")) {
    return "Transaction failed on the network";
  }

  if (message.includes("Transaction send failed")) {
    return "Failed to submit transaction to the network";
  }

  if (message.includes("Wallet not connected")) {
    return "Please connect your wallet first";
  }

  // Fallback — strip verbose simulation prefixes
  const cleaned = message
    .replace(/^Simulation error for \w+:\s*/, "")
    .replace(/HostError:\s*/, "")
    .replace(/Event log \(newest first\):.*$/s, "")
    .trim();

  return cleaned || "An unexpected error occurred";
}
