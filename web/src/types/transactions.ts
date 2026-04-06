/** Source of a transaction entry. */
export type EntrySource = "vault" | "timelock" | "treasury";

/** A transaction from the Horizon API, tagged with its source. */
export interface TransactionEntry {
  txHash: string;
  ledger: number;
  /** ISO-8601 timestamp. */
  createdAt: string;
  sourceAccount: string;
  memo: string;
  operationCount: number;
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
