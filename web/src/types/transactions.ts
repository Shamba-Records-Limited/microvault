/** Source of a transaction entry. */
export type EntrySource = "vault" | "timelock" | "treasury";

/**
 * A single Horizon operation, tagged with its source.
 *
 * The Treasury tab renders one row per operation (not per transaction), so
 * users can see exactly what was executed (payment, invoke, change_trust, …)
 * rather than just an op count.
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

/** A contract event from the Stellar Expert API. */
export interface ContractEventEntry {
  id: string;
  /** ISO-8601 timestamp. */
  createdAt: string;
  contract: string;
  initiator: string;
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
