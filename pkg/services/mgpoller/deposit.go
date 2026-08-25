package mgpoller

import (
	"context"
	"fmt"
	"log/slog"
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
		"message", tx.Message,
	)

	if err := d.recorder.RecordDepositUpdate(ctx, rec.LoanID, tx); err != nil {
		d.logger.Warn("failed to persist deposit update",
			"loan_id", rec.LoanID, "error", err)
	}

	switch tx.Status {
	case stellaranchor.StatusIncomplete:
		// The borrower has not finished the webview. This is where a deposit
		// spends most of its life, and the only status the window expires from
		// — a borrower who has committed and is standing at a counter must not
		// have the quote pulled out from under them.
		d.handleIncomplete(ctx, rec)

	case stellaranchor.StatusPendingUserTransferStart:
		// Committed: agent chosen, walking to the counter. This is the only
		// point MoneyGram has issued a reference and the borrower has not yet
		// paid, so it is the one chance to send it.
		d.sendReferenceOnce(ctx, rec, tx)

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
func (d *DepositDriver) handleIncomplete(ctx context.Context, rec RepaymentRecord) {
	now := d.now()

	if !rec.ExpiresAt.IsZero() && !now.Before(rec.ExpiresAt) {
		d.expireRepayment(ctx, rec)
		return
	}

	if d.shouldRemind(rec, now) {
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
func (d *DepositDriver) shouldRemind(rec RepaymentRecord, now time.Time) bool {
	if rec.ReminderSent || rec.ExpiresAt.IsZero() || d.cfg.DepositReminderBefore <= 0 {
		return false
	}
	return !now.Before(rec.ExpiresAt.Add(-d.cfg.DepositReminderBefore))
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

// sendReferenceOnce delivers MoneyGram's deposit reference to the borrower.
//
// Sent once, marked before the send. The borrower cannot pay without it — the
// agent needs a reference to take cash against — so a silent failure here
// strands a repayment that everything else is ready for.
func (d *DepositDriver) sendReferenceOnce(ctx context.Context, rec RepaymentRecord, tx *stellaranchor.Transaction) {
	if rec.ReferenceSent || tx.ExternalTransactionID == "" {
		return
	}

	// Marker first: a failing SMS provider must not be retried every tick for
	// the rest of the window.
	if err := d.recorder.MarkReferenceSent(ctx, rec.LoanID); err != nil {
		d.logger.Error("failed to mark deposit reference sent, not sending",
			"loan_id", rec.LoanID, "error", err)
		return
	}

	d.logger.Info("sending deposit reference",
		"loan_id", rec.LoanID, "reference", tx.ExternalTransactionID)

	d.notify("reference", rec, func(n RepaymentNotifier) error {
		return n.NotifyRepaymentReference(rec.LoanID, tx.ExternalTransactionID)
	})
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
