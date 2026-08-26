package mgpoller

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/Shamba-Records-Limited/microvault/pkg/payment/moneygram"
	"github.com/Shamba-Records-Limited/microvault/pkg/payment/stellaranchor"
)

// This file holds the deposit direction: cash in, an agent counter to
// MoneyGram to the treasury, settling a borrower's loan. It is the second
// implementation of Runner's Driver.
//
// The two directions differ in more than sign. A withdrawal is something we
// started and MoneyGram must finish, so it is polled hard until it resolves. A
// deposit is something the borrower must finish and nothing on our side
// compels them to: the window is four to five days, most of which they spend
// doing nothing. Cadence therefore lives in a repayment_next_poll_at column
// rather than in the ticker — a restart would lose in-memory timers, and a
// 30-second tick against MoneyGram for three days is tens of thousands of
// pointless requests.

// RepaymentRecord is the projection of a loan row with a borrower repayment in
// flight. The lending module builds these from rows whose repayment_status is
// one of the live states and whose repayment_next_poll_at is due.
type RepaymentRecord struct {
	LoanID        string
	SequenceID    string // = loans.ramp_sequence_id; the handle notifications use
	MoneyGramTxID string // = loans.repayment_mg_tx_id

	// ChildAccountIndex re-derives the SEP-10 memo. The deposit was created
	// under a JWT scoped to this borrower, which is what attributes the
	// MoneyGram transaction to them — not any field on the transaction.
	ChildAccountIndex uint32

	// BorrowerAddress is the child account's Stellar address, carried onto the
	// vault's Repaid event by repay_for. Attribution only: the treasury is the
	// payer and the borrower authorizes nothing.
	BorrowerAddress string

	// PayoffStroops is the payoff quoted and locked at initiation. Borrow-index
	// movement since then does not change what the borrower owes.
	PayoffStroops int64

	// VaultAttempts is how many times the treasury-to-vault leg has already
	// failed for this loan. Durable, so a restart does not reset the ceiling.
	VaultAttempts int

	// ReferenceSent marks the one SMS carrying MoneyGram's deposit reference,
	// which is what the borrower quotes at the counter to pay in.
	ReferenceSent bool

	RepaymentStatus string
	ExpiresAt       time.Time
	ReminderSent    bool
	PhoneNumber     string
	UserID          string
}

// Repayment status strings the deposit driver writes. These match the
// LoanRepaymentStatus* constants in the lending module's loan model, hardcoded
// here for the same reason the disbursement ones are: this package cannot
// import the lending module without a layering inversion.
const (
	repaymentInitiated     = "initiated"
	repaymentFundsReceived = "funds_received"
	repaymentSettled       = "settled"
	repaymentExpired       = "expired"
	repaymentFailed        = "failed"
)

// RepaymentFetcher returns the repayments due for evaluation this tick.
// Implementations select loans whose repayment_status is initiated or
// funds_received and whose repayment_next_poll_at has passed.
type RepaymentFetcher interface {
	GetDueRepayments(ctx context.Context, limit int) ([]RepaymentRecord, error)
}

// RepaymentRecorder writes the deposit driver's state transitions.
//
// Each method is the whole of one transition, so a crash between two of them
// leaves the loan in a state the next tick can resume from rather than a
// half-applied one.
type RepaymentRecorder interface {
	// RecordDepositUpdate persists the latest fields from a polled deposit
	// transaction. Idempotent.
	RecordDepositUpdate(ctx context.Context, loanID string, tx *stellaranchor.Transaction) error

	// MarkFundsReceived records that the borrower's cash reached the treasury
	// as USDC. Writes repayment_status=funds_received and the anchor's Stellar
	// transaction. loans.status deliberately does not move yet: the borrower is
	// told at this point, and the vault leg may still be retrying.
	MarkFundsReceived(ctx context.Context, loanID string, tx *stellaranchor.Transaction) error

	// MarkSettled records the confirmed treasury-to-vault leg. This is the
	// transition that flips loans.status to repaid.
	MarkSettled(ctx context.Context, loanID string, vaultTxHash string) error

	// MarkExpired releases the quote lock after the cash-in window elapsed
	// without the borrower paying, returning the loan to its prior status.
	MarkExpired(ctx context.Context, loanID string) error

	// MarkFailed ends the rail before any funds moved. Never used for a failed
	// vault leg: money already on the treasury stays at funds_received so
	// reconciliation keeps retrying.
	MarkFailed(ctx context.Context, loanID string, reason string) error

	// MarkReminderSent is written before the SMS, so a failed send is not
	// retried on every tick. It records a notification, not a movement of
	// money, which is why it needs its own marker.
	MarkReminderSent(ctx context.Context, loanID string) error

	// ScheduleNextPoll sets when this repayment should next be looked at.
	ScheduleNextPoll(ctx context.Context, loanID string, at time.Time) error

	// MarkReferenceSent stamps the deposit-reference SMS before it is sent, so
	// a failing provider is not retried on every tick for the rest of the
	// window. Same reasoning as MarkReminderSent.
	MarkReferenceSent(ctx context.Context, loanID string) error

	// RecordVaultAttempt increments the failed-vault-leg counter. Written
	// after each failure so the count survives a restart, which is what makes
	// the escalation ceiling meaningful.
	RecordVaultAttempt(ctx context.Context, loanID string, attempts int) error
}

// VaultRepayer settles the on-chain leg, treasury to vault, attributed to the
// borrower.
type VaultRepayer interface {
	RepayForBorrower(ctx context.Context, loanID, borrowerAddress string, amountStroops int64) (txHash string, err error)
}

// RepaymentNotifier sends the borrower-facing messages for the cash-in rail.
// Optional: a nil notifier logs instead, which keeps the state machine
// testable without a notification stack.
type RepaymentNotifier interface {
	// NotifyRepaymentReference carries MoneyGram's deposit reference once the
	// borrower has committed in the webview. Without it they reach the counter
	// with nothing to quote.
	NotifyRepaymentReference(loanID, reference string) error

	// NotifyRepaymentMoreInfo carries MoneyGram's transaction page instead,
	// for the case where no reference has been issued. See sendPayInstructions.
	NotifyRepaymentMoreInfo(loanID string) error

	NotifyRepaymentReceived(loanID string) error
	NotifyRepaymentReminder(loanID string) error
	NotifyRepaymentExpired(loanID string) error
}

// DepositDriver drives the borrower repayment cash-in state machine.
type DepositDriver struct {
	client   *moneygram.Client
	fetcher  RepaymentFetcher
	recorder RepaymentRecorder
	vault    VaultRepayer
	notifier RepaymentNotifier
	alerts   AlertService
	cfg      PollerConfig
	logger   *slog.Logger
	now      func() time.Time

	runner *Runner[RepaymentRecord]
}

// DepositDriverDeps are the collaborators and settings a DepositDriver needs.
// Client, Fetcher, Recorder and Vault are required; Notifier, Alerts and
// Logger may be nil.
type DepositDriverDeps struct {
	Client   *moneygram.Client
	Fetcher  RepaymentFetcher
	Recorder RepaymentRecorder
	Vault    VaultRepayer
	Notifier RepaymentNotifier
	Alerts   AlertService
	Config   PollerConfig
	Logger   *slog.Logger
}

// NewDepositDriver validates the dependencies and returns a DepositDriver.
func NewDepositDriver(deps DepositDriverDeps) (*DepositDriver, error) {
	const direction = "deposit"

	client, fetcher := deps.Client, deps.Fetcher
	recorder, vault := deps.Recorder, deps.Vault

	if missing, found := firstMissing([]dep{
		{"moneygram_client", client == nil},
		{"repayment_fetcher", fetcher == nil},
		{"repayment_recorder", recorder == nil},
		{"vault_repayer", vault == nil},
	}); found {
		return nil, missingDep(direction, missing.name)
	}

	cfg := deps.Config.withDepositDefaults()
	logger := deps.Logger
	if logger == nil {
		logger = slog.Default()
	}

	d := &DepositDriver{
		client:   client,
		fetcher:  fetcher,
		recorder: recorder,
		vault:    vault,
		notifier: deps.Notifier,
		alerts:   deps.Alerts,
		cfg:      cfg,
		logger:   logger.With("component", "mgpoller"),
		now:      time.Now,
	}
	// Two-step because the runner's Driver is the driver itself.
	d.runner = NewRunner(RunnerDeps[RepaymentRecord]{
		Direction: direction,
		Interval:  cfg.DepositPollInterval,
		MaxBatch:  cfg.DepositMaxBatch,
		Fetcher:   FetchFunc[RepaymentRecord](fetcher.GetDueRepayments),
		Driver:    d,
		Logger:    d.logger,
	})
	return d, nil
}

// Start runs the deposit driver until ctx is cancelled.
func (d *DepositDriver) Start(ctx context.Context) { d.runner.Start(ctx) }

// poll runs a single deposit cycle.
func (d *DepositDriver) poll(ctx context.Context) { d.runner.poll(ctx) }

// Drive evaluates one repayment and applies the appropriate transition.
// Errors are logged but never abort the batch.
func (d *DepositDriver) Drive(ctx context.Context, rec RepaymentRecord) {
	if rec.MoneyGramTxID == "" {
		d.logger.Warn("repayment has no MoneyGram transaction id, skipping",
			"loan_id", rec.LoanID, "sequence_id", rec.SequenceID)
		return
	}

	childMemo := stellaranchor.ChildAccountMemo(d.client.TreasuryAddress(), rec.ChildAccountIndex)
	tx, err := d.client.GetTransaction(ctx, childMemo, rec.MoneyGramTxID)
	if err != nil {
		d.logger.Error("GetTransaction failed",
			"loan_id", rec.LoanID, "mg_tx_id", rec.MoneyGramTxID, "error", err)
		// Reschedule anyway: without this a transient MoneyGram outage would
		// leave next_poll_at in the past and the row spinning every tick.
		d.reschedule(ctx, rec, d.cfg.DepositIdleBackoff)
		return
	}

	// Unconditional: every branch below can return without logging, which
	// makes a parked transaction indistinguishable from a stalled poller.
	d.logger.Info("MoneyGram deposit polled",
		"loan_id", rec.LoanID,
		"mg_tx_id", rec.MoneyGramTxID,
		"status", tx.Status,
		"mg_stellar_tx_id", tx.StellarTransactionID,
		"to", tx.To,
		"external_transaction_id", tx.ExternalTransactionID,
		"deposit_memo", tx.DepositMemo,
		"deposit_memo_type", tx.DepositMemoType,
		"more_info_url", tx.MoreInfoURL,
		"message", tx.Message,
	)

	// MoneyGram states its own deadline, and it is the one that binds: the
	// deposit lapses on user_action_required_by whatever our configured window
	// says. Observed at roughly 24 hours against a REPAYMENT_WINDOW default of
	// 96, so acting on ours would keep a dead deposit live for three days and
	// tell the borrower they still had time.
	//
	// Applied to this tick's copy as well as persisted, so expiry and the
	// reminder use the real deadline now rather than one poll later.
	if deadline, ok := parseAnchorTime(tx.UserActionRequiredBy); ok {
		rec.ExpiresAt = deadline
	}

	if err := d.recorder.RecordDepositUpdate(ctx, rec.LoanID, tx); err != nil {
		d.logger.Warn("failed to persist deposit update",
			"loan_id", rec.LoanID, "error", err)
	}

	// Checked on every tick rather than inside one status branch. Tying the
	// send to a nominated status means guessing which one the artifact arrives
	// at. Any tick that sees one and no marker sends it.
	d.sendPayInstructionsOnce(ctx, rec, tx)

	switch tx.Status {
	case stellaranchor.StatusIncomplete:
		// The borrower has not finished the webview. This is where a deposit
		// spends most of its life, and the only status the window expires from
		// — a borrower who has committed and is standing at a counter must not
		// have the quote pulled out from under them.
		d.handleIncomplete(ctx, rec, tx)

	case stellaranchor.StatusPendingUserTransferStart:
		// Committed: agent chosen, walking to the counter.
		// They tend to pay within the hour, so poll on the active cadence.
		d.reschedule(ctx, rec, d.cfg.DepositActiveBackoff)

	case stellaranchor.StatusCompleted:
		d.handleCompleted(ctx, rec, tx)

	case stellaranchor.StatusRefunded:
		// MoneyGram returned the borrower's cash. Their money is back and the
		// loan is untouched, so this ends the rail rather than failing a debt.
		d.failRepayment(ctx, rec, "MoneyGram refunded the deposit: "+tx.Message)

	case stellaranchor.StatusTooSmall, stellaranchor.StatusTooLarge:
		// Our own floor should have kept us out of these. Alert as well as
		// fail: it means the amount gate and MoneyGram's limits disagree.
		d.alertOps("MoneyGram deposit outside anchor limits",
			fmt.Sprintf("Loan %s: MG reported %s for a %d stroop payoff. Check the repayment amount gate against MG's deposit limits. Message: %s",
				rec.LoanID, tx.Status, rec.PayoffStroops, tx.Message))
		d.failRepayment(ctx, rec, fmt.Sprintf("MoneyGram reported %s", tx.Status))

	case stellaranchor.StatusExpired, stellaranchor.StatusNoMarket, stellaranchor.StatusError:
		d.failRepayment(ctx, rec, fmt.Sprintf("MoneyGram reported %s: %s", tx.Status, tx.Message))

	case stellaranchor.StatusOnHold:
		d.logger.Warn("MoneyGram deposit on hold",
			"loan_id", rec.LoanID, "mg_tx_id", rec.MoneyGramTxID, "message", tx.Message)
		d.alertOps("MoneyGram deposit on hold",
			fmt.Sprintf("Loan %s: MG on_hold for additional checks. Message: %s",
				rec.LoanID, tx.Message))
		d.reschedule(ctx, rec, d.cfg.DepositActiveBackoff)

	case stellaranchor.StatusPendingUserTransferComplete,
		stellaranchor.StatusPendingAnchor,
		stellaranchor.StatusPendingExternal,
		stellaranchor.StatusPendingStellar,
		stellaranchor.StatusPendingTrust,
		stellaranchor.StatusPendingUser:
		// In flight on someone else's side. Status is already logged above.
		d.reschedule(ctx, rec, d.cfg.DepositActiveBackoff)

	default:
		d.logger.Warn("unexpected MoneyGram deposit status",
			"loan_id", rec.LoanID, "status", tx.Status)
		d.reschedule(ctx, rec, d.cfg.DepositIdleBackoff)
	}
}

// handleIncomplete covers the borrower who has not committed yet: expire the
// window if it has run out, otherwise remind them once as it approaches.
func (d *DepositDriver) handleIncomplete(ctx context.Context, rec RepaymentRecord, tx *stellaranchor.Transaction) {
	now := d.now()

	if !rec.ExpiresAt.IsZero() && !now.Before(rec.ExpiresAt) {
		d.expireRepayment(ctx, rec)
		return
	}

	if d.shouldRemind(rec, tx, now) {
		// Marker first: a failed send must not be retried every tick.
		if err := d.recorder.MarkReminderSent(ctx, rec.LoanID); err != nil {
			d.logger.Error("failed to mark repayment reminder sent, not sending",
				"loan_id", rec.LoanID, "error", err)
		} else {
			d.notify("reminder", rec, func(n RepaymentNotifier) error {
				return n.NotifyRepaymentReminder(rec.LoanID)
			})
		}
	}

	d.reschedule(ctx, rec, d.cfg.DepositIdleBackoff)
}

// shouldRemind reports whether the single pre-expiry reminder is due.
//
// The lead time is capped at a quarter of the window that actually applies.
// DepositReminderBefore defaults to 24h, which was sized for a four-day window;
// against MoneyGram's real 24-hour deadline that would put the reminder at the
// moment of initiation, where it says nothing the borrower was not just told.
func (d *DepositDriver) shouldRemind(rec RepaymentRecord, tx *stellaranchor.Transaction, now time.Time) bool {
	if rec.ReminderSent || rec.ExpiresAt.IsZero() || d.cfg.DepositReminderBefore <= 0 {
		return false
	}
	return !now.Before(rec.ExpiresAt.Add(-d.reminderLead(rec, tx)))
}

// reminderLead is how long before expiry the reminder goes out.
//
// Capped at a fraction of the whole window, measured from when MoneyGram
// started the deposit — not from the time remaining, which would make the
// threshold chase itself and only ever be met at expiry.
//
// Falls back to the configured lead when started_at is missing, which is the
// old behaviour and safe for the long windows it was sized for.
func (d *DepositDriver) reminderLead(rec RepaymentRecord, tx *stellaranchor.Transaction) time.Duration {
	lead := d.cfg.DepositReminderBefore

	startedAt, ok := parseAnchorTime(tx.StartedAt)
	if !ok {
		return lead
	}
	window := rec.ExpiresAt.Sub(startedAt)
	if window <= 0 {
		return lead
	}
	if capped := window / reminderLeadDivisor; lead > capped {
		lead = capped
	}
	return lead
}

// reminderLeadDivisor caps the reminder lead at this fraction of the remaining
// window. A quarter leaves the borrower most of the window before being
// nudged, and still gives them hours to act afterwards.
const reminderLeadDivisor = 4

// parseAnchorTime reads a SEP-24 timestamp. Anchors send RFC 3339; anything
// else is ignored rather than guessed at, since a misparsed deadline would
// expire a live deposit.
func parseAnchorTime(s string) (time.Time, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}, false
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return time.Time{}, false
	}
	return t, true
}

// handleCompleted settles a deposit whose cash reached the treasury.
//
// Order matters and is deliberate. The borrower is told the moment the funds
// land, before the vault leg is attempted, because from their side the
// repayment is done — the treasury-to-vault transfer is our leg, and a retry
// on it is not something they should hear about.
func (d *DepositDriver) handleCompleted(ctx context.Context, rec RepaymentRecord, tx *stellaranchor.Transaction) {
	if rec.RepaymentStatus != repaymentFundsReceived {
		if err := d.recorder.MarkFundsReceived(ctx, rec.LoanID, tx); err != nil {
			d.logger.Error("failed to mark funds received, retrying next tick",
				"loan_id", rec.LoanID, "error", err)
			d.reschedule(ctx, rec, d.cfg.DepositActiveBackoff)
			return
		}
		d.notify("received", rec, func(n RepaymentNotifier) error {
			return n.NotifyRepaymentReceived(rec.LoanID)
		})
	}

	if rec.BorrowerAddress == "" {
		// Without the borrower we could still call plain repay, but that would
		// silently drop the attribution the whole rail exists to produce.
		d.logger.Error("repayment has no borrower address, cannot attribute vault repay",
			"loan_id", rec.LoanID)
		d.alertOps("Repayment missing borrower address",
			fmt.Sprintf("Loan %s: funds received but no child account address to attribute repay_for. Vault leg withheld.", rec.LoanID))
		d.reschedule(ctx, rec, d.cfg.DepositActiveBackoff)
		return
	}
	if rec.PayoffStroops <= 0 {
		d.logger.Error("repayment has no locked payoff, cannot repay vault",
			"loan_id", rec.LoanID, "payoff_stroops", rec.PayoffStroops)
		d.alertOps("Repayment missing locked payoff",
			fmt.Sprintf("Loan %s: funds received but repayment_payoff_stroops is %d. Vault leg withheld.", rec.LoanID, rec.PayoffStroops))
		d.reschedule(ctx, rec, d.cfg.DepositActiveBackoff)
		return
	}

	// Observe what actually arrived before repaying. SEP-24 defines amount_out
	// as amount_in less fee.total, so a fee-bearing corridor credits the
	// treasury with less than the borrower was quoted — and MoneyGram charges
	// 3.00 USD on a 23.40 deposit in the sandbox.
	//
	// The repay still uses the quoted payoff. Whether amount_out is genuinely
	// net of the fee is unconfirmed: the only payload seen so far predates the
	// borrower paying, and reported amount_out_asset as fiat rather than USDC,
	// so it cannot be read as settled. Changing what is repaid on that basis
	// would be guessing. This records the discrepancy so the first completed
	// deposit answers it with evidence.
	d.checkDepositShortfall(rec, tx)

	hash, err := d.vault.RepayForBorrower(ctx, rec.LoanID, rec.BorrowerAddress, rec.PayoffStroops)
	if err != nil {
		d.vaultLegFailed(ctx, rec, err)
		return
	}

	if err := d.recorder.MarkSettled(ctx, rec.LoanID, hash); err != nil {
		// The chain moved but the row did not. Loud, because the next tick
		// would see funds_received again and could repay a second time.
		d.logger.Error("CRITICAL: vault repay_for succeeded but settlement was not recorded",
			"loan_id", rec.LoanID, "vault_tx_hash", hash, "error", err)
		d.alertOps("Repayment settled on-chain but not recorded",
			fmt.Sprintf("Loan %s: repay_for landed as %s but the loan row was not updated. Do not let this loan be repaid again.", rec.LoanID, hash))
		return
	}

	d.logger.Info("repayment settled",
		"loan_id", rec.LoanID,
		"borrower", rec.BorrowerAddress,
		"amount_stroops", rec.PayoffStroops,
		"vault_tx_hash", hash)
}

// checkDepositShortfall compares what MoneyGram credited against what the
// borrower was quoted.
//
// Reports only — it does not change the vault repay. A shortfall means the
// treasury is repaying the vault more than it received, which is a slow drain
// rather than a visible failure, so it is worth an ops alert even while the
// cause is still being established.
func (d *DepositDriver) checkDepositShortfall(rec RepaymentRecord, tx *stellaranchor.Transaction) {
	arrived, ok := usdcStroops(tx.AmountOut)
	if !ok || arrived <= 0 {
		d.logger.Warn("deposit completed without a readable amount_out",
			"loan_id", rec.LoanID, "amount_out", tx.AmountOut, "amount_out_asset", tx.AmountOutAsset)
		return
	}

	shortfall := rec.PayoffStroops - arrived
	if shortfall <= 0 {
		d.logger.Info("deposit credited in full",
			"loan_id", rec.LoanID, "arrived_stroops", arrived, "payoff_stroops", rec.PayoffStroops)
		return
	}

	feeTotal := ""
	if tx.FeeDetails != nil {
		feeTotal = tx.FeeDetails.Total
	}

	d.logger.Error("CRITICAL: deposit credited less than the quoted payoff",
		"loan_id", rec.LoanID,
		"arrived_stroops", arrived,
		"payoff_stroops", rec.PayoffStroops,
		"shortfall_stroops", shortfall,
		"amount_out", tx.AmountOut,
		"amount_out_asset", tx.AmountOutAsset,
		"fee_total", feeTotal)

	d.alertOps("Repayment deposit short of the quoted payoff",
		fmt.Sprintf("Loan %s: MoneyGram credited %d stroops against a quoted payoff of %d — short by %d. "+
			"The vault is being repaid the full payoff, so the treasury absorbs the difference. "+
			"amount_out=%s %s, fee_total=%s.",
			rec.LoanID, arrived, rec.PayoffStroops, shortfall,
			tx.AmountOut, tx.AmountOutAsset, feeTotal))
}

// usdcStroops parses a SEP-24 decimal amount into USDC stroops.
func usdcStroops(s string) (int64, bool) {
	v, err := parseDecimal(s)
	if err != nil || v < 0 {
		return 0, false
	}
	return int64(v * 1e7), true
}

// vaultLegFailed records a failed treasury-to-vault leg and decides how loudly
// to react.
//
// The borrower is never told. From their side the repayment is done — the cash
// reached the treasury and they were notified at that point — and this leg is
// ours. The row stays at funds_received, which is what brings it back here.
//
// Retrying never stops. The borrower's USDC is already on the treasury, so
// there is no state in which abandoning the leg is correct; the ceiling decides
// when a human is told, not when to give up.
func (d *DepositDriver) vaultLegFailed(ctx context.Context, rec RepaymentRecord, cause error) {
	attempts := rec.VaultAttempts + 1

	d.logger.Error("CRITICAL: vault repay_for failed — borrower USDC held in treasury",
		"loan_id", rec.LoanID,
		"borrower", rec.BorrowerAddress,
		"amount_stroops", rec.PayoffStroops,
		"attempts", attempts,
		"max_attempts", d.cfg.DepositVaultMaxAttempts,
		"error", cause)

	if err := d.recorder.RecordVaultAttempt(ctx, rec.LoanID, attempts); err != nil {
		// Losing the count is not fatal to the retry, but it does mean the
		// ceiling may never be reached, so it is worth its own line.
		d.logger.Error("failed to record vault repay attempt; escalation ceiling may not fire",
			"loan_id", rec.LoanID, "attempts", attempts, "error", err)
	}

	// Attempts only ever increment, so equality fires exactly once. Alerting on
	// >= instead would page ops on every subsequent tick.
	if attempts == d.cfg.DepositVaultMaxAttempts {
		d.alertOps("Repayment vault leg stuck",
			fmt.Sprintf("Loan %s: repay_for has failed %d times. The borrower paid, their USDC is on the treasury, "+
				"and the loan is still open. Borrower %s, amount %d stroops. Last error: %v",
				rec.LoanID, attempts, rec.BorrowerAddress, rec.PayoffStroops, cause))
	}

	backoff := d.cfg.DepositActiveBackoff
	if attempts >= d.cfg.DepositVaultMaxAttempts {
		backoff = d.cfg.DepositVaultRetryBackoff
	}
	d.reschedule(ctx, rec, backoff)
}

// sendPayInstructionsOnce tells the borrower how to hand over the cash.
//
// Two artifacts can carry that, and MoneyGram issues them at different points:
//
//   - external_transaction_id — the reference quoted at the counter. SEP-24
//     defines it as the external transaction that "started the deposit", so on
//     a cash-in it does not exist until the borrower has already paid. Testnet
//     confirms it empty through pending_user_transfer_start.
//   - more_info_url — the field the spec designates for telling a user how to
//     start a deposit. Populated from the first poll onward.
//
// Prefer the reference, because a code a borrower can read out beats a link
// they must open on a feature phone. Fall back to the page, because a borrower
// holding neither cannot pay at all.
//
// Sent once either way, marked before the send. The borrower cannot pay without
// this, so a silent failure strands a repayment everything else is ready for.
func (d *DepositDriver) sendPayInstructionsOnce(ctx context.Context, rec RepaymentRecord, tx *stellaranchor.Transaction) {
	if rec.ReferenceSent {
		return
	}

	reference := strings.TrimSpace(tx.ExternalTransactionID)
	if reference == "" && strings.TrimSpace(tx.MoreInfoURL) == "" {
		d.logger.Debug("no deposit reference or transaction page yet",
			"loan_id", rec.LoanID, "status", tx.Status)
		return
	}

	// Marker first: a failing SMS provider must not be retried every tick for
	// the rest of the window.
	if err := d.recorder.MarkReferenceSent(ctx, rec.LoanID); err != nil {
		d.logger.Error("failed to mark deposit instructions sent, not sending",
			"loan_id", rec.LoanID, "error", err)
		return
	}

	if reference != "" {
		d.logger.Info("sending deposit reference",
			"loan_id", rec.LoanID, "reference", reference)
		d.notifyPayInstructions("reference", rec, func(n RepaymentNotifier) error {
			return n.NotifyRepaymentReference(rec.LoanID, reference)
		})
		return
	}

	d.logger.Info("sending deposit transaction page, no reference issued",
		"loan_id", rec.LoanID, "status", tx.Status)
	d.notifyPayInstructions("more_info", rec, func(n RepaymentNotifier) error {
		return n.NotifyRepaymentMoreInfo(rec.LoanID)
	})
}

// notifyPayInstructions sends the one message the borrower cannot pay without,
// and escalates when it does not go out.
//
// The marker is already spent by the time this runs, so a failure here is
// terminal for the loan: no later tick retries, and the deposit sits open until
// it lapses with the borrower never told how to use it. A log line is too
// quiet for that — it needs a human to clear the marker and let the next tick
// resend.
func (d *DepositDriver) notifyPayInstructions(kind string, rec RepaymentRecord, send func(RepaymentNotifier) error) {
	if d.notifier == nil {
		d.logger.Error("no repayment notifier configured, borrower cannot be told how to pay",
			"kind", kind, "loan_id", rec.LoanID)
		d.alertOps("Repayment instructions not delivered",
			fmt.Sprintf("Loan %s: no notifier is wired, so the borrower was never told how to pay. The send marker is spent; clear repayment_reference_sent to retry.", rec.LoanID))
		return
	}

	if err := send(d.notifier); err != nil {
		d.logger.Error("CRITICAL: borrower was not told how to pay",
			"kind", kind, "loan_id", rec.LoanID, "error", err)
		d.alertOps("Repayment instructions not delivered",
			fmt.Sprintf("Loan %s: the %s message failed and will not be retried — the send marker is already spent. Clear repayment_reference_sent to resend. Error: %v", rec.LoanID, kind, err))
	}
}

// expireRepayment releases the quote lock after the window elapsed.
func (d *DepositDriver) expireRepayment(ctx context.Context, rec RepaymentRecord) {
	if err := d.recorder.MarkExpired(ctx, rec.LoanID); err != nil {
		d.logger.Error("failed to expire repayment, retrying next tick",
			"loan_id", rec.LoanID, "error", err)
		d.reschedule(ctx, rec, d.cfg.DepositIdleBackoff)
		return
	}
	d.logger.Info("repayment window expired",
		"loan_id", rec.LoanID, "expires_at", rec.ExpiresAt)
	d.notify("expired", rec, func(n RepaymentNotifier) error {
		return n.NotifyRepaymentExpired(rec.LoanID)
	})
}

// failRepayment ends the rail before any funds moved.
func (d *DepositDriver) failRepayment(ctx context.Context, rec RepaymentRecord, reason string) {
	if err := d.recorder.MarkFailed(ctx, rec.LoanID, reason); err != nil {
		d.logger.Error("failed to mark repayment failed, retrying next tick",
			"loan_id", rec.LoanID, "error", err)
		d.reschedule(ctx, rec, d.cfg.DepositIdleBackoff)
		return
	}
	d.logger.Info("repayment failed", "loan_id", rec.LoanID, "reason", reason)
	d.notify("expired", rec, func(n RepaymentNotifier) error {
		return n.NotifyRepaymentExpired(rec.LoanID)
	})
}

// reschedule sets the next poll time, clamping to a sane floor so a
// misconfigured zero interval cannot turn the driver into a spin loop.
func (d *DepositDriver) reschedule(ctx context.Context, rec RepaymentRecord, in time.Duration) {
	if in <= 0 {
		in = defaultDepositActiveBackoff
	}
	if err := d.recorder.ScheduleNextPoll(ctx, rec.LoanID, d.now().Add(in)); err != nil {
		d.logger.Warn("failed to schedule next poll",
			"loan_id", rec.LoanID, "in", in, "error", err)
	}
}

// notify sends one borrower message, tolerating a nil notifier.
func (d *DepositDriver) notify(kind string, rec RepaymentRecord, send func(RepaymentNotifier) error) {
	if d.notifier == nil {
		d.logger.Warn("no repayment notifier configured, message not sent",
			"kind", kind, "loan_id", rec.LoanID)
		return
	}
	if err := send(d.notifier); err != nil {
		d.logger.Warn("failed to send repayment notification",
			"kind", kind, "loan_id", rec.LoanID, "error", err)
	}
}

func (d *DepositDriver) alertOps(subject, message string) {
	alertOps(d.alerts, d.logger, subject, message)
}
