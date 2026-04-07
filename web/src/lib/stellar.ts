import { Horizon, scValToNative, xdr } from "@stellar/stellar-sdk";
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
// Shared Horizon operation helpers
// ---------------------------------------------------------------------------

function shortAddr(addr: string | undefined): string {
  if (!addr) return "—";
  return `${addr.slice(0, 4)}…${addr.slice(-4)}`;
}

function assetLabel(
  type: string | undefined,
  code: string | undefined,
): string {
  if (!type || type === "native") return "XLM";
  return code ?? type;
}

/**
 * One-line, human-readable description of a Horizon operation.
 *
 * Covers the operation types we expect to see on a vault treasury: payments,
 * account lifecycle, trustlines, offers, contract invocations. Falls back to
 * the (humanized) type name for anything we don't special-case.
 */
// biome-ignore lint/suspicious/noExplicitAny: Horizon op records are a wide discriminated union; switching by `type` is the canonical way to read them.
function formatOpSummary(op: any): string {
  switch (op.type) {
    case "payment":
      return `${op.amount} ${assetLabel(op.asset_type, op.asset_code)} → ${shortAddr(op.to)}`;
    case "create_account":
      return `create ${shortAddr(op.account)} (${op.starting_balance} XLM)`;
    case "path_payment_strict_send":
    case "path_payment_strict_receive":
      return `${op.amount} ${assetLabel(op.asset_type, op.asset_code)} → ${shortAddr(op.to)}`;
    case "account_merge":
      return `merge → ${shortAddr(op.into)}`;
    case "change_trust": {
      const asset = assetLabel(op.asset_type, op.asset_code);
      return op.limit === "0.0000000" ? `remove trust ${asset}` : `trust ${asset}`;
    }
    case "manage_sell_offer":
    case "manage_buy_offer":
    case "create_passive_sell_offer":
      return `offer ${op.amount ?? ""} ${assetLabel(op.selling_asset_type, op.selling_asset_code)}/${assetLabel(op.buying_asset_type, op.buying_asset_code)}`.trim();
    case "set_options":
      return "set options";
    case "manage_data":
      return op.name ? `data ${op.name}` : "manage data";
    case "bump_sequence":
      return `bump → ${op.bump_to}`;
    case "invoke_host_function":
      return op.function ? `invoke ${String(op.function).replace(/^HostFunctionType/, "")}` : "invoke contract";
    case "extend_footprint_ttl":
      return "extend footprint TTL";
    case "restore_footprint":
      return "restore footprint";
    default:
      return String(op.type).replace(/_/g, " ");
  }
}

/** Derive ledger sequence from a Horizon operation `id` (toid format). */
function ledgerFromOpId(id: string): number {
  try {
    return Number(BigInt(id) >> 32n);
  } catch {
    return 0;
  }
}

// biome-ignore lint/suspicious/noExplicitAny: see formatOpSummary
function mapHorizonOp(record: any, source: EntrySource): TransactionEntry {
  return {
    id: record.id,
    txHash: record.transaction_hash,
    ledger: ledgerFromOpId(record.id),
    createdAt: record.created_at,
    type: record.type,
    summary: formatOpSummary(record),
    successful: record.transaction_successful ?? true,
    source,
  };
}

// ---------------------------------------------------------------------------
// Fetch operations (works for contracts and accounts)
// ---------------------------------------------------------------------------

/**
 * Fetch a page of operations for any Stellar account or contract.
 *
 * Uses Horizon's `/accounts/{id}/operations` endpoint so each row in the UI
 * corresponds to a concrete executed operation (payment, invoke, …) rather
 * than a transaction wrapper.
 */
export async function fetchTransactions(
  accountId: string,
  source: EntrySource,
  cursor?: string,
): Promise<TransactionsPage> {
  let builder = horizonServer
    .operations()
    .forAccount(accountId)
    .order("desc")
    .limit(EVENTS_PAGE_SIZE);

  if (cursor) {
    builder = builder.cursor(cursor);
  }

  const response = await builder.call();
  const records = response.records;

  const transactions = records.map((r) => mapHorizonOp(r, source));
  const nextCursor =
    records.length > 0 ? records[records.length - 1].paging_token : undefined;

  return { transactions, cursor: nextCursor };
}

/**
 * Open an SSE stream of new operations on any Stellar account or contract.
 * Returns a close function to stop the stream.
 */
export function streamTransactions(
  accountId: string,
  source: EntrySource,
  onMessage: (tx: TransactionEntry) => void,
  onError?: (err: MessageEvent) => void,
): () => void {
  return horizonServer
    .operations()
    .forAccount(accountId)
    .cursor("now")
    .stream({
      onmessage: (record) => onMessage(mapHorizonOp(record, source)),
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
  /**
   * Base64-encoded XDR `ScVal` of the event body. Most contract events
   * (`borrowed`, `interest_accrued`, `ownership_transferred`, …) stash their
   * fields here rather than in additional topics, so we have to decode this
   * client-side to get a useful detail string.
   */
  bodyXdr?: string;
  paging_token: string;
}

/**
 * Decode a base64 `ScVal` body into a native JS value (object / bigint /
 * string). Returns `undefined` on any decoding error so the table can fall
 * back to topic args without crashing the page.
 */
function decodeEventBody(bodyXdr: string | undefined): unknown {
  if (!bodyXdr) return undefined;
  try {
    return scValToNative(xdr.ScVal.fromXDR(bodyXdr, "base64"));
  } catch {
    return undefined;
  }
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
    value: decodeEventBody(record.bodyXdr),
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

/**
 * Build an explorer URL for a single operation.
 *
 * Stellar Expert event ids are formatted as `<op_toid>-<event_index>`; the
 * leading toid is the same id used by `/op/<id>` on the explorer.
 */
export function opExplorerUrl(eventOrOpId: string): string {
  const opId = eventOrOpId.split("-")[0];
  return `${explorerUrl}/op/${opId}`;
}
