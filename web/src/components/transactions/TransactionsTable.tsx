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

/** Decode a contract event's argument topics into a readable, comma-separated
 *  list. The first topic (event name) is dropped — it's already shown as the
 *  Event badge in its own column. */
function formatEventArgs(topics: string[]): string {
  const args = topics.slice(1);
  if (args.length === 0) return "—";
  return args.map(formatTopicArg).join(", ");
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
      <td className="py-3 text-sm text-muted-foreground whitespace-nowrap">
        {formatTime(event.createdAt)}
      </td>
      <td className="py-3">
        <Badge variant="secondary" className="capitalize">
          {formatEventName(event.eventName)}
        </Badge>
      </td>
      <td className="py-3 text-sm font-mono text-muted-foreground">
        <a
          href={accountExplorerUrl(event.initiator)}
          target="_blank"
          rel="noopener noreferrer"
          className="text-blue-400 hover:underline"
        >
          {truncateHash(event.initiator)}
        </a>
      </td>
      <td className="py-3 text-sm">
        <a
          href={opExplorerUrl(event.id)}
          target="_blank"
          rel="noopener noreferrer"
          className="inline-flex items-center gap-1 font-mono text-xs text-blue-400 hover:underline"
        >
          {formatEventArgs(event.topics)}
          <ExternalLink className="h-3 w-3 shrink-0" />
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
          {formatEventArgs(event.topics)}
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
                      <th className="text-left py-2 font-medium text-muted-foreground">
                        Time
                      </th>
                      <th className="text-left py-2 font-medium text-muted-foreground">
                        Event
                      </th>
                      <th className="text-left py-2 font-medium text-muted-foreground">
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
