/**
 * Horizon and Soroban RPC clients for the Transactions page: treasury
 * operation fetches and SSE streams, contract event fetches via
 * Soroban RPC, and explorer URL helpers.
 * @module lib/stellar
 * @remarks Horizon powers the Treasury tab (operation history + streaming).
 * Soroban RPC powers the Vault/Governance tabs; its `getEvents` retention
 * window is ~24h on testnet, which is why those tabs are labelled "Recent".
 */
import { Horizon, rpc, scValToNative, xdr } from "@stellar/stellar-sdk";
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

/** Truncates a G-address to `GABC…WXYZ` for compact row labels. */
function shortAddr(addr: string | undefined): string {
  if (!addr) return "—";
  return `${addr.slice(0, 4)}…${addr.slice(-4)}`;
}

/** Picks a display label for a Horizon asset_type/asset_code pair. */
function assetLabel(
  type: string | undefined,
  code: string | undefined,
): string {
  if (!type || type === "native") return "XLM";
  return code ?? type;
}

/**
 * Builds a one-line, human-readable description of a Horizon operation.
 * @param op - Raw Horizon operation record (loosely typed discriminated union)
 * @returns A short summary suitable for the Treasury tab "Details" column
 * @remarks Covers the operation types we expect to see on a vault treasury:
 * payments, account lifecycle, trustlines, offers, contract invocations.
 * Falls back to the humanised type name for anything unrecognised.
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
    case "invoke_host_function": {
      // Horizon exposes the host function as `parameters: [{type, value}]`
      // where each `value` is a base64-encoded ScVal. For an InvokeContract
      // host function, the conventional layout is:
      //   params[0] = ScAddress (the contract being called)
      //   params[1] = ScSymbol  (the function name)
      //   params[2..] = function args
      // Decode params[1] to surface the actual function name in the UI; fall
      // back gracefully on any non-invoke variants (CreateContract, etc.).
      const params = Array.isArray(op.parameters) ? op.parameters : [];
      const fnParam = params[1];
      if (fnParam?.value) {
        try {
          const decoded = scValToNative(
            xdr.ScVal.fromXDR(fnParam.value, "base64"),
          );
          if (typeof decoded === "string" && decoded.length > 0) {
            return `invoke ${decoded}`;
          }
        } catch {
          // fall through to generic label
        }
      }
      return op.function
        ? `invoke ${String(op.function).replace(/^HostFunctionType/, "")}`
        : "invoke contract";
    }
    case "extend_footprint_ttl":
      return "extend footprint TTL";
    case "restore_footprint":
      return "restore footprint";
    default:
      return String(op.type).replace(/_/g, " ");
  }
}

/**
 * Derives a ledger sequence from a Horizon operation id (toid format).
 * @param id - Horizon operation id — a 64-bit total-order id as a decimal string
 * @returns The ledger sequence encoded in the upper 32 bits, or 0 on parse error
 */
function ledgerFromOpId(id: string): number {
  try {
    return Number(BigInt(id) >> 32n);
  } catch {
    return 0;
  }
}

/**
 * Maps a raw Horizon operation record to the table's `TransactionEntry` shape.
 * @param record - Horizon operation record as returned by the SDK
 * @param source - Tab tag attached to the row
 * @returns Normalised entry ready for rendering
 * @remarks `transaction_successful` defaults to `true` when absent because
 * streamed records may omit it on in-flight ops; the subsequent Horizon poll
 * will correct any false positive.
 */
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
 * Fetches a page of operations for any Stellar account or contract.
 * @param accountId - G- or C-address to query
 * @param source - Tab tag attached to every returned row
 * @param cursor - Horizon paging token from a previous page (omit for first page)
 * @returns The mapped operations plus the next cursor (undefined at end-of-list)
 * @remarks Uses `/accounts/{id}/operations` so each row corresponds to a
 * concrete executed operation rather than a transaction wrapper — users see
 * exactly what was done, not just an op count.
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
 * Opens an SSE stream of new operations for a Stellar account or contract.
 * @param accountId - G- or C-address to stream
 * @param source - Tab tag attached to every emitted row
 * @param onMessage - Invoked once per new operation
 * @param onError - Optional handler for stream-level errors
 * @returns A close function — call it to tear the stream down on unmount
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
 * Best-effort decodes an event topic ScVal into a printable string.
 * @param topic - Already-native-decoded topic value from `scValToNative`
 * @returns A display-safe string for the topics column
 * @remarks Symbol topics (the common case — event names, addresses) arrive as
 * strings; everything else is JSON-serialised so the table can still render
 * exotic topic shapes without throwing.
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

/**
 * Maps a Soroban RPC `EventResponse` to the table's `ContractEventEntry` shape.
 * @param record - Raw RPC event
 * @param source - Tab tag attached to the row
 * @returns Normalised event entry
 * @remarks Uses `record.id` as the paging token — the SDK type has no
 * `pagingToken` field, and `id` is already a unique, monotonic toid-event id.
 */
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
 * Fetches contract events via Soroban RPC `getEvents`.
 * @param contractId - C-address to filter events by
 * @param source - Tab tag attached to every returned event
 * @param cursor - RPC cursor from a previous page (omit for first page)
 * @returns Newest-first page of events plus the next cursor
 * @remarks RPC retains only a rolling ~24h window of events on testnet, which
 * is fine for the realtime metrics this UI surfaces. Two important quirks:
 * (1) RPC caps `getEvents` at ~10k ledgers per call, so starting at
 * `oldestLedger` would return nothing interesting — we instead start close to
 * `latestLedger`. (2) RPC returns oldest-first; we reverse the page because
 * the table renders newest-first.
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

/** Builds a block-explorer URL for a transaction hash. */
export function txExplorerUrl(txHash: string): string {
  return `${explorerUrl}/tx/${txHash}`;
}

/** Builds a block-explorer URL for a contract C-address. */
export function contractExplorerUrl(contractId: string): string {
  return `${explorerUrl}/contract/${contractId}`;
}

/** Builds a block-explorer URL for an account G-address. */
export function accountExplorerUrl(address: string): string {
  return `${explorerUrl}/account/${address}`;
}

/**
 * Builds a block-explorer URL for a single operation.
 * @param eventOrOpId - Either a bare op toid or a `<op_toid>-<event_index>` event id
 * @returns Explorer `/op/<id>` URL
 * @remarks Stellar Expert event ids are formatted as `<op_toid>-<event_index>`;
 * the leading toid is the same id used by `/op/<id>` on the explorer, so we
 * strip the suffix before building the link.
 */
export function opExplorerUrl(eventOrOpId: string): string {
  const opId = eventOrOpId.split("-")[0];
  return `${explorerUrl}/op/${opId}`;
}
