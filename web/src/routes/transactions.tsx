/**
 * Transactions page: tabbed view of Vault / Governance contract events and
 * Treasury account operations with per-tab cursor pagination.
 * @module routes/transactions
 */
import { useState, useCallback, useRef } from "react";
import {
  TransactionsTable,
  type TabId,
} from "@/components/transactions/TransactionsTable";
import { useContractEvents } from "@/hooks/use-contract-events";
import { useTransactions } from "@/hooks/use-transactions";
import { EVENTS_PAGE_SIZE } from "@/lib/constants";

const vaultContractId = import.meta.env.VITE_VAULT_CONTRACT_ID as
  | string
  | undefined;
const timelockContractId = import.meta.env.VITE_TIMELOCK_CONTRACT_ID as
  | string
  | undefined;
const treasuryAddress = import.meta.env.VITE_TREASURY_ADDRESS as
  | string
  | undefined;

/** Maps a tab id to the on-chain account or contract address it queries. */
function getAccountId(tab: TabId): string | undefined {
  if (tab === "vault") return vaultContractId;
  if (tab === "timelock") return timelockContractId;
  return treasuryAddress;
}

/**
 * Routed page that owns per-tab cursor state and delegates rendering to
 * `TransactionsTable`.
 * @remarks Cursors are tracked per tab in local state and a ref-based history
 * stack powers "Previous" navigation without making an extra RPC call. The
 * `newItemIds` set is cleared on every tab switch and pagination action so
 * the fade-in animation only triggers for genuinely new streamed items.
 */
export default function TransactionsPage() {
  const [activeTab, setActiveTab] = useState<TabId>("vault");

  // Per-tab cursor state for pagination
  const [vaultCursor, setVaultCursor] = useState<string | undefined>();
  const [timelockCursor, setTimelockCursor] = useState<string | undefined>();
  const [treasuryCursor, setTreasuryCursor] = useState<string | undefined>();

  // Cursor history stacks for "Previous" navigation
  const vaultHistory = useRef<string[]>([]);
  const timelockHistory = useRef<string[]>([]);
  const treasuryHistory = useRef<string[]>([]);

  // Track newly streamed item IDs for highlight animation
  const [newItemIds, setNewItemIds] = useState<Set<string>>(new Set());

  // Contract event queries (Stellar Expert API)
  const vault = useContractEvents(vaultContractId, "vault", vaultCursor);
  const timelock = useContractEvents(timelockContractId, "timelock", timelockCursor);

  // Treasury transaction query (Horizon API + SSE streaming)
  const treasury = useTransactions(treasuryAddress, "treasury", treasuryCursor);

  // Active tab data
  const currentCursor =
    activeTab === "vault"
      ? vaultCursor
      : activeTab === "timelock"
        ? timelockCursor
        : treasuryCursor;

  const currentHistory =
    activeTab === "vault"
      ? vaultHistory
      : activeTab === "timelock"
        ? timelockHistory
        : treasuryHistory;

  const setCursor =
    activeTab === "vault"
      ? setVaultCursor
      : activeTab === "timelock"
        ? setTimelockCursor
        : setTreasuryCursor;

  // Determine active query and data based on tab type
  const isContractTab = activeTab !== "treasury";
  const activeQuery = isContractTab
    ? activeTab === "vault"
      ? vault
      : timelock
    : treasury;

  const transactions = isContractTab
    ? []
    : (treasury.data?.transactions ?? []);
  const events = isContractTab
    ? ((activeTab === "vault" ? vault : timelock).data?.events ?? [])
    : [];

  const responseCursor = isContractTab
    ? (activeTab === "vault" ? vault : timelock).data?.cursor
    : treasury.data?.cursor;

  const itemCount = isContractTab ? events.length : transactions.length;
  const isLive = !currentCursor;
  const hasNext = itemCount >= EVENTS_PAGE_SIZE;

  const handleTabChange = useCallback((tab: TabId) => {
    setActiveTab(tab);
    setNewItemIds(new Set());
  }, []);

  const handleNext = useCallback(() => {
    if (!responseCursor) return;
    currentHistory.current.push(currentCursor ?? "");
    setCursor(responseCursor);
    setNewItemIds(new Set());
  }, [responseCursor, currentCursor, currentHistory, setCursor]);

  const handlePrevious = useCallback(() => {
    const prev = currentHistory.current.pop();
    setCursor(prev === "" ? undefined : prev);
    setNewItemIds(new Set());
  }, [currentHistory, setCursor]);

  return (
    <main className="container py-8">
      <section className="mb-8">
        <h1 className="text-3xl md:text-4xl font-bold mb-3">Transactions</h1>
        <p className="text-muted-foreground text-lg max-w-2xl">
          Live contract and treasury account activity on the Stellar Network.
        </p>
      </section>

      {/* Error state */}
      {activeQuery.error && (
        <div className="rounded-lg border border-destructive/50 bg-destructive/5 p-6 mb-8">
          <h3 className="font-semibold text-destructive mb-2">
            Failed to load data
          </h3>
          <p className="text-sm text-muted-foreground">
            {activeQuery.error.message}
          </p>
        </div>
      )}

      <TransactionsTable
        activeTab={activeTab}
        onTabChange={handleTabChange}
        transactions={transactions}
        events={events}
        isLoading={activeQuery.isLoading}
        isLive={isLive}
        accountId={getAccountId(activeTab)}
        newItemIds={newItemIds}
        onPrevious={handlePrevious}
        onNext={handleNext}
        hasPrevious={currentHistory.current.length > 0}
        hasNext={hasNext}
      />
    </main>
  );
}
