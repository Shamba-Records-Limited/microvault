package ussd

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/Shamba-Records-Limited/microvault/pkg/contracts"
)

// paybillNotifier records the paybill SMS it was asked to send.
type paybillNotifier struct {
	contracts.LoanNotifier
	sent []contracts.LoanNotification
}

func (c *paybillNotifier) NotifyRepaymentPaybill(_ context.Context, n contracts.LoanNotification) error {
	c.sent = append(c.sent, n)
	return nil
}

// repayLoanSvc is a LoanService whose repay surface is controllable.
type repayLoanSvc struct {
	fakeLoanSvc

	loans       []any
	quotes      map[string]*RepaymentQuote
	quoteErr    error
	initiated   []string
	initiateErr error
}
func (s *repayLoanSvc) GetUserLoans(context.Context, string) ([]any, error) {
	return s.loans, nil
}

func (s *repayLoanSvc) GetRepaymentQuote(_ context.Context, loanID string) (*RepaymentQuote, error) {
	if s.quoteErr != nil {
		return nil, s.quoteErr
	}
	q, ok := s.quotes[loanID]
	if !ok {
		return nil, errors.New("no quote")
	}
	return q, nil
}

func (s *repayLoanSvc) InitiateRepayment(_ context.Context, loanID, _ string) error {
	if s.initiateErr != nil {
		return s.initiateErr
	}
	s.initiated = append(s.initiated, loanID)
	return nil
}

// promptingLoanSvc adds an M-Pesa prompt surface to the repay fake.
type promptingLoanSvc struct {
	repayLoanSvc
	prompted  []string
	promptErr error
}

func (s *promptingLoanSvc) PromptRepayment(_ context.Context, loanID, _ string) error {
	if s.promptErr != nil {
		return s.promptErr
	}
	s.prompted = append(s.prompted, loanID)
	return nil
}

func loanRow(id, ref, status string) map[string]any {
	return map[string]any{"id": id, "loan_reference": &ref, "status": status}
}

func loanRowRepaying(id, ref, status, repaymentStatus string) map[string]any {
	row := loanRow(id, ref, status)
	row["repayment_status"] = repaymentStatus
	return row
}

// loanRowRepayingVia is loanRowRepaying with the rail the in-flight repayment
// belongs to — the menu reads the provider to tell an M-Pesa prompt from a
// MoneyGram cash deposit.
func loanRowRepayingVia(id, ref, status, repaymentStatus, provider string) map[string]any {
	row := loanRowRepaying(id, ref, status, repaymentStatus)
	row["repayment_provider"] = provider
	return row
}

// newRepayHarness wires a handler whose only interesting dependency is the
// loan service. paybill may be blank to model an unconfigured builder.
func newRepayHarness(t *testing.T, svc *repayLoanSvc, paybill string) *USSDHandler {
	t.Helper()
	h := newHarness(t, &fakeUserSvc{user: map[string]any{"id": "u1"}}, &fakePINSvc{hasPIN: true})
	h.loanService = svc
	h.repayPaybill = paybill
	return h
}

// newPromptHarness enables the STK prompt rail, modelling a builder with
// Daraja configured.
func newPromptHarness(t *testing.T, svc *promptingLoanSvc, paybill string) *USSDHandler {
	t.Helper()
	h := newRepayHarness(t, &svc.repayLoanSvc, paybill)
	h.mpesaPrompter = svc
	h.mpesaPromptOn = true
	return h
}

func repaySession() *Session {
	return &Session{
		SessionID:   "sess-repay",
		PhoneNumber: "254711000111",
		UserID:      "u1",
		Language:    "en",
		CurrentMenu: "repay_loan",
		Data:        map[string]any{},
	}
}

// aboveFloor is comfortably over MoneyGram's 15 USDC minimum; belowFloor is a
// KES 1,000-sized loan, which the cash rail cannot accept at all.
const (
	aboveFloor int64 = 500_000_000 // 50 USDC
	belowFloor int64 = 70_000_000  // 7 USDC
)

// GetUserLoans returns newest first, and the product allows one active loan at
// a time, so repay acts on the first outstanding loan and never offers a
// choice. Anything already settled or not yet disbursed is skipped.
func TestRepay_PicksNewestOutstandingLoan(t *testing.T) {
	svc := &repayLoanSvc{
		loans: []any{
			loanRow("l0", "LN-0", "pending"),   // newest, not yet disbursed
			loanRow("l1", "LN-1", "disbursed"), // the one to act on
			loanRow("l2", "LN-2", "disbursed"), // older, must not win
		},
		quotes: map[string]*RepaymentQuote{
			"l1": {AmountUSDCStroops: aboveFloor, AmountLocalCents: 645000, LocalCurrency: "KES"},
			"l2": {AmountUSDCStroops: aboveFloor, AmountLocalCents: 999900, LocalCurrency: "KES"},
		},
	}
	h := newRepayHarness(t, svc, "247247")

	resp, err := h.handleRepayLoan(context.Background(), repaySession(), "")
	if err != nil {
		t.Fatalf("handleRepayLoan: %v", err)
	}
	if !strings.Contains(resp, "LN-1") {
		t.Errorf("expected the newest outstanding loan: %q", resp)
	}
	for _, notWant := range []string{"LN-0", "LN-2"} {
		if strings.Contains(resp, notWant) {
			t.Errorf("%s must not appear: %q", notWant, resp)
		}
	}
	// Straight to the rail choice — there is no list to select from.
	if !strings.Contains(resp, "Mobile money") {
		t.Errorf("expected the rail menu on first entry: %q", resp)
	}
}

func TestRepay_NoOutstandingLoan_EndsSession(t *testing.T) {
	svc := &repayLoanSvc{loans: []any{loanRow("l1", "LN-1", "repaid")}}
	h := newRepayHarness(t, svc, "247247")

	resp, _ := h.handleRepayLoan(context.Background(), repaySession(), "")

	if !strings.HasPrefix(resp, "END ") {
		t.Errorf("nothing to repay: %q", resp)
	}
}

// The borrower reads local currency. The figure comes from the quote's FX
// cascade — MoneyGram's rate once its credentials are configured, YellowCard's
// until then.
func TestRepay_QuotesInLocalCurrency(t *testing.T) {
	svc := &repayLoanSvc{
		loans:  []any{loanRow("l1", "LN-1", "disbursed")},
		quotes: map[string]*RepaymentQuote{"l1": {AmountUSDCStroops: aboveFloor, AmountLocalCents: 645000, LocalCurrency: "KES"}},
	}
	h := newRepayHarness(t, svc, "247247")

	resp, _ := h.handleRepayLoan(context.Background(), repaySession(), "")

	if !strings.Contains(resp, "KES 6450.00") {
		t.Errorf("expected the local-currency payoff: %q", resp)
	}
}

// A failed FX cascade leaves no local figure. The screen falls back to USDC
// rather than showing nothing — the borrower can still act on it, and the
// deposit settles in USDC regardless.
func TestRepay_FallsBackToUSDCWhenFXUnavailable(t *testing.T) {
	svc := &repayLoanSvc{
		loans:  []any{loanRow("l1", "LN-1", "disbursed")},
		quotes: map[string]*RepaymentQuote{"l1": {AmountUSDCStroops: aboveFloor}},
	}
	h := newRepayHarness(t, svc, "247247")

	resp, _ := h.handleRepayLoan(context.Background(), repaySession(), "")

	if !strings.Contains(resp, "USDC 50.00") {
		t.Errorf("expected the USDC fallback: %q", resp)
	}
}

func TestRepay_CashRailHiddenBelowMoneyGramFloor(t *testing.T) {
	svc := &repayLoanSvc{
		loans:  []any{loanRow("l1", "LN-1", "disbursed")},
		quotes: map[string]*RepaymentQuote{"l1": {AmountUSDCStroops: belowFloor}},
	}
	h := newRepayHarness(t, svc, "247247")

	session := repaySession()
	resp, err := h.handleRepayLoan(context.Background(), session, "1")
	if err != nil {
		t.Fatalf("select: %v", err)
	}
	if strings.Contains(resp, "MoneyGram") {
		t.Errorf("a 7 USDC payoff is under MoneyGram's 15 USDC floor and must not be offered: %q", resp)
	}
	if !strings.Contains(resp, "Mobile money") {
		t.Errorf("mobile money has no floor and must remain available: %q", resp)
	}
}

func TestRepay_CashRailOfferedAboveFloor(t *testing.T) {
	svc := &repayLoanSvc{
		loans:  []any{loanRow("l1", "LN-1", "disbursed")},
		quotes: map[string]*RepaymentQuote{"l1": {AmountUSDCStroops: aboveFloor}},
	}
	h := newRepayHarness(t, svc, "247247")

	resp, _ := h.handleRepayLoan(context.Background(), repaySession(), "1")

	if !strings.Contains(resp, "MoneyGram") {
		t.Errorf("expected the cash rail to be offered: %q", resp)
	}
}

func TestRepay_CashRailInitiatesAndEndsTheSession(t *testing.T) {
	svc := &repayLoanSvc{
		loans:  []any{loanRow("l1", "LN-1", "disbursed")},
		quotes: map[string]*RepaymentQuote{"l1": {AmountUSDCStroops: aboveFloor}},
	}
	h := newRepayHarness(t, svc, "247247")

	session := repaySession()
	if _, err := h.handleRepayLoan(context.Background(), session, "1"); err != nil {
		t.Fatalf("select: %v", err)
	}

	resp, err := h.handleRepayRail(context.Background(), session, "1")
	if err != nil {
		t.Fatalf("rail: %v", err)
	}
	if len(svc.initiated) != 1 || svc.initiated[0] != "l1" {
		t.Fatalf("expected the deposit to be opened for l1, got %v", svc.initiated)
	}
	if !strings.HasPrefix(resp, "END ") {
		t.Errorf("the session must end: the link cannot be followed from a USSD screen: %q", resp)
	}
	if !strings.Contains(resp, "SMS") {
		t.Errorf("the borrower must be told where the link went: %q", resp)
	}
}

// Below the floor the cash option is never rendered, so "1" is the paybill.
// Getting this wrong would open a deposit MoneyGram is certain to reject.
func TestRepay_FirstOptionIsPaybillWhenCashRailHidden(t *testing.T) {
	svc := &repayLoanSvc{
		loans:  []any{loanRow("l1", "LN-1", "disbursed")},
		quotes: map[string]*RepaymentQuote{"l1": {AmountUSDCStroops: belowFloor}},
	}
	h := newRepayHarness(t, svc, "247247")

	session := repaySession()
	if _, err := h.handleRepayLoan(context.Background(), session, "1"); err != nil {
		t.Fatalf("select: %v", err)
	}

	// Below the cash floor the only rail is mobile money.
	sub, _ := h.handleRepayRail(context.Background(), session, "1")
	if !strings.Contains(sub, "Get PayBill") {
		t.Fatalf("expected the paybill option in the mobile-money menu: %q", sub)
	}

	resp, _ := h.handleRepayMobile(context.Background(), session, "1")

	if len(svc.initiated) != 0 {
		t.Error("a below-floor payoff must never reach MoneyGram")
	}
	if !strings.Contains(resp, "247247") {
		t.Errorf("expected the paybill: %q", resp)
	}
}

func TestRepay_PaybillShowsLoanReferenceAsAccount(t *testing.T) {
	svc := &repayLoanSvc{
		loans:  []any{loanRow("l1", "LN-1", "disbursed")},
		quotes: map[string]*RepaymentQuote{"l1": {AmountUSDCStroops: aboveFloor}},
	}
	h := newRepayHarness(t, svc, "247247")

	session := repaySession()
	if _, err := h.handleRepayLoan(context.Background(), session, "1"); err != nil {
		t.Fatalf("select: %v", err)
	}

	// Cash is rail 1, mobile money rail 2; paybill is the only sub-option
	// without a prompter wired.
	if _, err := h.handleRepayRail(context.Background(), session, "2"); err != nil {
		t.Fatalf("rail: %v", err)
	}
	resp, _ := h.handleRepayMobile(context.Background(), session, "1")

	// The reference is what attributes an unsolicited paybill payment to a
	// loan; without it the money arrives with nothing tying it to a borrower.
	if !strings.Contains(resp, "LN-1") {
		t.Errorf("expected the loan reference as the account number: %q", resp)
	}
}

func TestRepay_NoPaybillConfigured_HidesMobileMoney(t *testing.T) {
	svc := &repayLoanSvc{
		loans:  []any{loanRow("l1", "LN-1", "disbursed")},
		quotes: map[string]*RepaymentQuote{"l1": {AmountUSDCStroops: aboveFloor}},
	}
	h := newRepayHarness(t, svc, "")

	resp, _ := h.handleRepayLoan(context.Background(), repaySession(), "1")

	if strings.Contains(resp, "Mobile money") {
		t.Errorf("an unconfigured paybill must not be offered: %q", resp)
	}
	if !strings.Contains(resp, "MoneyGram") {
		t.Errorf("the cash rail is still available: %q", resp)
	}
}

func TestRepay_NoRailAvailable_SaysSo(t *testing.T) {
	svc := &repayLoanSvc{
		loans:  []any{loanRow("l1", "LN-1", "disbursed")},
		quotes: map[string]*RepaymentQuote{"l1": {AmountUSDCStroops: belowFloor}},
	}
	h := newRepayHarness(t, svc, "")

	resp, _ := h.handleRepayLoan(context.Background(), repaySession(), "1")

	if !strings.HasPrefix(resp, "END ") {
		t.Errorf("with no rail available the session must end rather than show an empty menu: %q", resp)
	}
}

func TestRepay_InitiationFailure_DoesNotClaimAnSMSWasSent(t *testing.T) {
	svc := &repayLoanSvc{
		loans:       []any{loanRow("l1", "LN-1", "disbursed")},
		quotes:      map[string]*RepaymentQuote{"l1": {AmountUSDCStroops: aboveFloor}},
		initiateErr: errors.New("anchor unreachable"),
	}
	h := newRepayHarness(t, svc, "247247")

	session := repaySession()
	if _, err := h.handleRepayLoan(context.Background(), session, "1"); err != nil {
		t.Fatalf("select: %v", err)
	}

	resp, _ := h.handleRepayRail(context.Background(), session, "1")

	if strings.Contains(resp, "SMS") {
		t.Errorf("no deposit was opened, so no SMS is coming: %q", resp)
	}
}

func TestRepay_NoOutstandingLoans_EndsSession(t *testing.T) {
	svc := &repayLoanSvc{loans: []any{loanRow("l1", "LN-1", "repaid")}}
	h := newRepayHarness(t, svc, "247247")

	resp, _ := h.handleRepayLoan(context.Background(), repaySession(), "")

	if !strings.HasPrefix(resp, "END ") {
		t.Errorf("nothing to repay: %q", resp)
	}
}

// A loan whose quote hard-fails is still listed, with the amount blanked. It
// has no payoff, so it cannot clear the floor and the cash rail stays hidden.
func TestRepay_UnquotableLoanIsListedButCannotUseCashRail(t *testing.T) {
	svc := &repayLoanSvc{
		loans:    []any{loanRow("l1", "LN-1", "disbursed")},
		quoteErr: errors.New("vault unreachable"),
	}
	h := newRepayHarness(t, svc, "247247")

	list, _ := h.handleRepayLoan(context.Background(), repaySession(), "")
	if !strings.Contains(list, "LN-1") {
		t.Errorf("the loan must still be listed: %q", list)
	}

	rail, _ := h.handleRepayLoan(context.Background(), repaySession(), "1")
	if strings.Contains(rail, "MoneyGram") {
		t.Errorf("without a payoff the floor cannot be cleared: %q", rail)
	}
}

// The screen must render from the quote it already has, never from the
// initiation result. That is what lets LoanService.InitiateRepayment return
// before MoneyGram has answered — the sandbox measured that handshake at 15.7s,
// well past the point Africa's Talking abandons a USSD session.
func TestRepay_CashRailScreenRendersFromTheQuote(t *testing.T) {
	svc := &repayLoanSvc{
		loans:  []any{loanRow("l1", "LN-1", "disbursed")},
		quotes: map[string]*RepaymentQuote{"l1": {AmountUSDCStroops: aboveFloor, AmountLocalCents: 645000, LocalCurrency: "KES"}},
	}
	h := newRepayHarness(t, svc, "247247")

	session := repaySession()
	if _, err := h.handleRepayLoan(context.Background(), session, ""); err != nil {
		t.Fatalf("select: %v", err)
	}

	resp, err := h.handleRepayRail(context.Background(), session, "1")
	if err != nil {
		t.Fatalf("rail: %v", err)
	}

	// The quoted figure, which the handler had before initiation was called.
	if !strings.Contains(resp, "KES 6450.00") {
		t.Errorf("expected the already-quoted amount: %q", resp)
	}
	if !strings.Contains(resp, "SMS") {
		t.Errorf("expected the check-your-SMS screen: %q", resp)
	}
}

// No prompt capability, no prompt option — even with the flag set — so a
// builder without a prompt-capable loan service never offers what cannot run.
func TestRepay_MpesaPromptHiddenWithoutCapability(t *testing.T) {
	svc := &repayLoanSvc{
		loans:  []any{loanRow("l1", "LN-1", "disbursed")},
		quotes: map[string]*RepaymentQuote{"l1": {AmountUSDCStroops: aboveFloor}},
	}
	h := newRepayHarness(t, svc, "")
	h.mpesaPromptOn = true

	resp, _ := h.handleRepayLoan(context.Background(), repaySession(), "")

	if strings.Contains(resp, "M-Pesa") {
		t.Errorf("no prompter behind the loan service, so no prompt option: %q", resp)
	}
}

func TestRepay_MpesaPromptOfferedWhenAvailable(t *testing.T) {
	svc := &promptingLoanSvc{repayLoanSvc: repayLoanSvc{
		loans:  []any{loanRow("l1", "LN-1", "disbursed")},
		quotes: map[string]*RepaymentQuote{"l1": {AmountUSDCStroops: aboveFloor}},
	}}
	h := newPromptHarness(t, svc, "247247")

	session := repaySession()
	if _, err := h.handleRepayLoan(context.Background(), session, ""); err != nil {
		t.Fatalf("select: %v", err)
	}

	// The prompt now lives one level down, in the mobile-money menu.
	if _, err := h.handleRepayRail(context.Background(), session, "2"); err != nil {
		t.Fatalf("rail: %v", err)
	}
	resp, _ := h.handleRepayMobile(context.Background(), session, "")

	if !strings.Contains(resp, "M-PESA") {
		t.Errorf("expected the prompt option to be offered: %q", resp)
	}
}

func TestRepay_MpesaPromptPushesAndEndsTheSession(t *testing.T) {
	svc := &promptingLoanSvc{repayLoanSvc: repayLoanSvc{
		loans:  []any{loanRow("l1", "LN-1", "disbursed")},
		quotes: map[string]*RepaymentQuote{"l1": {AmountUSDCStroops: aboveFloor}},
	}}
	h := newPromptHarness(t, svc, "247247")

	session := repaySession()
	if _, err := h.handleRepayLoan(context.Background(), session, ""); err != nil {
		t.Fatalf("select: %v", err)
	}

	// Cash is rail 1, mobile money rail 2; the prompt is sub-option 1.
	if _, err := h.handleRepayRail(context.Background(), session, "2"); err != nil {
		t.Fatalf("rail: %v", err)
	}
	resp, err := h.handleRepayMobile(context.Background(), session, "1")
	if err != nil {
		t.Fatalf("mobile: %v", err)
	}

	if len(svc.prompted) != 1 || svc.prompted[0] != "l1" {
		t.Fatalf("expected the prompt to be pushed for l1, got %v", svc.prompted)
	}
	if !strings.HasPrefix(resp, "END ") {
		t.Errorf("the session must end once the prompt is sent: %q", resp)
	}
	if !strings.Contains(resp, "PIN") {
		t.Errorf("the borrower must be told what happens next: %q", resp)
	}
}

func TestRepay_MpesaPromptRefusalShowsError(t *testing.T) {
	svc := &promptingLoanSvc{repayLoanSvc: repayLoanSvc{
		loans:  []any{loanRow("l1", "LN-1", "disbursed")},
		quotes: map[string]*RepaymentQuote{"l1": {AmountUSDCStroops: aboveFloor}},
	}, promptErr: errors.New("push declined")}
	h := newPromptHarness(t, svc, "247247")

	session := repaySession()
	if _, err := h.handleRepayLoan(context.Background(), session, ""); err != nil {
		t.Fatalf("select: %v", err)
	}

	if _, err := h.handleRepayRail(context.Background(), session, "2"); err != nil {
		t.Fatalf("rail: %v", err)
	}
	resp, _ := h.handleRepayMobile(context.Background(), session, "1")

	if strings.Contains(resp, "was sent") {
		t.Errorf("a refused push must not claim a prompt is on the phone: %q", resp)
	}
}

// An open repayment blocks every rail — there is no cancel, and a second
// payment against the same loan is a double payment we cannot reverse. The
// borrower is told to wait, and the session ends with no option to start
// another.
func TestRepay_MpesaInFlightBlocksAllRails(t *testing.T) {
	svc := &promptingLoanSvc{repayLoanSvc: repayLoanSvc{
		loans:  []any{loanRowRepayingVia("l1", "LN-1", "disbursed", "initiated", "mpesa")},
		quotes: map[string]*RepaymentQuote{"l1": {AmountUSDCStroops: aboveFloor}},
	}}
	h := newPromptHarness(t, svc, "247247")

	resp, err := h.handleRepayLoan(context.Background(), repaySession(), "")
	if err != nil {
		t.Fatalf("select: %v", err)
	}

	if !strings.HasPrefix(resp, "END ") {
		t.Errorf("an open repayment ends the session with no rail offered: %q", resp)
	}
	if !strings.Contains(resp, "in progress") {
		t.Errorf("the borrower must be told a repayment is in progress: %q", resp)
	}
}

func TestRepay_FundsReceivedBlocksAllRails(t *testing.T) {
	svc := &promptingLoanSvc{repayLoanSvc: repayLoanSvc{
		loans:  []any{loanRowRepayingVia("l1", "LN-1", "disbursed", "funds_received", "mpesa")},
		quotes: map[string]*RepaymentQuote{"l1": {AmountUSDCStroops: aboveFloor}},
	}}
	h := newPromptHarness(t, svc, "247247")

	resp, err := h.handleRepayLoan(context.Background(), repaySession(), "")
	if err != nil {
		t.Fatalf("select: %v", err)
	}

	// Landed but not yet settled still blocks: another payment could double up.
	if !strings.HasPrefix(resp, "END ") {
		t.Errorf("funds received ends the session with no rail offered: %q", resp)
	}
	if !strings.Contains(resp, "in progress") {
		t.Errorf("the borrower must be told a repayment is in progress: %q", resp)
	}
}

// A MoneyGram cash repayment in flight shares the initiated status. It blocks
// every rail like any other, and the menu names MoneyGram rather than claim an
// M-Pesa prompt is on the handset.
func TestRepay_MoneyGramInFlightBlocksAndIsNamed(t *testing.T) {
	svc := &promptingLoanSvc{repayLoanSvc: repayLoanSvc{
		loans:  []any{loanRowRepayingVia("l1", "LN-1", "disbursed", "initiated", "moneygram")},
		quotes: map[string]*RepaymentQuote{"l1": {AmountUSDCStroops: aboveFloor}},
	}}
	h := newPromptHarness(t, svc, "247247")

	resp, err := h.handleRepayLoan(context.Background(), repaySession(), "")
	if err != nil {
		t.Fatalf("select: %v", err)
	}

	if !strings.HasPrefix(resp, "END ") {
		t.Errorf("an open repayment ends the session with no rail offered: %q", resp)
	}
	if !strings.Contains(resp, "MoneyGram") {
		t.Errorf("the in-flight repayment must be named as MoneyGram: %q", resp)
	}
	if !strings.Contains(resp, "in progress") {
		t.Errorf("the borrower must be told it is in progress, not offered a rail: %q", resp)
	}
}

// The paybill screen shows the amount and shortcode, and the same details go
// out by SMS since the session ends before the borrower can act on them.
func TestRepay_PaybillTextsTheDetails(t *testing.T) {
	svc := &repayLoanSvc{
		loans:  []any{loanRow("l1", "LN-1", "disbursed")},
		quotes: map[string]*RepaymentQuote{"l1": {AmountUSDCStroops: aboveFloor, AmountLocalCents: 645000, LocalCurrency: "KES"}},
	}
	h := newRepayHarness(t, svc, "247247")
	ln := &paybillNotifier{}
	h.loanNotifier = ln

	session := repaySession()
	if _, err := h.handleRepayLoan(context.Background(), session, ""); err != nil {
		t.Fatalf("select: %v", err)
	}
	if _, err := h.handleRepayRail(context.Background(), session, "2"); err != nil {
		t.Fatalf("rail: %v", err)
	}
	resp, _ := h.handleRepayMobile(context.Background(), session, "1")

	// The full first line pins amount and paybill to their own slots — a
	// substring check passes even when the two are swapped.
	if !strings.Contains(resp, "Pay KES 6450.00 to PayBill 247247.") {
		t.Errorf("the screen must lead with amount and paybill in order: %q", resp)
	}
	if !strings.Contains(resp, "LN-1") {
		t.Errorf("the screen must carry the reference: %q", resp)
	}
	if !strings.HasPrefix(resp, "END ") {
		t.Errorf("the paybill screen ends the session: %q", resp)
	}
	if len(ln.sent) != 1 {
		t.Fatalf("expected one paybill SMS, got %d", len(ln.sent))
	}
	n := ln.sent[0]
	if n.PaybillNumber != "247247" || n.LoanReference != "LN-1" || n.DisplayAmount != 6450.00 || n.DisplayCurrency != "KES" {
		t.Errorf("SMS must carry the same details: %+v", n)
	}
}
