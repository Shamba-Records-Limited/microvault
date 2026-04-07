import { Horizon, rpc, scValToNative } from "@stellar/stellar-sdk";
import {
  DEFAULT_HORIZON_URL,
  DEFAULT_EXPLORER_URL,
  DEFAULT_RPC_URL,
  EVENTS_PAGE_SIZE,
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

const sorobanRpcUrl = import.meta.env.VITE_SOROBAN_RPC_URL || DEFAULT_RPC_URL;
const sorobanServer = new rpc.Server(sorobanRpcUrl);

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
// Contract events via Soroban RPC
// ---------------------------------------------------------------------------

/**
 * Best-effort decode of an event topic ScVal into a printable string. Symbol
 * topics (the common case — event names, addresses) come back as strings;
 * everything else gets JSON-serialized so the table can still render it.
 */
function topicToString(topic: ReturnType<typeof scValToNative>): string {
  if (topic == null) return "";
  if (typeof topic === "string") return topic;
  if (typeof topic === "bigint") return topic.toString();
  if (typeof topic === "number" || typeof topic === "boolean") {
    return String(topic);
  }
  try {
    return JSON.stringify(topic);
  } catch {
    return String(topic);
  }
}

function mapRpcEvent(
  record: rpc.Api.EventResponse,
  source: EntrySource,
): ContractEventEntry {
  const topics = record.topic.map((t) => topicToString(scValToNative(t)));
  return {
    id: record.id,
    createdAt: new Date(record.ledgerClosedAt).toISOString(),
    contract: record.contractId?.toString() ?? "",
    topics,
    value: scValToNative(record.value),
    eventName: topics[0] ?? "unknown",
    pagingToken: record.id,
    source,
  };
}

/**
 * Fetch contract events via Soroban RPC `getEvents`.
 *
 * RPC only retains a rolling window of events (~7 days on testnet, ~24h on
 * mainnet), which is fine for the realtime metrics this UI surfaces. For full
 * history, users can follow the explorer link to Stellar Expert.
 */
export async function fetchContractEvents(
  contractId: string,
  source: EntrySource,
  cursor?: string,
): Promise<ContractEventsPage> {
  const baseRequest = {
    filters: [
      { type: "contract" as const, contractIds: [contractId] },
    ],
    limit: EVENTS_PAGE_SIZE,
  };

  // RPC requires either `startLedger` or `cursor`, never both. Soroban RPC
  // caps `getEvents` at ~10k ledgers per call, so we can't ask it to scan the
  // whole retention window in one shot — if we started at `oldestLedger`, RPC
  // would only return events from the *oldest* 10k ledgers and stop, missing
  // anything recent. Since this UI is for realtime metrics, start the scan
  // close to `latestLedger` instead.
  const RPC_MAX_LEDGER_SPAN = 10_000;
  let request: rpc.Server.GetEventsRequest;
  if (cursor) {
    request = { ...baseRequest, cursor };
  } else {
    const latest = await sorobanServer.getLatestLedger();
    request = {
      ...baseRequest,
      startLedger: Math.max(latest.sequence - RPC_MAX_LEDGER_SPAN + 1, 1),
    };
  }

  const response = await sorobanServer.getEvents(request);
  // RPC returns oldest-first; the table renders newest-first.
  const records = [...response.events].reverse();

  const events = records.map((r) => mapRpcEvent(r, source));
  const nextCursor = response.cursor;

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
