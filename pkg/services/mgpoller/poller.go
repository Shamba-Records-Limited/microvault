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
package mgpoller

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/Shamba-Records-Limited/microvault/pkg/payment/moneygram"
	"github.com/Shamba-Records-Limited/microvault/pkg/payment/offramp"
	"github.com/Shamba-Records-Limited/microvault/pkg/payment/stellaranchor"
	"github.com/Shamba-Records-Limited/microvault/pkg/stellar/rpc"
	"github.com/Shamba-Records-Limited/microvault/pkg/stellar/types"
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

// Disbursement status strings the poller writes. These match the canonical
// DisbursementStatus* constants in the lending module's loan model. We hardcode
// them here because this package can't import from the lending module without a
// layering inversion.
//
// statusRefundPending is intentionally not in the lending module's enum yet
// — added inline here to mirror what the existing YC flow writes; reconcile
// in a follow-up that also fixes YC's "complete" vs the model's "completed".
const (
	statusProcessing     = "processing"
	statusCompleted      = "completed"
	statusFailed         = "failed"
	statusRefundPending  = "refund_pending"
	statusRefundReceived = "refund_received"
)

// refundAssetCode is the asset a MoneyGram refund must arrive in. A payment in
// any other asset is not a settlement of this loan.
const refundAssetCode = "USDC"

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
}

// DefaultConfig returns a sensible PollerConfig.
func DefaultConfig() PollerConfig {
	return PollerConfig{
		PollInterval:            30 * time.Second,
		MaxBatch:                100,
		PayoutDriftAlertPct:     0.02,
		RefundSettleMaxAttempts: 20,
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
}

// NewPoller validates the dependencies and returns a Poller. client,
// fetcher, recorder, disbursement, and treasury are required; verifier,
// alerts and logger may be nil.
func NewPoller(
	client *moneygram.Client,
	fetcher LoanFetcher,
	recorder LoanRecorder,
	disbursement DisbursementUpdater,
	treasury offramp.TreasuryTransfer,
	verifier PaymentVerifier,
	alerts AlertService,
	cfg PollerConfig,
	logger *slog.Logger,
) (*Poller, error) {
	if client == nil {
		return nil, fmt.Errorf("mgpoller: moneygram client is required")
	}
	if fetcher == nil {
		return nil, fmt.Errorf("mgpoller: loan fetcher is required")
	}
	if recorder == nil {
		return nil, fmt.Errorf("mgpoller: loan recorder is required")
	}
	if disbursement == nil {
		return nil, fmt.Errorf("mgpoller: disbursement updater is required")
	}
	if treasury == nil {
		return nil, fmt.Errorf("mgpoller: treasury transfer is required")
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
	return &Poller{
		client:       client,
		fetcher:      fetcher,
		recorder:     recorder,
		disbursement: disbursement,
		treasury:     treasury,
		verifier:     verifier,
		alerts:       alerts,
		cfg:          cfg,
		refundWaits:  make(map[string]int),
		logger:       logger.With("component", "mgpoller"),
	}, nil
}

// Start runs the poller until ctx is cancelled. Mirrors RefundPoller.Start.
func (p *Poller) Start(ctx context.Context) {
	ticker := time.NewTicker(p.cfg.PollInterval)
	defer ticker.Stop()

	p.logger.Info("starting", "interval", p.cfg.PollInterval, "max_batch", p.cfg.MaxBatch)

	// Run once immediately for boot-time resume so we don't wait one full
	// interval before catching up on loans that completed during downtime.
	p.poll(ctx)

	for {
		select {
		case <-ctx.Done():
			p.logger.Info("shutting down")
			return
		case <-ticker.C:
			p.poll(ctx)
		}
	}
}

// poll runs a single cycle: fetch active MG loans to drive each.
func (p *Poller) poll(ctx context.Context) {
	loans, err := p.fetcher.GetActiveMoneyGramLoans(ctx, p.cfg.MaxBatch)
	if err != nil {
		p.logger.Error("failed to fetch active loans", "error", err)
		return
	}
	if len(loans) == 0 {
		return
	}
	p.logger.Info("polling tick", "active_loans", len(loans))

	for _, l := range loans {
		if ctx.Err() != nil {
			return
		}
		p.driveLoan(ctx, l)
	}
}

// driveLoan evaluates a single loan and applies the appropriate state
// transition. Errors are logged but never abort the batch — every loan
// gets its turn each tick.
func (p *Poller) driveLoan(ctx context.Context, rec LoanRecord) {
	if rec.MoneyGramTxID == "" {
		p.logger.Warn("loan has no MoneyGram transaction id, skipping",
			"loan_id", rec.LoanID, "sequence_id", rec.SequenceID)
		return
	}

	childMemo := stellaranchor.ChildAccountMemo(p.client.TreasuryAddress(), rec.ChildAccountIndex)
	tx, err := p.client.GetTransaction(ctx, childMemo, rec.MoneyGramTxID)
	if err != nil {
		p.logger.Error("GetTransaction failed",
			"loan_id", rec.LoanID,
			"mg_tx_id", rec.MoneyGramTxID,
			"error", err)
		return
	}

	// Unconditional: every branch below can return without logging, which
	// makes a parked transaction indistinguishable from a stalled poller.
	p.logger.Info("MoneyGram transaction polled",
		"loan_id", rec.LoanID,
		"mg_tx_id", rec.MoneyGramTxID,
		"status", tx.Status,
		"mg_stellar_tx_id", tx.StellarTransactionID,
		"withdraw_anchor_account", tx.WithdrawAnchorAccount,
		"message", tx.Message,
	)

	// Always persist the latest fields — pending_user_transfer_complete
	// is the one that matters most (carries amount_out + reference) but
	// having amount_in available earlier is harmless.
	if err := p.recorder.RecordTransactionUpdate(ctx, rec.LoanID, tx); err != nil {
		p.logger.Warn("failed to persist transaction update",
			"loan_id", rec.LoanID, "error", err)
	}

	switch tx.Status {
	case stellaranchor.StatusPendingUserTransferStart:
		p.handlePendingUserTransferStart(ctx, rec, tx)

	case stellaranchor.StatusPendingUserTransferComplete:
		p.handlePendingUserTransferComplete(ctx, rec, tx)

	case stellaranchor.StatusCompleted:
		p.handleCompleted(rec, tx)

	case stellaranchor.StatusRefunded:
		p.handleRefunded(ctx, rec, tx)

	case stellaranchor.StatusExpired,
		stellaranchor.StatusNoMarket,
		stellaranchor.StatusTooSmall,
		stellaranchor.StatusTooLarge,
		stellaranchor.StatusError:
		p.handleTerminalFailure(rec, tx)

	case stellaranchor.StatusOnHold:
		// MG paused the transaction for additional checks (compliance,
		// fraud review). Not terminal, but it can sit here for a while —
		// alert ops so a human can chase MG support if needed.
		p.logger.Warn("MoneyGram transaction on hold",
			"loan_id", rec.LoanID, "mg_tx_id", rec.MoneyGramTxID,
			"message", tx.Message)
		p.alertOps("MoneyGram transaction on hold",
			fmt.Sprintf("Loan %s: MG on_hold for additional checks. Message: %s",
				rec.LoanID, tx.Message))

	case stellaranchor.StatusPendingTrust:
		// User-side trustline missing. In our custodial-wallet model the
		// user never holds USDC directly, so this only arises on the MG
		// side and likely indicates an anchor-config issue worth flagging.
		p.logger.Warn("MoneyGram pending_trust — anchor missing trustline?",
			"loan_id", rec.LoanID, "mg_tx_id", rec.MoneyGramTxID,
			"message", tx.Message)
		p.alertOps("MoneyGram pending_trust",
			fmt.Sprintf("Loan %s: MG reported pending_trust. Investigate anchor trustline. Message: %s",
				rec.LoanID, tx.Message))

	case stellaranchor.StatusPendingUser:
		// MG is waiting on the user (additional KYC, action at the agent,
		// etc.). Nothing automated to do — log for visibility and let
		// the existing reminder SMS path handle nudges.
		p.logger.Info("MoneyGram waiting on user action",
			"loan_id", rec.LoanID, "mg_tx_id", rec.MoneyGramTxID,
			"message", tx.Message)

	case stellaranchor.StatusIncomplete,
		stellaranchor.StatusPendingAnchor,
		stellaranchor.StatusPendingExternal,
		stellaranchor.StatusPendingStellar:
		// In-flight, no action needed this cycle. pending_stellar is
		// the natural transient state right after our SendUSDC while
		// the network confirms the payment to MG's anchor. Status is
		// already logged unconditionally above.

	default:
		p.logger.Warn("unexpected MoneyGram status",
			"loan_id", rec.LoanID, "status", tx.Status)
	}
}

// handlePendingUserTransferStart sends USDC from treasury to MG's anchor
// account using the memo MG provided. Idempotency is best-effort: if MG's
// tx.stellar_transaction_id is already populated we skip. There is a
// small window between SendUSDC succeeding and MG observing the payment
// where re-sending is theoretically possible — see §22 / poller TODOs.
func (p *Poller) handlePendingUserTransferStart(ctx context.Context, rec LoanRecord, tx *stellaranchor.Transaction) {
	if tx.StellarTransactionID != "" || rec.HasStellarSend {
		// Already observed — wait for MG to advance. Logged because this is
		// otherwise a silent permanent no-op: if MG reports a stellar_transaction_id
		// we never sent, the loan parks here forever with no diagnostic.
		p.logger.Info("skipping SendUSDC — payment already observed",
			"loan_id", rec.LoanID, "mg_tx_id", rec.MoneyGramTxID,
			"mg_stellar_tx_id", tx.StellarTransactionID,
			"has_stellar_send", rec.HasStellarSend,
			"withdraw_anchor_account", tx.WithdrawAnchorAccount)
		return
	}
	if tx.WithdrawAnchorAccount == "" {
		p.logger.Error("pending_user_transfer_start without withdraw_anchor_account",
			"loan_id", rec.LoanID, "mg_tx_id", rec.MoneyGramTxID)
		return
	}
	if rec.PrincipalStroops <= 0 {
		p.logger.Error("loan has no principal_stroops to send",
			"loan_id", rec.LoanID)
		return
	}

	// Claim the send before submitting. If the process dies between submission
	// and RecordSendUSDC, the claim is already durable, so the next tick sees
	// HasStellarSend and refuses to pay twice. Failing to claim means we cannot
	// guarantee idempotency, so we don't send at all.
	if err := p.recorder.RecordSendAttempt(ctx, rec.LoanID); err != nil {
		p.logger.Error("could not claim send attempt; refusing to send",
			"loan_id", rec.LoanID, "error", err)
		return
	}

	p.logger.Info("sending USDC to MoneyGram anchor",
		"loan_id", rec.LoanID,
		"destination", tx.WithdrawAnchorAccount,
		"memo", tx.WithdrawMemo,
		"amount_stroops", rec.PrincipalStroops,
	)

	txHash, err := p.treasury.SendUSDC(ctx, tx.WithdrawAnchorAccount, tx.WithdrawMemo, rec.PrincipalStroops)
	if err != nil {
		p.logger.Error("SendUSDC failed",
			"loan_id", rec.LoanID, "error", err)

		if errors.Is(err, types.ErrTransactionFailedOnLedger) {
			// The transaction was included but its operation failed (e.g.
			// PAYMENT_UNDERFUNDED): the fee was burned, no USDC moved. Safe to
			// release the claim so a later tick retries once the cause is fixed.
			if cerr := p.recorder.ClearSendAttempt(ctx, rec.LoanID); cerr != nil {
				p.logger.Error("failed to release send claim after on-ledger failure; loan will not retry until cleared manually",
					"loan_id", rec.LoanID, "error", cerr)
			}
			p.alertOps("MoneyGram USDC send failed on ledger",
				fmt.Sprintf("Loan %s: payment to %s failed on ledger (no funds moved), will retry: %v",
					rec.LoanID, tx.WithdrawAnchorAccount, err))
			return
		}

		// Outcome unknown (submission error, poll timeout): the payment may or
		// may not have landed. Keep the claim — a duplicate payment is worse
		// than a stalled loan — and escalate for manual reconciliation.
		p.alertOps("MoneyGram USDC send outcome UNKNOWN — manual reconciliation required",
			fmt.Sprintf("Loan %s: SendUSDC to %s returned %v. The payment may have landed. "+
				"Verify on-chain before clearing ramp_stellar_tx_hash; clearing it allows a re-send.",
				rec.LoanID, tx.WithdrawAnchorAccount, err))
		return
	}
	p.logger.Info("USDC sent to MoneyGram",
		"loan_id", rec.LoanID, "tx_hash", txHash)
	if err := p.recorder.RecordSendUSDC(ctx, rec.LoanID, txHash); err != nil {
		p.logger.Warn("failed to record SendUSDC tx hash",
			"loan_id", rec.LoanID, "error", err)
	}
}

// handlePendingUserTransferComplete is when MG has locked in the payout —
// `amount_out`, `amount_out_asset`, and `external_transaction_id` are
// populated. The recorder already wrote those above; here we just emit a
// drift alert if the locked payout deviates from what the user saw at
// USSD entry.
func (p *Poller) handlePendingUserTransferComplete(_ context.Context, rec LoanRecord, tx *stellaranchor.Transaction) {
	// MG has received our USDC and the cash is collectable at an agent. Move
	// the loan off its initiated status exactly once and tell the borrower
	// how to collect. The status transition is itself the idempotency guard,
	// so the SMS is not re-sent on every 30s tick.
	//
	// Not terminal: the loan keeps being polled until MG reports completed
	// (the borrower actually collected).
	if rec.DisbursementStatus != statusProcessing {
		if err := p.disbursement.UpdateDisbursementStatus(rec.SequenceID, statusProcessing); err != nil {
			// Bail before notifying — sending an SMS we failed to record
			// would re-send it every tick.
			p.logger.Error("failed to mark cash ready for pickup",
				"loan_id", rec.LoanID, "sequence_id", rec.SequenceID, "error", err)
			return
		}
		p.logger.Info("cash available for pickup",
			"loan_id", rec.LoanID,
			"mg_tx_id", rec.MoneyGramTxID,
			"reference", tx.ExternalTransactionID,
			"amount_out", tx.AmountOut,
		)
		if err := p.disbursement.NotifyCashPickupReady(rec.SequenceID); err != nil {
			p.logger.Warn("cash-pickup ready SMS failed",
				"loan_id", rec.LoanID, "error", err)
		}
	}

	if p.cfg.PayoutDriftAlertPct == 0 || rec.RequestedLocalAmount == 0 {
		return
	}
	got, err := parseDecimal(tx.AmountOut)
	if err != nil || got == 0 {
		return
	}
	deviation := (got - rec.RequestedLocalAmount) / rec.RequestedLocalAmount
	if deviation < -p.cfg.PayoutDriftAlertPct || deviation > p.cfg.PayoutDriftAlertPct {
		p.logger.Warn("PAYOUT DRIFT: MG amount_out diverges from requested",
			"loan_id", rec.LoanID,
			"requested", rec.RequestedLocalAmount,
			"locked", got,
			"deviation_pct", deviation*100)
	}
}

// handleCompleted: MG confirmed the user picked up the cash.
func (p *Poller) handleCompleted(rec LoanRecord, _ *stellaranchor.Transaction) {
	if err := p.disbursement.UpdateDisbursementStatus(rec.SequenceID, statusCompleted); err != nil {
		p.logger.Error("failed to mark disbursement complete",
			"loan_id", rec.LoanID, "sequence_id", rec.SequenceID, "error", err)
		return
	}
	if err := p.disbursement.NotifyDisbursementComplete(rec.SequenceID); err != nil {
		p.logger.Warn("failed to notify disbursement complete",
			"loan_id", rec.LoanID, "error", err)
	}
}

// handleRefunded settles a MoneyGram refund. The usual cause is the borrower
// cancelling in MG's own UI, so this is an expected path rather than a failure.
//
// It runs in two stages across ticks. First the loan is marked refund_pending,
// which is not terminal for this poller — the loan keeps being fetched. Then,
// once MG reports the refund payments, the funds are confirmed on-ledger, the
// vault is repaid with what actually came back, and the loan reaches
// refund_received.
//
// Splitting it this way means every step is retried on the next tick until it
// succeeds. Nothing depends on a single observation.
func (p *Poller) handleRefunded(ctx context.Context, rec LoanRecord, tx *stellaranchor.Transaction) {
	if rec.DisbursementStatus != statusRefundPending {
		if err := p.disbursement.UpdateDisbursementStatus(rec.SequenceID, statusRefundPending); err != nil {
			p.logger.Error("failed to mark refund_pending",
				"loan_id", rec.LoanID, "sequence_id", rec.SequenceID, "error", err)
			return
		}
		p.logger.Info("MoneyGram refunded — awaiting inbound USDC",
			"loan_id", rec.LoanID, "sequence_id", rec.SequenceID)
	}

	payments := tx.Refunds.StellarPayments()
	if len(payments) == 0 {
		// MoneyGram does not populate the SEP-24 refunds object, but it does
		// move stellar_transaction_id onto the refund once one settles. Read
		// the payment off the ledger rather than trusting the field's meaning.
		if p.settleFromStellarTx(ctx, rec, tx) {
			return
		}
		// MG has declared the refund but published no payments to settle it.
		// Observed on testnet: status "refunded" arrives with no refunds object
		// at all (and the deprecated `refunded` flag set false), so this is not
		// always a transient gap — without a ceiling the loan polls forever and
		// the vault is never repaid.
		p.awaitRefundDetails(rec, tx)
		return
	}
	p.clearRefundWait(rec.LoanID)

	// Trust but verify: MG naming a Stellar hash is not proof the payment
	// landed. Repaying the vault against a refund that never arrived would
	// overdraw the treasury.
	lastHash := payments[len(payments)-1].ID
	if !p.refundLanded(ctx, rec, payments) {
		return
	}

	net, ok := p.ledgerRefundTotal(ctx, rec, tx, payments)
	if !ok {
		return
	}
	if net <= 0 {
		p.logger.Error("refund settled to zero; not repaying vault",
			"loan_id", rec.LoanID, "mg_tx_id", rec.MoneyGramTxID)
		p.alertOps("MoneyGram refund settled to zero",
			fmt.Sprintf("Loan %s: MG reported a refund of 0 after we sent %d stroops. Needs manual review.",
				rec.LoanID, rec.PrincipalStroops))
		return
	}
	p.crossCheckReportedRefund(rec, tx, net)

	p.settleRefund(ctx, rec, lastHash, net)
}

// ledgerRefundTotal sums the anchor's refund payments as they appear on-ledger.
//
// This is the only amount safe to repay the vault with. An anchor's own refund
// arithmetic can disagree with what it actually sent: MoneyGram reports
// amount_refunded already net of its withdrawal fee and then restates that fee
// in amount_fee, so deriving the total from those fields subtracts it twice.
//
// Reports false when the total cannot be established, leaving the loan in
// refund_pending for the next tick rather than guessing.
func (p *Poller) ledgerRefundTotal(ctx context.Context, rec LoanRecord, tx *stellaranchor.Transaction, payments []stellaranchor.RefundPayment) (int64, bool) {
	dest := p.refundDestination()
	if p.verifier == nil || dest == "" {
		net, err := tx.Refunds.NetRefundedStroops()
		if err != nil {
			p.logger.Error("no ledger verification available and refund amounts are unparseable; not repaying vault",
				"loan_id", rec.LoanID, "mg_tx_id", rec.MoneyGramTxID, "error", err)
			p.alertOps("MoneyGram refund amount unparseable",
				fmt.Sprintf("Loan %s: cannot parse MG refund amounts and cannot read the ledger, "+
					"vault repay withheld: %v", rec.LoanID, err))
			return 0, false
		}
		p.logger.Warn("settling refund on the anchor's reported amount; ledger unverified",
			"loan_id", rec.LoanID, "mg_tx_id", rec.MoneyGramTxID, "reported_stroops", net)
		return net, true
	}

	// MG may list the same hash more than once when it splits a refund across
	// payments; PaymentsTo already returns every payment in that transaction,
	// so reading it twice would double the total.
	seen := make(map[string]bool, len(payments))
	var total int64
	for _, pay := range payments {
		if seen[pay.ID] {
			continue
		}
		seen[pay.ID] = true

		received, err := p.verifier.PaymentsTo(ctx, pay.ID, dest, refundAssetCode, p.refundAssetIssuer())
		if err != nil {
			p.logger.Warn("could not read refund payment from ledger; will retry",
				"loan_id", rec.LoanID, "refund_tx_hash", pay.ID, "error", err)
			return 0, false
		}
		if len(received) == 0 {
			p.logger.Warn("refund transaction carries no payment to us; will retry",
				"loan_id", rec.LoanID, "refund_tx_hash", pay.ID, "destination", dest)
			return 0, false
		}
		for _, r := range received {
			total += r.AmountStroops
		}
	}
	return total, true
}

// crossCheckReportedRefund alerts when the anchor's stated refund total differs
// from what the ledger shows. Settlement has already used the ledger figure;
// this exists so a systematically wrong anchor payload is visible rather than
// silently tolerated.
func (p *Poller) crossCheckReportedRefund(rec LoanRecord, tx *stellaranchor.Transaction, ledger int64) {
	reported, err := tx.Refunds.NetRefundedStroops()
	if err != nil {
		p.logger.Warn("anchor refund amounts unparseable; settled on the ledger regardless",
			"loan_id", rec.LoanID, "mg_tx_id", rec.MoneyGramTxID, "error", err)
		return
	}
	if reported == ledger {
		return
	}
	p.logger.Warn("anchor refund amount disagrees with the ledger",
		"loan_id", rec.LoanID, "mg_tx_id", rec.MoneyGramTxID,
		"reported_stroops", reported, "ledger_stroops", ledger)
	p.alertOps("MoneyGram refund amount mismatch",
		fmt.Sprintf("Loan %s: MG reported %d stroops refunded but the ledger shows %d. "+
			"Settled on the ledger figure.", rec.LoanID, reported, ledger))
}

// settleRefund records the refund, repays the vault with exactly what came
// back, marks the loan terminal and notifies the borrower.
//
// Shared by both settlement routes — the SEP-24 refunds object and
// stellar_transaction_id — so the money-moving sequence and its crash ordering
// exist in one place regardless of how the refund was discovered.
func (p *Poller) settleRefund(ctx context.Context, rec LoanRecord, refundHash string, net int64) {
	// repay is capped at the principal: an anchor that returns more than we
	// sent is not the vault's windfall, and pushing the excess in would credit
	// the loan with money it never lent. The excess stays put for an admin.
	var shortfall, excess int64
	repay := net
	switch {
	case rec.PrincipalStroops > net:
		shortfall = rec.PrincipalStroops - net
	case net > rec.PrincipalStroops:
		excess = net - rec.PrincipalStroops
		repay = rec.PrincipalStroops
	}

	// Persist before repaying: if the repay crashes mid-flight, the row still
	// records what came back and in which transaction.
	if err := p.recorder.RecordRefund(ctx, rec.LoanID, RefundRecord{
		TxHash:           refundHash,
		NetStroops:       net,
		ShortfallStroops: shortfall,
	}); err != nil {
		p.logger.Error("failed to record refund; not repaying vault",
			"loan_id", rec.LoanID, "error", err)
		return
	}

	if shortfall > 0 {
		p.logger.Warn("REFUND SHORTFALL: MG returned less than we sent",
			"loan_id", rec.LoanID,
			"sent_stroops", rec.PrincipalStroops,
			"returned_stroops", net,
			"shortfall_stroops", shortfall)
		p.alertOps("MoneyGram refund shortfall",
			fmt.Sprintf("Loan %s: sent %d stroops, MG returned %d, shortfall %d. "+
				"Vault repaid with what came back; treasury absorbed the difference.",
				rec.LoanID, rec.PrincipalStroops, net, shortfall))
	}

	if excess > 0 {
		p.logger.Warn("REFUND EXCESS: MG returned more than we sent",
			"loan_id", rec.LoanID,
			"sent_stroops", rec.PrincipalStroops,
			"returned_stroops", net,
			"excess_stroops", excess)
		p.alertOps("MoneyGram refund excess",
			fmt.Sprintf("Loan %s: sent %d stroops, MG returned %d, excess %d. "+
				"Vault repaid the principal; the excess is held in the funds wallet "+
				"for an admin to settle.",
				rec.LoanID, rec.PrincipalStroops, net, excess))
	}

	// Repay only what came back. Repaying the full principal would draw the
	// difference from unrelated treasury funds.
	if err := p.disbursement.RepayVaultAmount(rec.SequenceID, repay); err != nil {
		p.logger.Error("CRITICAL: vault repay failed after refund",
			"loan_id", rec.LoanID, "sequence_id", rec.SequenceID,
			"amount_stroops", net, "error", err)
		p.alertOps("Vault repay failed after MoneyGram refund",
			fmt.Sprintf("Loan %s: refund of %d stroops landed but vault repay failed: %v",
				rec.LoanID, net, err))
		// Stay in refund_pending so the next tick retries the repay.
		return
	}

	// Terminal only once the money is genuinely back in the vault.
	if err := p.disbursement.UpdateDisbursementStatus(rec.SequenceID, statusRefundReceived); err != nil {
		p.logger.Error("vault repaid but failed to mark refund_received",
			"loan_id", rec.LoanID, "sequence_id", rec.SequenceID, "error", err)
		return
	}

	p.logger.Info("MoneyGram refund settled",
		"loan_id", rec.LoanID,
		"sequence_id", rec.SequenceID,
		"refund_tx_hash", refundHash,
		"net_stroops", net,
		"repaid_stroops", repay,
		"shortfall_stroops", shortfall,
		"excess_stroops", excess)

	if err := p.disbursement.NotifyRefundReceived(rec.SequenceID); err != nil {
		p.logger.Warn("refund SMS failed",
			"loan_id", rec.LoanID, "error", err)
	}
}

// settleFromStellarTx settles a refund using tx.stellar_transaction_id, which
// MoneyGram repoints at the refund payment once one lands. Reports whether the
// refund was found and settled.
//
// The hash alone proves nothing: before the refund exists this field names our
// own outbound payment to the anchor, and that transaction also succeeded.
// Requiring a payment *into* RefundDestination, in USDC, and taking the amount
// from the ledger is what makes the distinction safe.
func (p *Poller) settleFromStellarTx(ctx context.Context, rec LoanRecord, tx *stellaranchor.Transaction) bool {
	hash := strings.TrimSpace(tx.StellarTransactionID)
	if hash == "" || p.verifier == nil {
		return false
	}

	dest := p.refundDestination()
	if dest == "" {
		p.logger.Warn("no refund destination configured; cannot settle from stellar_transaction_id",
			"loan_id", rec.LoanID)
		return false
	}

	received, err := p.verifier.PaymentsTo(ctx, hash, dest, refundAssetCode, p.refundAssetIssuer())
	if err != nil {
		p.logger.Warn("could not read stellar_transaction_id from ledger; will retry",
			"loan_id", rec.LoanID, "tx_hash", hash, "error", err)
		return false
	}
	if len(received) == 0 {
		// Still pointing at our outbound payment: the refund has not landed.
		return false
	}

	var net int64
	for _, pay := range received {
		net += pay.AmountStroops
	}
	p.clearRefundWait(rec.LoanID)
	p.settleRefund(ctx, rec, hash, net)
	return true
}

// refundDestination is the wallet MoneyGram returns funds to — the SEP-24
// funds account the withdrawal was made from.
//
// The fallback to the SEP-10 auth account only holds where the two wallets are
// configured to the same secret, which is true in development and not in
// staging or production. Where they differ, an unset RefundDestination makes
// this look for the refund at an account it will never arrive in, and the
// refund never settles. Callers are expected to set it explicitly; New logs
// when they have not.
func (p *Poller) refundDestination() string {
	if p.cfg.RefundDestination != "" {
		return p.cfg.RefundDestination
	}
	return p.client.TreasuryAddress()
}

// refundAssetIssuer is the USDC issuer a refund must arrive from. Empty means
// the issuer is unverified and only the asset code is matched — anyone can
// issue an asset called USDC, so an empty issuer is a weaker check, not a
// neutral one.
func (p *Poller) refundAssetIssuer() string {
	if p.cfg.RefundAssetIssuer != "" {
		return p.cfg.RefundAssetIssuer
	}
	if p.client != nil {
		return p.client.USDCIssuer()
	}
	return ""
}

// awaitRefundDetails counts consecutive ticks spent waiting for MoneyGram to
// publish refund payments, and escalates once past the configured ceiling.
//
// The count is in memory: it drives alerting, not money movement, so losing it
// on restart costs at most a repeated escalation. Polling continues after the
// alert in case MG backfills the object later — what stops is the silence.
func (p *Poller) awaitRefundDetails(rec LoanRecord, tx *stellaranchor.Transaction) {
	p.refundWaitMu.Lock()
	p.refundWaits[rec.LoanID]++
	attempts := p.refundWaits[rec.LoanID]
	p.refundWaitMu.Unlock()

	max := p.cfg.RefundSettleMaxAttempts
	if max <= 0 || attempts != max {
		if attempts < max {
			p.logger.Info("refund declared but no refund payments yet; waiting",
				"loan_id", rec.LoanID, "mg_tx_id", rec.MoneyGramTxID,
				"attempt", attempts, "max_attempts", max)
		}
		return
	}

	p.logger.Error("MoneyGram declared a refund but never published refund payments",
		"loan_id", rec.LoanID, "mg_tx_id", rec.MoneyGramTxID,
		"attempts", attempts,
		"mg_stellar_tx_id", tx.StellarTransactionID,
		"amount_in", tx.AmountIn)
	p.alertOps("MoneyGram refund has no settlement details",
		fmt.Sprintf("Loan %s (MG tx %s): status=refunded after %d polls but the SEP-24 refunds "+
			"object is absent, so no refund transaction hash is available and the vault has not "+
			"been repaid. We sent %d stroops. Verify on-chain whether the anchor returned funds "+
			"and settle manually.", rec.LoanID, rec.MoneyGramTxID, attempts, rec.PrincipalStroops))
}

// clearRefundWait drops the wait counter once MG supplies refund payments, so
// a later refund on the same loan starts from zero.
func (p *Poller) clearRefundWait(loanID string) {
	p.refundWaitMu.Lock()
	delete(p.refundWaits, loanID)
	p.refundWaitMu.Unlock()
}

// refundLanded confirms every refund payment succeeded on-ledger. A nil
// verifier trusts the anchor, which is logged rather than silent.
func (p *Poller) refundLanded(ctx context.Context, rec LoanRecord, payments []stellaranchor.RefundPayment) bool {
	if p.verifier == nil {
		p.logger.Warn("no payment verifier configured; trusting MoneyGram's refund unverified",
			"loan_id", rec.LoanID, "mg_tx_id", rec.MoneyGramTxID)
		return true
	}

	for _, pay := range payments {
		ok, err := p.verifier.TransactionSucceeded(ctx, pay.ID)
		if err != nil {
			// Unknown, not failed. Retry next tick rather than assuming either
			// way — treating this as landed could overdraw the treasury.
			p.logger.Warn("could not verify refund payment; will retry",
				"loan_id", rec.LoanID, "refund_tx_hash", pay.ID, "error", err)
			return false
		}
		if !ok {
			p.logger.Error("MoneyGram named a refund transaction that did not succeed on-ledger",
				"loan_id", rec.LoanID, "refund_tx_hash", pay.ID)
			p.alertOps("MoneyGram refund not on ledger",
				fmt.Sprintf("Loan %s: MG reported refund tx %s but it did not succeed on-ledger. "+
					"Vault repay withheld.", rec.LoanID, pay.ID))
			return false
		}
	}
	return true
}

// handleTerminalFailure: expired/no_market/too_small/too_large/error.
// Mark failed, notify the user. If we already sent USDC, repay vault.
func (p *Poller) handleTerminalFailure(rec LoanRecord, tx *stellaranchor.Transaction) {
	if err := p.disbursement.UpdateDisbursementStatus(rec.SequenceID, statusFailed); err != nil {
		p.logger.Error("failed to mark failed",
			"loan_id", rec.LoanID, "sequence_id", rec.SequenceID, "error", err)
	}
	if err := p.disbursement.NotifyDisbursementFailed(rec.SequenceID); err != nil {
		p.logger.Warn("failed to notify disbursement failed",
			"loan_id", rec.LoanID, "error", err)
	}
	if tx.StellarTransactionID != "" || rec.HasStellarSend {
		// USDC was sent before MG terminated. Repay the vault to keep books
		// straight — actual reconciliation happens via the inbound refund
		// memo handler, but RepayVault flips the loan accounting too.
		if err := p.disbursement.RepayVault(rec.SequenceID); err != nil {
			p.logger.Error("RepayVault failed after terminal failure",
				"loan_id", rec.LoanID, "sequence_id", rec.SequenceID, "error", err)
		}
	}
	p.alertOps("MoneyGram off-ramp terminated",
		fmt.Sprintf("Loan %s ended in MG status %s. Message: %s",
			rec.LoanID, tx.Status, tx.Message))
}

func (p *Poller) alertOps(subject, message string) {
	if p.alerts == nil {
		p.logger.Warn("ops alert", "subject", subject, "message", message)
		return
	}
	if err := p.alerts.AlertOps(subject, message); err != nil {
		p.logger.Warn("failed to send ops alert", "subject", subject, "error", err)
	}
}

// parseDecimal turns MG's stringly-typed amount fields into a float64.
// Empty input returns (0, nil). Malformed returns an error so the
// caller can decide to skip drift checking rather than alert on garbage.
func parseDecimal(s string) (float64, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, nil
	}
	var v float64
	_, err := fmt.Sscanf(s, "%f", &v)
	return v, err
}
