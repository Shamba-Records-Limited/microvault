package ussd

import (
	"context"
	"errors"
	"strings"
	"testing"
)

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

func loanRow(id, ref, status string) map[string]any {
	return map[string]any{"id": id, "loan_reference": &ref, "status": status}
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

	resp, _ := h.handleRepayRail(context.Background(), session, "1")

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

	resp, _ := h.handleRepayRail(context.Background(), session, "2")

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
//
// The latency guarantee itself belongs to the adapter, which runs the handshake
// on its own goroutine. What is testable here is the coupling that would undo
// it: if this screen ever reads a field off the initiation again, the handler
// has to wait for it.
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
