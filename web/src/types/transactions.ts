/**
 * Domain types for the transactions/events tabs: per-tab source tag, the
 * UI-facing transaction and contract-event shapes, and their paginated wrappers.
 * @module types/transactions
 */

/** Tab a transaction or event is rendered under. */
export type EntrySource = "vault" | "timelock" | "treasury";

/**
 * A single Horizon operation, tagged with its source tab.
 * @remarks The Treasury tab renders one row per operation (not per transaction)
 * so users can see exactly what executed — payment, invoke, change_trust, … —
 * rather than just an op count. `id` is the Horizon toid, monotonic and used
 * as both React key and stream-dedupe key.
 */
export interface TransactionEntry {
  /** Operation id (unique, monotonic — used as React key and stream dedupe). */
  id: string;
  txHash: string;
  ledger: number;
  /** ISO-8601 timestamp. */
  createdAt: string;
  /** Horizon operation type (e.g. "payment", "invoke_host_function"). */
  type: string;
  /** Human-readable one-line description of what the op did. */
  summary: string;
  /** Status of the parent transaction. */
  successful: boolean;
  source: EntrySource;
}

/** Paginated response for transactions. */
export interface TransactionsPage {
  transactions: TransactionEntry[];
  cursor: string | undefined;
}

/**
 * A single contract event surfaced in the Vault/Governance tabs.
 * @remarks Sourced from Soroban RPC `getEvents`. RPC retains only a rolling
 * ~24h window of events on testnet — older history is intentionally not shown
 * here; users follow the explorer link for full history.
 */
export interface ContractEventEntry {
  id: string;
  /** ISO-8601 timestamp. */
  createdAt: string;
  contract: string;
  /** Decoded topic names (e.g. ["operation_scheduled", "CBA65...", "CC3QF..."]). */
  topics: string[];
  /**
   * Decoded event data payload. Many events (`borrowed`, `interest_accrued`,
   * `ownership_transferred`, …) carry their fields here rather than as extra
   * topics. Shape varies per event; the table renders it generically.
   */
  value?: unknown;
  /** First topic — the event name. */
  eventName: string;
  pagingToken: string;
  source: EntrySource;
}

/** Paginated response for contract events. */
export interface ContractEventsPage {
  events: ContractEventEntry[];
  cursor: string | undefined;
}
