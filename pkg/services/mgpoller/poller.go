// Package mgpoller polls MoneyGram for SEP-24 transaction status changes
// and drives the existing disbursement state machine accordingly.
//
// MoneyGram does not publish webhooks. The integration plan in
// internal-docs/moneygram-integration.md §10 makes polling the canonical
// driver for cash-pickup loans:
//
//  1. After InitiateWithdrawal, MG returns an interactive URL. The user
//     opens it (delivered via SMS by the USSD layer) and completes KYC.
//  2. MG transitions the SEP-24 transaction to pending_user_transfer_start.
//     The poller observes this and sends USDC from treasury to MG's
//     anchor account using the memo embedded in the tx response.
//  3. MG processes the payment and transitions to
//     pending_user_transfer_complete. The poller backfills the locked
//     payout amount, currency, and cash-pickup reference number on the
//     loan row, then SMSes the reference to the user.
//  4. completed / refunded / expired / error are terminal; the poller
//     transitions disbursement_status accordingly and stops polling.
//
// The poller mirrors pkg/webhook/refund_poller.go's lifecycle conventions:
// Start(ctx) runs a ticker loop and exits on context cancellation.
//
// # Layout
//
// The package is split by what varies. runner.go holds Runner, which knows
// only how to wake on a cadence, ask a Fetcher for a batch, and hand each
// record to a Driver — nothing about which way money is moving.
// withdrawal.go holds the cash-out state machine described above, as one
// Driver implementation. Poller wires the two together and stays the
// package's entry point: NewPoller, Start, and the dependency interfaces
// declared here are unchanged by the split.
//
// The seam exists because the same cadence-and-batch loop drives repayment
// cash-in, whose state machine is otherwise unrelated. Splitting it means the
// second direction adds a Driver rather than another branch through this one.
package mgpoller

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/Shamba-Records-Limited/microvault/pkg/payment/moneygram"
	"github.com/Shamba-Records-Limited/microvault/pkg/payment/offramp"
	"github.com/Shamba-Records-Limited/microvault/pkg/payment/stellaranchor"
	"github.com/Shamba-Records-Limited/microvault/pkg/stellar/rpc"
)

// LoanRecord is the projection of a loan row the poller needs to drive
// state for a single MoneyGram cash-pickup transaction.
//
// The lending module's loan repository constructs these from active rows
// where ramp_provider="moneygram" and disbursement_status is in the active set.
type LoanRecord struct {
	LoanID               string
	SequenceID           string  // = loans.ramp_sequence_id
	MoneyGramTxID        string  // = loans.ramp_request_id
	ChildAccountIndex    uint32  // for SEP-10 memo re-derivation
	PrincipalStroops     int64   // USDC stroops to send to MG anchor
	RequestedLocalAmount float64 // KES the user typed in USSD; used for drift alerts
	HasStellarSend       bool    // true once MG.tx.stellar_transaction_id is observed
	DisbursementStatus   string
	PhoneNumber          string
	UserID               string
}

// LoanFetcher retrieves active MoneyGram cash-pickup loans for the poller
// to evaluate. Implementations should select loans where ramp_provider =
// "moneygram" AND disbursement_status NOT IN terminal-states.
type LoanFetcher interface {
	GetActiveMoneyGramLoans(ctx context.Context, limit int) ([]LoanRecord, error)
}

// LoanRecorder writes back the per-tick state changes the poller derives
// from MG's SEP-24 transaction object.
type LoanRecorder interface {
	// RecordTransactionUpdate persists the latest fields from a polled MG
	// transaction onto the loan row: amount_out, amount_out_asset, amount_fee,
	// external_transaction_id, more_info_url. Idempotent.
	RecordTransactionUpdate(ctx context.Context, loanID string, tx *stellaranchor.Transaction) error

	// RecordSendUSDC records the Stellar tx hash from a successful USDC
	// transfer to MG's anchor account, replacing the pending claim written by
	// RecordSendAttempt.
	RecordSendUSDC(ctx context.Context, loanID string, txHash string) error

	// RecordSendAttempt claims the send *before* the payment is submitted, so
	// a crash between submission and RecordSendUSDC cannot let the next tick
	// re-send. The claim makes LoanRecord.HasStellarSend true on reload.
	RecordSendAttempt(ctx context.Context, loanID string) error

	// ClearSendAttempt releases the claim. Only called when the payment
	// definitively did not move funds, so a later tick may safely retry.
	ClearSendAttempt(ctx context.Context, loanID string) error

	// RecordRefund persists the settled refund: the Stellar hash MG returned
	// the USDC in, the net stroops received, and any shortfall against the
	// principal we originally sent. Written before the vault repay so a crash
	// mid-repay leaves evidence of what came back.
	RecordRefund(ctx context.Context, loanID string, refund RefundRecord) error
}

// RefundRecord is the settled outcome of a MoneyGram refund.
type RefundRecord struct {
	// TxHash is the Stellar transaction the refund arrived in. When MG splits
	// a refund across several payments this is the last one; the amount is
	// always the total.
	TxHash string

	// NetStroops is what actually landed, summed from the refund payments as
	// they appear on-ledger rather than from the anchor's reported amounts.
	NetStroops int64

	// ShortfallStroops is PrincipalStroops - NetStroops when MG returned less
	// than we sent, otherwise zero. Non-zero means the treasury absorbed the
	// difference and the loan needs a human to settle it.
	ShortfallStroops int64
}

// PaymentVerifier confirms that a Stellar transaction an anchor claims to have
// made actually succeeded on-ledger.
//
// Optional: a nil verifier skips confirmation and trusts the anchor, which is
// logged. Supplying one means we never repay the vault against a refund that
// did not land.
type PaymentVerifier interface {
	TransactionSucceeded(ctx context.Context, txHash string) (bool, error)

	// PaymentsTo returns the payments in txHash addressed to destination in
	// the named asset. Direction and amount both come from the ledger: an
	// anchor's outbound refund and our own inbound payment to that anchor are
	// both "successful transactions", so success alone cannot tell them apart.
	PaymentsTo(ctx context.Context, txHash, destination, assetCode, assetIssuer string) ([]rpc.Payment, error)
}

// DisbursementUpdater drives terminal state transitions and user
// notifications. The lending module's disbursement-status adapter already
// implements this for the YC flow; the same impl is reused here.
type DisbursementUpdater interface {
	UpdateDisbursementStatus(sequenceID string, status string) error
	NotifyDisbursementComplete(sequenceID string) error
	NotifyDisbursementFailed(sequenceID string) error

	// NotifyCashPickupReady tells the borrower their cash is collectable and
	// quotes the MG reference number. Sent once, when MG reports
	// pending_user_transfer_complete.
	NotifyCashPickupReady(sequenceID string) error

	// NotifyRefundReceived tells the borrower their cash pickup was cancelled
	// and the funds returned. Distinct from NotifyDisbursementFailed because
	// the usual cause is the borrower cancelling in MoneyGram's own UI,
	// sometimes by mistake — the message has to say they can request again.
	NotifyRefundReceived(sequenceID string) error

	RepayVault(sequenceID string) error

	// RepayVaultAmount repays an explicit stroop amount rather than the loan
	// principal. Used for refunds, where MG may return less than we sent and
	// repaying the full principal would overdraw the treasury.
	RepayVaultAmount(sequenceID string, amountStroops int64) error
}

// AlertService is the same interface used by the YC refund poller —
// receives ops alerts when something needs human attention. Optional.
type AlertService interface {
	AlertOps(subject, message string) error
}

// PollerConfig configures cadence and drift detection.
type PollerConfig struct {
	// PollInterval is how often the ticker fires. Default 30s.
	PollInterval time.Duration

	// MaxBatch caps the number of loans evaluated per tick. Default 100.
	MaxBatch int

	// PayoutDriftAlertPct triggers an ops alert when the locked
	// amount_out diverges from RequestedLocalAmount by more than this
	// fraction (e.g. 0.02 = 2 %). Default 0.02. Set to 0 to disable.
	PayoutDriftAlertPct float64

	// RefundSettleMaxAttempts caps how many ticks a loan may sit in
	// refund_pending waiting for MoneyGram to publish the SEP-24 refunds
	// object before ops are alerted. Observed MG behaviour is that it may
	// never arrive, so without a ceiling the loan polls silently forever.
	// Default 20 (10 minutes at the default interval).
	RefundSettleMaxAttempts int

	// RefundDestination is the account MoneyGram returns funds to — the
	// wallet we withdraw from. Defaults to the client's SEP-10 account.
	RefundDestination string

	// RefundAssetIssuer is the USDC issuer a refund payment must carry.
	// Defaults to the anchor client's configured issuer.
	RefundAssetIssuer string

	// Deposit-side cadence. Separate from the withdrawal fields above because
	// the two directions are not on comparable clocks: a withdrawal resolves
	// in minutes and is polled hard, while a deposit waits on a borrower for
	// up to five days. Per-record timing lives in repayment_next_poll_at; the
	// fields here set how often that column is consulted and what it is set to.

	// DepositPollInterval is how often the deposit runner asks for rows whose
	// next poll is due. Default 60s. This is not how often any one deposit is
	// polled — that is the backoff below.
	DepositPollInterval time.Duration

	// DepositMaxBatch caps repayments evaluated per tick. Default 100.
	DepositMaxBatch int

	// DepositActiveBackoff is the gap between polls once the borrower has
	// engaged — committed in the webview, or the transaction is moving through
	// MoneyGram. Default 2m.
	DepositActiveBackoff time.Duration

	// DepositIdleBackoff is the gap between polls while the borrower has not
	// opened the link. This is most of the window, and the reason cadence is
	// column-driven at all. Default 30m.
	DepositIdleBackoff time.Duration

	// DepositReminderBefore is how long before expiry the single reminder SMS
	// goes out. Default 24h. Zero disables the reminder.
	DepositReminderBefore time.Duration

	// DepositVaultMaxAttempts is how many times the treasury-to-vault leg may
	// fail before ops are alerted. Default 10. The borrower's USDC is already
	// on the treasury by this point, so the retry never stops — the ceiling
	// decides when a human is told, not when to give up.
	DepositVaultMaxAttempts int

	// DepositVaultRetryBackoff is the gap between vault-leg retries once the
	// ceiling has been passed. Default 1h. Slower than DepositActiveBackoff
	// because past the ceiling the cause is unlikely to clear on its own, and
	// hammering a broken RPC every two minutes helps nobody.
	DepositVaultRetryBackoff time.Duration
}

// Deposit-side defaults, named so reschedule can fall back to one without
// reaching for a literal.
const (
	defaultDepositPollInterval   = 60 * time.Second
	defaultDepositMaxBatch       = 100
	defaultDepositActiveBackoff  = 2 * time.Minute
	defaultDepositIdleBackoff    = 30 * time.Minute
	defaultDepositReminderBefore = 24 * time.Hour

	defaultDepositVaultMaxAttempts  = 10
	defaultDepositVaultRetryBackoff = time.Hour
)

// withDepositDefaults fills the deposit fields a caller left unset. Kept apart
// from NewPoller's validation so the withdrawal poller, which ignores these
// fields entirely, is not made to carry defaults it never reads.
func (c PollerConfig) withDepositDefaults() PollerConfig {
	if c.DepositPollInterval <= 0 {
		c.DepositPollInterval = defaultDepositPollInterval
	}
	if c.DepositMaxBatch <= 0 {
		c.DepositMaxBatch = defaultDepositMaxBatch
	}
	if c.DepositActiveBackoff <= 0 {
		c.DepositActiveBackoff = defaultDepositActiveBackoff
	}
	if c.DepositIdleBackoff <= 0 {
		c.DepositIdleBackoff = defaultDepositIdleBackoff
	}
	if c.DepositReminderBefore < 0 {
		c.DepositReminderBefore = 0
	}
	if c.DepositVaultMaxAttempts <= 0 {
		c.DepositVaultMaxAttempts = defaultDepositVaultMaxAttempts
	}
	if c.DepositVaultRetryBackoff <= 0 {
		c.DepositVaultRetryBackoff = defaultDepositVaultRetryBackoff
	}
	return c
}

// DefaultConfig returns a sensible PollerConfig.
func DefaultConfig() PollerConfig {
	return PollerConfig{
		PollInterval:            30 * time.Second,
		MaxBatch:                100,
		PayoutDriftAlertPct:     0.02,
		RefundSettleMaxAttempts: 20,
		DepositPollInterval:     defaultDepositPollInterval,
		DepositMaxBatch:         defaultDepositMaxBatch,
		DepositActiveBackoff:    defaultDepositActiveBackoff,
		DepositIdleBackoff:      defaultDepositIdleBackoff,
		DepositReminderBefore:   defaultDepositReminderBefore,

		DepositVaultMaxAttempts:  defaultDepositVaultMaxAttempts,
		DepositVaultRetryBackoff: defaultDepositVaultRetryBackoff,
	}
}

// Poller drives the MoneyGram cash-pickup state machine.
type Poller struct {
	client       *moneygram.Client
	fetcher      LoanFetcher
	recorder     LoanRecorder
	disbursement DisbursementUpdater
	treasury     offramp.TreasuryTransfer
	verifier     PaymentVerifier
	alerts       AlertService
	cfg          PollerConfig
	logger       *slog.Logger

	// refundWaits counts consecutive ticks a loan has spent waiting for MG to
	// publish refund payments. Guarded because Start runs in its own goroutine.
	refundWaitMu sync.Mutex
	refundWaits  map[string]int

	// runner supplies the ticker and batching. Poller is its Driver.
	runner *Runner[LoanRecord]
}

// PollerDeps are the collaborators and settings a withdrawal Poller needs.
// Client, Fetcher, Recorder, Disbursement and Treasury are required; Verifier,
// Alerts and Logger may be nil.
type PollerDeps struct {
	Client       *moneygram.Client
	Fetcher      LoanFetcher
	Recorder     LoanRecorder
	Disbursement DisbursementUpdater
	Treasury     offramp.TreasuryTransfer
	Verifier     PaymentVerifier
	Alerts       AlertService
	Config       PollerConfig
	Logger       *slog.Logger
}

// NewPoller validates the dependencies and returns a Poller.
func NewPoller(deps PollerDeps) (*Poller, error) {
	const direction = "withdrawal"

	client, fetcher, recorder := deps.Client, deps.Fetcher, deps.Recorder
	disbursement, treasury := deps.Disbursement, deps.Treasury
	cfg, logger := deps.Config, deps.Logger

	if missing, found := firstMissing([]dep{
		{"moneygram_client", client == nil},
		{"loan_fetcher", fetcher == nil},
		{"loan_recorder", recorder == nil},
		{"disbursement_updater", disbursement == nil},
		{"treasury_transfer", treasury == nil},
	}); found {
		return nil, missingDep(direction, missing.name)
	}

	if cfg.PollInterval <= 0 {
		cfg.PollInterval = 30 * time.Second
	}
	if cfg.MaxBatch <= 0 {
		cfg.MaxBatch = 100
	}
	if cfg.PayoutDriftAlertPct < 0 {
		cfg.PayoutDriftAlertPct = 0
	}
	if cfg.RefundSettleMaxAttempts < 0 {
		cfg.RefundSettleMaxAttempts = 0
	}
	if logger == nil {
		logger = slog.Default()
	}
	if cfg.RefundDestination == "" {
		// Only safe where the SEP-10 auth wallet and the SEP-24 funds wallet
		// share a secret. Where they differ the poller watches an account the
		// refund never reaches, and settlement stalls without an obvious cause.
		logger.Warn("mgpoller: no refund destination configured; falling back to the SEP-10 account",
			"fallback", client.TreasuryAddress())
	}
	p := &Poller{
		client:       client,
		fetcher:      fetcher,
		recorder:     recorder,
		disbursement: disbursement,
		treasury:     treasury,
		verifier:     deps.Verifier,
		alerts:       deps.Alerts,
		cfg:          cfg,
		refundWaits:  make(map[string]int),
		logger:       logger.With("component", "mgpoller"),
	}
	// Two-step because the runner's Driver is the Poller itself.
	p.runner = NewRunner(RunnerDeps[LoanRecord]{
		Direction: direction,
		Interval:  cfg.PollInterval,
		MaxBatch:  cfg.MaxBatch,
		Fetcher:   FetchFunc[LoanRecord](fetcher.GetActiveMoneyGramLoans),
		Driver:    p,
		Logger:    p.logger,
	})
	return p, nil
}

// Start runs the withdrawal poller until ctx is cancelled.
func (p *Poller) Start(ctx context.Context) { p.runner.Start(ctx) }

// poll runs a single withdrawal cycle.
func (p *Poller) poll(ctx context.Context) { p.runner.poll(ctx) }

// Drive implements Driver[LoanRecord]. The state machine is driveLoan, in
// withdrawal.go; this is the name the runner calls it by.
func (p *Poller) Drive(ctx context.Context, rec LoanRecord) { p.driveLoan(ctx, rec) }

func (p *Poller) alertOps(subject, message string) {
	alertOps(p.alerts, p.logger, subject, message)
}
