// Package mgpoller polls MoneyGram for SEP-24 transaction status changes
// and drives the existing disbursement state machine accordingly.
//
// MoneyGram does not publish webhooks. The integration plan in
// internal-docs/moneygram-integration.md §10 makes polling the canonical
// driver for cash-pickup loans:
//
//   1. After InitiateWithdrawal, MG returns an interactive URL. The user
//      opens it (delivered via SMS by the USSD layer) and completes KYC.
//   2. MG transitions the SEP-24 transaction to pending_user_transfer_start.
//      The poller observes this and sends USDC from treasury to MG's
//      anchor account using the memo embedded in the tx response.
//   3. MG processes the payment and transitions to
//      pending_user_transfer_complete. The poller backfills the locked
//      payout amount, currency, and cash-pickup reference number on the
//      loan row, then SMSes the reference to the user.
//   4. completed / refunded / expired / error are terminal; the poller
//      transitions disbursement_status accordingly and stops polling.
//
// The poller mirrors pkg/webhook/refund_poller.go's lifecycle conventions:
// Start(ctx) runs a ticker loop and exits on context cancellation.
package mgpoller

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/Shamba-Records-Limited/microvault/pkg/mobile/ussd/adapters"
	"github.com/Shamba-Records-Limited/microvault/pkg/payment/moneygram"
)

// LoanRecord is the projection of a loan row the poller needs to drive
// state for a single MoneyGram cash-pickup transaction.
//
// The implementation in microvault-credit (loan repository) constructs
// these from active rows where ramp_provider="moneygram" and
// disbursement_status is in the active set.
type LoanRecord struct {
	LoanID                string
	SequenceID            string  // = loans.ramp_sequence_id
	MoneyGramTxID         string  // = loans.ramp_request_id
	ChildAccountIndex     uint32  // for SEP-10 memo re-derivation
	PrincipalStroops      int64   // USDC stroops to send to MG anchor
	RequestedLocalAmount  float64 // KES the user typed in USSD; used for drift alerts
	HasStellarSend        bool    // true once MG.tx.stellar_transaction_id is observed
	DisbursementStatus    string
	PhoneNumber           string
	UserID                string
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
	RecordTransactionUpdate(ctx context.Context, loanID string, tx *moneygram.Transaction) error

	// RecordSendUSDC records the Stellar tx hash from a successful USDC
	// transfer to MG's anchor account. Optional — implementations may
	// no-op if a dedicated column hasn't been added yet (the poller will
	// rely on MG's tx.stellar_transaction_id as the next-best idempotency
	// marker).
	RecordSendUSDC(ctx context.Context, loanID string, txHash string) error
}

// DisbursementUpdater drives terminal state transitions and user
// notifications. The microvault-credit DisbursementStatusAdapter already
// implements this for the YC flow; the same impl is reused here.
type DisbursementUpdater interface {
	UpdateDisbursementStatus(sequenceID string, status string) error
	NotifyDisbursementComplete(sequenceID string) error
	NotifyDisbursementFailed(sequenceID string) error
	RepayVault(sequenceID string) error
}

// AlertService is the same interface used by the YC refund poller —
// receives ops alerts when something needs human attention. Optional.
type AlertService interface {
	AlertOps(subject, message string) error
}

// Disbursement status strings the poller writes. These match the canonical
// constants in microvault-credit/internal/credit/app/models/loan.go
// (DisbursementStatus*). We hardcode them here because this package can't
// import from microvault-credit without a layering inversion.
//
// statusRefundPending is intentionally not in microvault-credit's enum yet
// — added inline here to mirror what the existing YC flow writes; reconcile
// in a follow-up that also fixes YC's "complete" vs the model's "completed".
const (
	statusProcessing    = "processing"
	statusCompleted     = "completed"
	statusFailed        = "failed"
	statusRefundPending = "refund_pending"
)

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
}

// DefaultConfig returns a sensible PollerConfig.
func DefaultConfig() PollerConfig {
	return PollerConfig{
		PollInterval:        30 * time.Second,
		MaxBatch:            100,
		PayoutDriftAlertPct: 0.02,
	}
}

// Poller drives the MoneyGram cash-pickup state machine.
type Poller struct {
	client       *moneygram.Client
	fetcher      LoanFetcher
	recorder     LoanRecorder
	disbursement DisbursementUpdater
	treasury     adapters.TreasuryTransfer
	alerts       AlertService
	cfg          PollerConfig
	logger       *slog.Logger
}

// NewPoller validates the dependencies and returns a Poller. client,
// fetcher, recorder, disbursement, and treasury are required; alerts and
// logger may be nil.
func NewPoller(
	client *moneygram.Client,
	fetcher LoanFetcher,
	recorder LoanRecorder,
	disbursement DisbursementUpdater,
	treasury adapters.TreasuryTransfer,
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
	if logger == nil {
		logger = slog.Default()
	}
	return &Poller{
		client:       client,
		fetcher:      fetcher,
		recorder:     recorder,
		disbursement: disbursement,
		treasury:     treasury,
		alerts:       alerts,
		cfg:          cfg,
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

// poll runs a single cycle: fetch active MG loans → drive each.
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

	childMemo := moneygram.ChildAccountMemo(p.client.TreasuryAddress(), rec.ChildAccountIndex)
	tx, err := p.client.GetTransaction(ctx, childMemo, rec.MoneyGramTxID)
	if err != nil {
		p.logger.Error("GetTransaction failed",
			"loan_id", rec.LoanID,
			"mg_tx_id", rec.MoneyGramTxID,
			"error", err)
		return
	}

	// Always persist the latest fields — pending_user_transfer_complete
	// is the one that matters most (carries amount_out + reference) but
	// having amount_in available earlier is harmless.
	if err := p.recorder.RecordTransactionUpdate(ctx, rec.LoanID, tx); err != nil {
		p.logger.Warn("failed to persist transaction update",
			"loan_id", rec.LoanID, "error", err)
	}

	switch tx.Status {
	case moneygram.StatusPendingUserTransferStart:
		p.handlePendingUserTransferStart(ctx, rec, tx)

	case moneygram.StatusPendingUserTransferComplete:
		p.handlePendingUserTransferComplete(ctx, rec, tx)

	case moneygram.StatusCompleted:
		p.handleCompleted(rec, tx)

	case moneygram.StatusRefunded:
		p.handleRefunded(rec, tx)

	case moneygram.StatusExpired,
		moneygram.StatusNoMarket,
		moneygram.StatusTooSmall,
		moneygram.StatusTooLarge,
		moneygram.StatusError:
		p.handleTerminalFailure(rec, tx)

	case moneygram.StatusOnHold:
		// MG paused the transaction for additional checks (compliance,
		// fraud review). Not terminal, but it can sit here for a while —
		// alert ops so a human can chase MG support if needed.
		p.logger.Warn("MoneyGram transaction on hold",
			"loan_id", rec.LoanID, "mg_tx_id", rec.MoneyGramTxID,
			"message", tx.Message)
		p.alertOps("MoneyGram transaction on hold",
			fmt.Sprintf("Loan %s: MG on_hold for additional checks. Message: %s",
				rec.LoanID, tx.Message))

	case moneygram.StatusPendingTrust:
		// User-side trustline missing. In our custodial-wallet model the
		// user never holds USDC directly, so this only arises on the MG
		// side and likely indicates an anchor-config issue worth flagging.
		p.logger.Warn("MoneyGram pending_trust — anchor missing trustline?",
			"loan_id", rec.LoanID, "mg_tx_id", rec.MoneyGramTxID,
			"message", tx.Message)
		p.alertOps("MoneyGram pending_trust",
			fmt.Sprintf("Loan %s: MG reported pending_trust. Investigate anchor trustline. Message: %s",
				rec.LoanID, tx.Message))

	case moneygram.StatusPendingUser:
		// MG is waiting on the user (additional KYC, action at the agent,
		// etc.). Nothing automated to do — log for visibility and let
		// the existing reminder SMS path handle nudges.
		p.logger.Info("MoneyGram waiting on user action",
			"loan_id", rec.LoanID, "mg_tx_id", rec.MoneyGramTxID,
			"message", tx.Message)

	case moneygram.StatusIncomplete,
		moneygram.StatusPendingAnchor,
		moneygram.StatusPendingExternal,
		moneygram.StatusPendingStellar:
		// In-flight, no action needed this cycle. pending_stellar is
		// the natural transient state right after our SendUSDC while
		// the network confirms the payment to MG's anchor.

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
func (p *Poller) handlePendingUserTransferStart(ctx context.Context, rec LoanRecord, tx *moneygram.Transaction) {
	if tx.StellarTransactionID != "" || rec.HasStellarSend {
		// Already observed — wait for MG to advance.
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
		// Don't transition — MG keeps the tx in pending_user_transfer_start
		// until either we send or the tx expires. Next tick retries.
		p.alertOps("MoneyGram USDC send failed",
			fmt.Sprintf("Loan %s: SendUSDC to %s failed: %v",
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
func (p *Poller) handlePendingUserTransferComplete(_ context.Context, rec LoanRecord, tx *moneygram.Transaction) {
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
func (p *Poller) handleCompleted(rec LoanRecord, _ *moneygram.Transaction) {
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

// handleRefunded: MG initiated a refund. Mark refund_pending — the
// Stellar ingest worker watches for the inbound USDC payment by memo
// and transitions to refund_received when it arrives. Vault repay
// happens after the refund is observed (out of this poller's scope).
func (p *Poller) handleRefunded(rec LoanRecord, _ *moneygram.Transaction) {
	if err := p.disbursement.UpdateDisbursementStatus(rec.SequenceID, statusRefundPending); err != nil {
		p.logger.Error("failed to mark refund_pending",
			"loan_id", rec.LoanID, "sequence_id", rec.SequenceID, "error", err)
	}
	p.logger.Info("MoneyGram refunded — awaiting inbound USDC",
		"loan_id", rec.LoanID, "sequence_id", rec.SequenceID)
}

// handleTerminalFailure: expired/no_market/too_small/too_large/error.
// Mark failed, notify the user. If we already sent USDC, repay vault.
func (p *Poller) handleTerminalFailure(rec LoanRecord, tx *moneygram.Transaction) {
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
