import { Horizon } from "@stellar/stellar-sdk";
import {
  DEFAULT_HORIZON_URL,
  DEFAULT_EXPLORER_URL,
  EVENTS_PAGE_SIZE,
  STELLAR_EXPERT_API_URL,
} from "./constants";
import type {
  EntrySource,
  ContractEventEntry,
  ContractEventsPage,
  TransactionEntry,
  TransactionsPage,
} from "@/types/transactions";

const horizonUrl =
  import.meta.env.VITE_HORIZON_URL || DEFAULT_HORIZON_URL;
const explorerUrl =
  import.meta.env.VITE_STELLAR_EXPLORER_URL || DEFAULT_EXPLORER_URL;

const horizonServer = new Horizon.Server(horizonUrl);

// ---------------------------------------------------------------------------
// Shared Horizon transaction helpers
// ---------------------------------------------------------------------------

/** Map a Horizon transaction record to our TransactionEntry type. */
function mapHorizonTx(
  record: Horizon.ServerApi.TransactionRecord,
  source: EntrySource,
): TransactionEntry {
  return {
    txHash: record.hash,
    ledger: record.ledger_attr,
    createdAt: record.created_at,
    sourceAccount: record.source_account,
    memo: record.memo ?? "",
    operationCount: record.operation_count,
    successful: record.successful,
    source,
  };
}

// ---------------------------------------------------------------------------
// Fetch transactions (works for contracts and accounts)
// ---------------------------------------------------------------------------

/**
 * Fetch a page of transactions for any Stellar account or contract.
 * Uses Horizon's `/accounts/{id}/transactions` endpoint which provides
 * full history with server-side filtering.
 */
export async function fetchTransactions(
  accountId: string,
  source: EntrySource,
  cursor?: string,
): Promise<TransactionsPage> {
  let builder = horizonServer
    .transactions()
    .forAccount(accountId)
    .order("desc")
    .limit(EVENTS_PAGE_SIZE);

  if (cursor) {
    builder = builder.cursor(cursor);
  }

  const response = await builder.call();
  const records = response.records;

  const transactions = records.map((r) => mapHorizonTx(r, source));
  const nextCursor =
    records.length > 0 ? records[records.length - 1].paging_token : undefined;

  return { transactions, cursor: nextCursor };
}

/**
 * Open an SSE stream for new transactions on any Stellar account or contract.
 * Returns a close function to stop the stream.
 */
export function streamTransactions(
  accountId: string,
  source: EntrySource,
  onMessage: (tx: TransactionEntry) => void,
  onError?: (err: MessageEvent) => void,
): () => void {
  return horizonServer
    .transactions()
    .forAccount(accountId)
    .cursor("now")
    .stream({
      onmessage: (record) => onMessage(mapHorizonTx(record, source)),
      onerror: onError,
    });
}

// ---------------------------------------------------------------------------
// Contract events via Stellar Expert API
// ---------------------------------------------------------------------------

interface StellarExpertEvent {
  id: string;
  ts: number;
  contract: string;
  initiator: string;
  topics: string[];
  paging_token: string;
}

interface StellarExpertEventsResponse {
  _links: {
    next?: { href: string };
  };
  _embedded: {
    records: StellarExpertEvent[];
  };
}

function mapExpertEvent(
  record: StellarExpertEvent,
  source: EntrySource,
): ContractEventEntry {
  return {
    id: record.id,
    createdAt: new Date(record.ts * 1000).toISOString(),
    contract: record.contract,
    initiator: record.initiator,
    topics: record.topics,
    eventName: record.topics[0] ?? "unknown",
    pagingToken: record.paging_token,
    source,
  };
}

/**
 * Fetch contract events from the Stellar Expert API.
 * Works for both C... contract addresses and provides full history.
 */
export async function fetchContractEvents(
  contractId: string,
  source: EntrySource,
  cursor?: string,
): Promise<ContractEventsPage> {
  const url = new URL(
    `${STELLAR_EXPERT_API_URL}/contract/${contractId}/events`,
  );
  url.searchParams.set("order", "desc");
  url.searchParams.set("limit", String(EVENTS_PAGE_SIZE));
  if (cursor) {
    url.searchParams.set("cursor", cursor);
  }

  const response = await fetch(url.toString());
  if (!response.ok) {
    throw new Error(`Stellar Expert API error: ${response.status}`);
  }

  const data: StellarExpertEventsResponse = await response.json();
  const records = data._embedded.records;

  const events = records.map((r) => mapExpertEvent(r, source));
  const nextCursor =
    records.length > 0 ? records[records.length - 1].paging_token : undefined;

  return { events, cursor: nextCursor };
}

// ---------------------------------------------------------------------------
// Explorer link helpers
// ---------------------------------------------------------------------------

export function txExplorerUrl(txHash: string): string {
  return `${explorerUrl}/tx/${txHash}`;
}

export function contractExplorerUrl(contractId: string): string {
  return `${explorerUrl}/contract/${contractId}`;
}

export function accountExplorerUrl(address: string): string {
  return `${explorerUrl}/account/${address}`;
}
