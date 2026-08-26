/**
 * Tabbed table for the Transactions page: Treasury operations (Horizon) and
 * Vault / Governance contract events (Soroban RPC), with desktop-table and
 * mobile-card renderers plus live-update highlighting and pagination.
 * @module components/transactions/TransactionsTable
 */
import { ArrowRight, ArrowSquareOut, Info } from "@phosphor-icons/react";
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

/**
 * Renders an operation summary, drawing any `→` in it as an icon.
 *
 * The summary is a plain string built in `lib/stellar.ts`, and the arrow used
 * to ride along inside it. Rendered in a monospace face at 11-12px in muted
 * grey, U+2192 either fell back to a substitute glyph or sat on the baseline
 * rather than the x-height centre, so it read as a stray dash between the
 * amount and the destination. An inline SVG has no glyph coverage to depend on
 * and `items-center` puts it on the same optical line as the text either side.
 */
function OpSummary({ text, className }: { text: string; className?: string }) {
  const parts = text.split("→");

  if (parts.length === 1) {
    return <span className={className}>{text}</span>;
  }

  return (
    <span className={`inline-flex flex-wrap items-center gap-x-1.5 gap-y-1 ${className ?? ""}`}>
      {parts.map((part, i) => (
        // biome-ignore lint/suspicious/noArrayIndexKey: split of a fixed string, never reordered
        <span key={i} className="inline-flex items-center gap-x-1.5">
          {i > 0 && <ArrowRight weight="bold" className="h-3 w-3 shrink-0 text-foreground/70" aria-label="to" />}
          <span className="break-all">{part.trim()}</span>
        </span>
      ))}
    </span>
  );
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
    <span className="inline-flex items-center gap-1.5 ml-1 sm:ml-2">
      <span className="relative flex h-2 w-2">
        <span className="absolute inline-flex h-full w-full animate-ping rounded-full bg-green-400 opacity-75" />
        <span className="relative inline-flex h-2 w-2 rounded-full bg-green-500" />
      </span>
      <span className="hidden sm:inline text-[10px] font-mono uppercase tracking-wider text-green-400">Live</span>
    </span>
  );
}

function InfoBanner({ accountId, label }: { accountId: string; label: string }) {
  const isContract = label !== "Treasury";
  const href = isContract ? contractExplorerUrl(accountId) : accountExplorerUrl(accountId);

  return (
    <div className="flex items-start gap-3 rounded-lg border border-border/60 bg-muted/10 px-4 py-3.5 mb-6">
      <Info className="h-4 w-4 text-muted-foreground mt-0.5 shrink-0" />
      <p className="text-sm text-muted-foreground leading-relaxed">
        {isContract ? `Recent ${label.toLowerCase()} events` : `${label} transactions`} for{" "}
        <a
          href={href}
          target="_blank"
          rel="noopener noreferrer"
          className="font-mono text-foreground underline decoration-border hover:decoration-foreground transition-all duration-200"
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
    <div className="space-y-3" role="status" aria-live="polite" aria-label="Loading transaction history details...">
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
      className={`border-b border-border/40 hover:bg-muted/5 transition-colors ${isNew ? "animate-in fade-in duration-500 bg-green-500/5" : ""}`}
    >
      <td className="py-4 text-sm text-muted-foreground whitespace-nowrap">
        {formatTime(tx.createdAt)}
      </td>
      <td className="py-4">
        <Badge variant={tx.successful ? "secondary" : "destructive"} className="rounded-md font-mono text-[10px] uppercase tracking-wider px-2 py-0.5">
          {tx.successful ? "Success" : "Failed"}
        </Badge>
      </td>
      <td className="py-4">
        <a
          href={txExplorerUrl(tx.txHash)}
          target="_blank"
          rel="noopener noreferrer"
          className="inline-flex items-center gap-1 text-sm font-mono text-foreground underline decoration-border hover:decoration-foreground transition-all duration-200"
        >
          {truncateHash(tx.txHash)}
          <ArrowSquareOut className="h-3 w-3 text-muted-foreground shrink-0" />
        </a>
      </td>
      <td className="py-4 text-sm font-mono text-muted-foreground">{tx.ledger}</td>
      <td className="py-4 text-sm">
        <div className="flex items-center gap-2 flex-wrap">
          <Badge variant="outline" className="capitalize font-normal rounded-md text-xs py-0.5 px-2">
            {formatEventName(tx.type)}
          </Badge>
          <OpSummary
            text={tx.summary}
            className="text-muted-foreground font-mono text-xs"
          />
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
      className={`border border-border/60 bg-card/40 ${isNew ? "animate-in fade-in duration-500 border-green-500/30" : ""}`}
    >
      <CardContent className="grid grid-cols-2 gap-y-3 gap-x-4 p-5 text-xs">
        <span className="text-muted-foreground font-mono uppercase tracking-wider text-[10px]">Time</span>
        <span className="text-foreground font-medium">{formatTime(tx.createdAt)}</span>

        <span className="text-muted-foreground font-mono uppercase tracking-wider text-[10px]">Status</span>
        <Badge
          variant={tx.successful ? "secondary" : "destructive"}
          className="w-fit rounded-md font-mono text-[9px] uppercase tracking-wider px-2 py-0.5"
        >
          {tx.successful ? "Success" : "Failed"}
        </Badge>

        <span className="text-muted-foreground font-mono uppercase tracking-wider text-[10px]">Tx Hash</span>
        <a
          href={txExplorerUrl(tx.txHash)}
          target="_blank"
          rel="noopener noreferrer"
          className="inline-flex items-center gap-1 font-mono text-foreground underline decoration-border hover:decoration-foreground transition-all duration-200"
        >
          {truncateHash(tx.txHash)}
          <ArrowSquareOut className="h-3 w-3 text-muted-foreground shrink-0" />
        </a>

        <span className="text-muted-foreground font-mono uppercase tracking-wider text-[10px]">Ledger</span>
        <span className="font-mono text-foreground">{tx.ledger}</span>

        <span className="text-muted-foreground font-mono uppercase tracking-wider text-[10px]">Operation</span>
        <Badge
          variant="outline"
          className="w-fit max-w-full capitalize font-normal whitespace-normal break-words text-left rounded-md px-2 py-0.5"
        >
          {formatEventName(tx.type)}
        </Badge>

        <span className="text-muted-foreground font-mono uppercase tracking-wider text-[10px] self-start mt-0.5">Details</span>
        <OpSummary
          text={tx.summary}
          className="font-mono text-[11px] text-foreground/90 bg-muted/20 border border-border/30 rounded px-2 py-1.5"
        />
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
      className={`border-b border-border/40 hover:bg-muted/5 transition-colors ${isNew ? "animate-in fade-in duration-500 bg-green-500/5" : ""}`}
    >
      <td className="py-4 pr-4 text-sm text-muted-foreground whitespace-nowrap align-top">
        {formatTime(event.createdAt)}
      </td>
      <td className="py-4 pr-4 align-top">
        <Badge variant="secondary" className="capitalize rounded-md font-mono text-[10px] uppercase tracking-wider px-2 py-0.5">
          {formatEventName(event.eventName)}
        </Badge>
      </td>
      <td className="py-4 text-sm align-top max-w-0 w-full">
        <a
          href={opExplorerUrl(event.id)}
          target="_blank"
          rel="noopener noreferrer"
          className="font-mono text-xs text-foreground underline decoration-border/80 hover:decoration-foreground transition-all duration-200 break-all leading-relaxed"
        >
          {formatEventArgs(event.topics, event.value)}
          <ArrowSquareOut className="inline h-3.5 w-3.5 shrink-0 ml-1 text-muted-foreground align-text-bottom" />
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
      className={`border border-border/60 bg-card/40 ${isNew ? "animate-in fade-in duration-500 border-green-500/30" : ""}`}
    >
      <CardContent className="grid grid-cols-2 gap-y-3 gap-x-4 p-5 text-xs">
        <span className="text-muted-foreground font-mono uppercase tracking-wider text-[10px]">Time</span>
        <span className="text-foreground font-medium">{formatTime(event.createdAt)}</span>

        <span className="text-muted-foreground font-mono uppercase tracking-wider text-[10px]">Event</span>
        <Badge
          variant="secondary"
          className="w-fit max-w-full capitalize whitespace-normal break-words text-left rounded-md font-mono text-[9px] uppercase tracking-wider px-2 py-0.5"
        >
          {formatEventName(event.eventName)}
        </Badge>

        <span className="text-muted-foreground font-mono uppercase tracking-wider text-[10px] self-start mt-0.5">Details</span>
        <a
          href={opExplorerUrl(event.id)}
          target="_blank"
          rel="noopener noreferrer"
          className="font-mono text-[11px] text-foreground underline decoration-border hover:decoration-foreground transition-all duration-200 break-all bg-muted/20 border border-border/30 rounded px-2 py-1.5 leading-relaxed"
        >
          {formatEventArgs(event.topics, event.value)}
          <ArrowSquareOut className="h-3.5 w-3.5 shrink-0 text-muted-foreground inline-block ml-1 align-text-top" />
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

/**
 * Renders the full tabbed transactions view.
 */
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
            className={`flex-1 rounded-md py-2 px-1 text-[10px] sm:text-xs font-semibold uppercase tracking-normal sm:tracking-wider transition-all duration-200 cursor-pointer ${
              activeTab === tab.id
                ? "bg-background text-foreground shadow-sm animate-fade-in"
                : "text-muted-foreground hover:text-foreground"
            }`}
          >
            <span className="inline-flex items-center gap-1.5 sm:gap-2">
              {tab.label}
              {activeTab === tab.id && isLive && <LiveIndicator />}
            </span>
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
                    <tr className="border-b border-border/80">
                      <th className="text-left pb-3 pr-4 font-mono text-[10px] uppercase tracking-wider text-muted-foreground">
                        Time
                      </th>
                      <th className="text-left pb-3 pr-4 font-mono text-[10px] uppercase tracking-wider text-muted-foreground">
                        Event
                      </th>
                      <th className="text-left pb-3 font-mono text-[10px] uppercase tracking-wider text-muted-foreground">
                        Details
                      </th>
                    </tr>
                  </thead>
                  <tbody className="divide-y divide-border/40">
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
              <div className="md:hidden space-y-3.5">
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
                    <tr className="border-b border-border/80">
                      <th className="text-left pb-3 font-mono text-[10px] uppercase tracking-wider text-muted-foreground">
                        Time
                      </th>
                      <th className="text-left pb-3 font-mono text-[10px] uppercase tracking-wider text-muted-foreground">
                        Status
                      </th>
                      <th className="text-left pb-3 font-mono text-[10px] uppercase tracking-wider text-muted-foreground">
                        Tx Hash
                      </th>
                      <th className="text-left pb-3 font-mono text-[10px] uppercase tracking-wider text-muted-foreground">
                        Ledger
                      </th>
                      <th className="text-left pb-3 font-mono text-[10px] uppercase tracking-wider text-muted-foreground">
                        Details
                      </th>
                    </tr>
                  </thead>
                  <tbody className="divide-y divide-border/40">
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
              <div className="md:hidden space-y-3.5">
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
          <div className="flex items-center justify-between border-t border-border pt-6 mt-8">
            <Button
              variant="outline"
              size="sm"
              onClick={onPrevious}
              disabled={!hasPrevious}
              className="font-mono text-xs uppercase tracking-wider cursor-pointer"
            >
              Previous
            </Button>
            <Button
              variant="outline"
              size="sm"
              onClick={onNext}
              disabled={!hasNext}
              className="font-mono text-xs uppercase tracking-wider cursor-pointer"
            >
              Next
            </Button>
          </div>
        </>
      )}
    </div>
  );
}
