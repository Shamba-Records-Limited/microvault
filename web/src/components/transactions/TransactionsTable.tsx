import { ExternalLink, Info } from "lucide-react";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import { Skeleton } from "@/components/ui/skeleton";
import {
  txExplorerUrl,
  contractExplorerUrl,
  accountExplorerUrl,
  opExplorerUrl,
} from "@/lib/stellar";
import type { TransactionEntry, ContractEventEntry } from "@/types/transactions";

// ---------------------------------------------------------------------------
// Tab types
// ---------------------------------------------------------------------------

export type TabId = "vault" | "timelock" | "treasury";

interface Tab {
  id: TabId;
  label: string;
}

const TABS: Tab[] = [
  { id: "vault", label: "Vault" },
  { id: "timelock", label: "Governance" },
  { id: "treasury", label: "Treasury" },
];

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

function truncateHash(hash: string): string {
  return `${hash.slice(0, 6)}...${hash.slice(-6)}`;
}

function formatTime(iso: string): string {
  const d = new Date(iso);
  return d.toLocaleString(undefined, {
    month: "short",
    day: "numeric",
    hour: "2-digit",
    minute: "2-digit",
    second: "2-digit",
  });
}

function formatEventName(name: string): string {
  return name.replace(/_/g, " ");
}

/** Render a single event topic argument compactly. Stellar addresses get the
 *  same short form we use for tx hashes; other strings are truncated only if
 *  they would visually dominate the row. */
function formatTopicArg(t: string): string {
  if (/^[GC][A-Z0-9]{55}$/.test(t)) return truncateHash(t);
  return t.length > 24 ? `${t.slice(0, 10)}…${t.slice(-8)}` : t;
}

/** Recursively render an event `value` payload as `key=val, key=val`,
 *  truncating address-shaped strings the same way as topics. */
/** Event-value field names that hold USDC stroop amounts (i128, 7 decimals). */
const USDC_AMOUNT_KEYS = new Set([
  "amount",
  "assets",
  "total_borrowed",
  "new_total_borrowed",
  "interest_amount",
]);
/** Event-value field names that hold vault share amounts (i128, 13 decimals = USDC 7 + 6 virtual offset). */
const SHARE_AMOUNT_KEYS = new Set(["shares"]);
/** Decimal places for vault share token amounts. Mirrors `SHARE_DECIMALS` in web/src/lib/constants.ts. */
const SHARE_DECIMALS = 13;
/** Event-value field names that hold WAD (1e18) fixed-point rates rendered as percentages. */
const WAD_PERCENT_KEYS = new Set(["utilization_rate", "borrow_apr"]);

/** WAD precision (1e18) used by the vault for rate fields. */
const WAD = 1_000_000_000_000_000_000n;

/** Convert a WAD-scaled bigint into a percentage string with 2 decimals. */
function formatWadPercent(value: bigint): string {
  // Carry 4 fractional digits of precision then trim to 2 for display.
  const scaled = (value * 10_000n) / WAD;
  const whole = scaled / 100n;
  const frac = (scaled % 100n).toString().padStart(2, "0");
  return `${whole}.${frac}%`;
}

/** Format an i128 stroop value as a fixed-decimal string (no trailing zeros). */
function formatStroops(value: bigint, decimals = 7): string {
  const negative = value < 0n;
  const abs = negative ? -value : value;
  const base = 10n ** BigInt(decimals);
  const whole = abs / base;
  const frac = (abs % base).toString().padStart(decimals, "0").replace(/0+$/, "");
  return `${negative ? "-" : ""}${whole}${frac ? `.${frac}` : ""}`;
}

function formatEventValue(value: unknown, key?: string): string {
  if (value == null) return "";
  if (typeof value === "string") return formatTopicArg(value);
  if (typeof value === "bigint") {
    if (key && USDC_AMOUNT_KEYS.has(key)) return `${formatStroops(value)} USDC`;
    if (key && SHARE_AMOUNT_KEYS.has(key)) return `${formatStroops(value, SHARE_DECIMALS)} mvUSDC`;
    if (key && WAD_PERCENT_KEYS.has(key)) return formatWadPercent(value);
    return value.toString();
  }
  if (typeof value === "number" || typeof value === "boolean") return String(value);
  // ScvBytes decodes to Uint8Array (and Buffer in some envs). Render as a
  // truncated hex string instead of walking the indexed byte entries — that's
  // what produced the `0=71, 1=243, …` noise on governance events.
  if (value instanceof Uint8Array) {
    const hex = Array.from(value)
      .map((b) => b.toString(16).padStart(2, "0"))
      .join("");
    return hex.length > 20 ? `0x${hex.slice(0, 8)}…${hex.slice(-8)}` : `0x${hex}`;
  }
  if (Array.isArray(value)) return value.map((v) => formatEventValue(v)).join(", ");
  if (typeof value === "object") {
    return Object.entries(value as Record<string, unknown>)
      .map(([k, v]) => `${k}=${formatEventValue(v, k)}`)
      .join(", ");
  }
  return String(value);
}

/** Decode a contract event into a readable, comma-separated detail string.
 *
 *  Two sources are combined:
 *  - Topic args (everything after the event name) — usually indexed addresses.
 *  - The decoded `value` payload — where most events stash their real fields
 *    (e.g. `borrowed` → `amount=…, recipient=…, total_borrowed=…`).
 *
 *  Either may be empty depending on the event; combining both means we never
 *  render a blank `—` when there's actually data on the wire. */
function formatEventArgs(topics: string[], value: unknown): string {
  const argParts = topics.slice(1).map(formatTopicArg);
  const valuePart = formatEventValue(value);
  const combined = [argParts.join(", "), valuePart].filter(Boolean).join(", ");
  return combined || "—";
}

// ---------------------------------------------------------------------------
// Sub-components
// ---------------------------------------------------------------------------

function LiveIndicator() {
  return (
    <span className="inline-flex items-center gap-1.5 ml-2">
      <span className="relative flex h-2 w-2">
        <span className="absolute inline-flex h-full w-full animate-ping rounded-full bg-green-400 opacity-75" />
        <span className="relative inline-flex h-2 w-2 rounded-full bg-green-500" />
      </span>
      <span className="text-xs font-medium text-green-400">Live</span>
    </span>
  );
}

function InfoBanner({ accountId, label }: { accountId: string; label: string }) {
  const isContract = label !== "Treasury";
  const href = isContract ? contractExplorerUrl(accountId) : accountExplorerUrl(accountId);

  return (
    <div className="flex items-start gap-2 rounded-lg border border-border/50 bg-muted/30 px-4 py-3 mb-4">
      <Info className="h-4 w-4 text-muted-foreground mt-0.5 shrink-0" />
      <p className="text-sm text-muted-foreground">
        {label} {isContract ? "events" : "transactions"} for{" "}
        <a
          href={href}
          target="_blank"
          rel="noopener noreferrer"
          className="font-mono text-blue-400 hover:underline"
        >
          {truncateHash(accountId)}
        </a>
      </p>
    </div>
  );
}

function EmptyState() {
  return (
    <div className="flex items-center justify-center py-16">
      <p className="text-muted-foreground">No activity found</p>
    </div>
  );
}

function LoadingSkeleton() {
  return (
    <div className="space-y-3">
      {/* biome-ignore lint/suspicious/noArrayIndexKey: Static skeleton loader list that never reorders */}
      {Array.from({ length: 5 }).map((_, i) => (
        <Skeleton key={i} className="h-14 rounded-xl" />
      ))}
    </div>
  );
}

// ---------------------------------------------------------------------------
// Transaction row (desktop) — Treasury tab
// ---------------------------------------------------------------------------

function TxTableRow({ tx, isNew }: { tx: TransactionEntry; isNew: boolean }) {
  return (
    <tr
      className={`border-b border-border/50 ${isNew ? "animate-in fade-in duration-500 bg-green-500/5" : ""}`}
    >
      <td className="py-3 text-sm text-muted-foreground whitespace-nowrap">
        {formatTime(tx.createdAt)}
      </td>
      <td className="py-3">
        <Badge variant={tx.successful ? "secondary" : "destructive"}>
          {tx.successful ? "Success" : "Failed"}
        </Badge>
      </td>
      <td className="py-3">
        <a
          href={txExplorerUrl(tx.txHash)}
          target="_blank"
          rel="noopener noreferrer"
          className="inline-flex items-center gap-1 text-sm font-mono text-blue-400 hover:underline"
        >
          {truncateHash(tx.txHash)}
          <ExternalLink className="h-3 w-3" />
        </a>
      </td>
      <td className="py-3 text-sm text-muted-foreground">{tx.ledger}</td>
      <td className="py-3 text-sm">
        <div className="flex items-center gap-2 flex-wrap">
          <Badge variant="outline" className="capitalize font-normal">
            {formatEventName(tx.type)}
          </Badge>
          <span className="text-muted-foreground font-mono text-xs">
            {tx.summary}
          </span>
        </div>
      </td>
    </tr>
  );
}

// ---------------------------------------------------------------------------
// Transaction card (mobile) — Treasury tab
// ---------------------------------------------------------------------------

function TxCard({ tx, isNew }: { tx: TransactionEntry; isNew: boolean }) {
  return (
    <Card
      className={`${isNew ? "animate-in fade-in duration-500 border-green-500/30" : ""}`}
    >
      <CardContent className="grid grid-cols-2 gap-2 p-4 text-sm">
        <span className="text-muted-foreground">Time</span>
        <span>{formatTime(tx.createdAt)}</span>
        <span className="text-muted-foreground">Status</span>
        <Badge
          variant={tx.successful ? "secondary" : "destructive"}
          className="w-fit"
        >
          {tx.successful ? "Success" : "Failed"}
        </Badge>
        <span className="text-muted-foreground">Tx Hash</span>
        <a
          href={txExplorerUrl(tx.txHash)}
          target="_blank"
          rel="noopener noreferrer"
          className="inline-flex items-center gap-1 font-mono text-blue-400 hover:underline"
        >
          {truncateHash(tx.txHash)}
          <ExternalLink className="h-3 w-3" />
        </a>
        <span className="text-muted-foreground">Ledger</span>
        <span>{tx.ledger}</span>
        <span className="text-muted-foreground">Operation</span>
        <Badge variant="outline" className="w-fit capitalize font-normal">
          {formatEventName(tx.type)}
        </Badge>
        <span className="text-muted-foreground">Details</span>
        <span className="font-mono text-xs break-all">{tx.summary}</span>
      </CardContent>
    </Card>
  );
}

// ---------------------------------------------------------------------------
// Event row (desktop) — Contract tabs
// ---------------------------------------------------------------------------

function EventTableRow({ event, isNew }: { event: ContractEventEntry; isNew: boolean }) {
  return (
    <tr
      className={`border-b border-border/50 ${isNew ? "animate-in fade-in duration-500 bg-green-500/5" : ""}`}
    >
      <td className="py-3 pr-4 text-sm text-muted-foreground whitespace-nowrap align-top">
        {formatTime(event.createdAt)}
      </td>
      <td className="py-3 pr-4 align-top">
        <Badge variant="secondary" className="capitalize">
          {formatEventName(event.eventName)}
        </Badge>
      </td>
      <td className="py-3 pr-4 text-sm font-mono text-muted-foreground whitespace-nowrap align-top">
        <a
          href={accountExplorerUrl(event.initiator)}
          target="_blank"
          rel="noopener noreferrer"
          className="text-blue-400 hover:underline"
        >
          {truncateHash(event.initiator)}
        </a>
      </td>
      <td className="py-3 text-sm align-top max-w-0 w-full">
        <a
          href={opExplorerUrl(event.id)}
          target="_blank"
          rel="noopener noreferrer"
          className="font-mono text-xs text-blue-400 hover:underline break-all"
        >
          {formatEventArgs(event.topics, event.value)}
          <ExternalLink className="inline h-3 w-3 shrink-0 ml-1 align-text-bottom" />
        </a>
      </td>
    </tr>
  );
}

// ---------------------------------------------------------------------------
// Event card (mobile) — Contract tabs
// ---------------------------------------------------------------------------

function EventCard({ event, isNew }: { event: ContractEventEntry; isNew: boolean }) {
  return (
    <Card
      className={`${isNew ? "animate-in fade-in duration-500 border-green-500/30" : ""}`}
    >
      <CardContent className="grid grid-cols-2 gap-2 p-4 text-sm">
        <span className="text-muted-foreground">Time</span>
        <span>{formatTime(event.createdAt)}</span>
        <span className="text-muted-foreground">Event</span>
        <Badge variant="secondary" className="w-fit capitalize">
          {formatEventName(event.eventName)}
        </Badge>
        <span className="text-muted-foreground">Initiator</span>
        <a
          href={accountExplorerUrl(event.initiator)}
          target="_blank"
          rel="noopener noreferrer"
          className="font-mono text-blue-400 hover:underline"
        >
          {truncateHash(event.initiator)}
        </a>
        <span className="text-muted-foreground">Details</span>
        <a
          href={opExplorerUrl(event.id)}
          target="_blank"
          rel="noopener noreferrer"
          className="inline-flex items-start gap-1 font-mono text-xs text-blue-400 hover:underline break-all"
        >
          {formatEventArgs(event.topics, event.value)}
          <ExternalLink className="h-3 w-3 shrink-0 mt-0.5" />
        </a>
      </CardContent>
    </Card>
  );
}

// ---------------------------------------------------------------------------
// Main component
// ---------------------------------------------------------------------------

interface TransactionsTableProps {
  activeTab: TabId;
  onTabChange: (tab: TabId) => void;
  transactions: TransactionEntry[];
  events: ContractEventEntry[];
  isLoading: boolean;
  isLive: boolean;
  accountId?: string;
  newItemIds: Set<string>;
  onPrevious: () => void;
  onNext: () => void;
  hasPrevious: boolean;
  hasNext: boolean;
}

export function TransactionsTable({
  activeTab,
  onTabChange,
  transactions,
  events,
  isLoading,
  isLive,
  accountId,
  newItemIds,
  onPrevious,
  onNext,
  hasPrevious,
  hasNext,
}: TransactionsTableProps) {
  const tabLabel = TABS.find((t) => t.id === activeTab)?.label ?? "";
  const isContractTab = activeTab !== "treasury";
  const isEmpty = isContractTab ? events.length === 0 : transactions.length === 0;
  const hasContent = !isLoading && !isEmpty;

  return (
    <div>
      {/* Tab bar */}
      <div className="flex gap-1 rounded-lg bg-muted p-1 mb-6">
        {TABS.map((tab) => (
          <button
            key={tab.id}
            type="button"
            onClick={() => onTabChange(tab.id)}
            className={`flex-1 rounded-md px-3 py-2 text-sm font-medium transition-colors ${
              activeTab === tab.id
                ? "bg-background text-foreground shadow-sm"
                : "text-muted-foreground hover:text-foreground"
            }`}
          >
            {tab.label}
            {activeTab === tab.id && isLive && <LiveIndicator />}
          </button>
        ))}
      </div>

      {/* Info banner */}
      {accountId && <InfoBanner accountId={accountId} label={tabLabel} />}

      {/* Loading */}
      {isLoading && <LoadingSkeleton />}

      {/* Empty */}
      {!isLoading && isEmpty && <EmptyState />}

      {/* Content */}
      {hasContent && (
        <>
          {isContractTab ? (
            <>
              {/* Desktop table — contract events */}
              <div className="hidden md:block overflow-x-auto">
                <table className="w-full text-sm">
                  <thead>
                    <tr className="border-b border-border">
                      <th className="text-left py-2 pr-4 font-medium text-muted-foreground">
                        Time
                      </th>
                      <th className="text-left py-2 pr-4 font-medium text-muted-foreground">
                        Event
                      </th>
                      <th className="text-left py-2 pr-4 font-medium text-muted-foreground">
                        Initiator
                      </th>
                      <th className="text-left py-2 font-medium text-muted-foreground">
                        Details
                      </th>
                    </tr>
                  </thead>
                  <tbody>
                    {events.map((event) => (
                      <EventTableRow
                        key={event.id}
                        event={event}
                        isNew={newItemIds.has(event.id)}
                      />
                    ))}
                  </tbody>
                </table>
              </div>

              {/* Mobile cards — contract events */}
              <div className="md:hidden space-y-3">
                {events.map((event) => (
                  <EventCard
                    key={event.id}
                    event={event}
                    isNew={newItemIds.has(event.id)}
                  />
                ))}
              </div>
            </>
          ) : (
            <>
              {/* Desktop table — treasury transactions */}
              <div className="hidden md:block overflow-x-auto">
                <table className="w-full text-sm">
                  <thead>
                    <tr className="border-b border-border">
                      <th className="text-left py-2 font-medium text-muted-foreground">
                        Time
                      </th>
                      <th className="text-left py-2 font-medium text-muted-foreground">
                        Status
                      </th>
                      <th className="text-left py-2 font-medium text-muted-foreground">
                        Tx Hash
                      </th>
                      <th className="text-left py-2 font-medium text-muted-foreground">
                        Ledger
                      </th>
                      <th className="text-left py-2 font-medium text-muted-foreground">
                        Details
                      </th>
                    </tr>
                  </thead>
                  <tbody>
                    {transactions.map((tx) => (
                      <TxTableRow
                        key={tx.id}
                        tx={tx}
                        isNew={newItemIds.has(tx.id)}
                      />
                    ))}
                  </tbody>
                </table>
              </div>

              {/* Mobile cards — treasury transactions */}
              <div className="md:hidden space-y-3">
                {transactions.map((tx) => (
                  <TxCard
                    key={tx.id}
                    tx={tx}
                    isNew={newItemIds.has(tx.id)}
                  />
                ))}
              </div>
            </>
          )}

          {/* Pagination */}
          <div className="flex items-center justify-between mt-6">
            <Button
              variant="outline"
              size="sm"
              onClick={onPrevious}
              disabled={!hasPrevious}
            >
              Previous
            </Button>
            <Button
              variant="outline"
              size="sm"
              onClick={onNext}
              disabled={!hasNext}
            >
              Next
            </Button>
          </div>
        </>
      )}
    </div>
  );
}
