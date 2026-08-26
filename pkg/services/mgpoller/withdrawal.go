package mgpoller

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/Shamba-Records-Limited/microvault/pkg/payment/stellaranchor"
	"github.com/Shamba-Records-Limited/microvault/pkg/stellar/types"
)

// This file holds the withdrawal direction: cash out, treasury to MoneyGram to
// an agent counter. It is one implementation of Runner's Driver — the state
// machine only, with the ticker and batching left to runner.go.

// Disbursement status strings the poller writes, duplicated from the lending
// module's loan model to avoid a layering inversion. statusRefundPending is not
// in that enum yet.
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
		// In-flight elsewhere; status is logged unconditionally above.

	default:
		p.logger.Warn("unexpected MoneyGram status",
			"loan_id", rec.LoanID, "status", tx.Status)
	}
}

// handlePendingUserTransferStart sends treasury USDC to MG's anchor account
// using the memo MG provided. Idempotency is best-effort; see doc.go.
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

	// Claim the send before submitting: a durable claim is what stops the next
	// tick paying twice. No claim, no send.
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
	// Cash is collectable. The status transition is the idempotency guard, so
	// the SMS is not re-sent every tick. Not terminal — polling continues.
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

// handleRefunded settles a MoneyGram refund, an expected path rather than a
// failure. Runs in stages across ticks so every step is retried; see doc.go.
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

// ledgerRefundTotal sums the refund payments as they appear on-ledger — the
// only amount safe to repay with. False leaves the loan for the next tick.
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

// settleFromStellarTx settles a refund from tx.stellar_transaction_id. The hash
// alone proves nothing — it names our own outbound payment until a refund lands,
// so a USDC payment into RefundDestination is what is actually required.
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

// refundDestination is the wallet MoneyGram returns funds to. The fallback to
// the SEP-10 auth account only holds where both use the same secret.
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

// awaitRefundDetails counts ticks spent waiting for MoneyGram to publish refund
// payments and escalates past the ceiling. The count is in memory; it only
// drives alerting.
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
