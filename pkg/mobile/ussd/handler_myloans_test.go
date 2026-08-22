package ussd

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Shamba-Records-Limited/microvault/pkg/contracts"
	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

// statementLoanSvc serves a fixed loan list and payoff quote.
type statementLoanSvc struct {
	fakeLoanSvc
	loans    []any
	quote    *RepaymentQuote
	loansErr error
	quoteErr error
	quotedID string
}

func (s *statementLoanSvc) GetUserLoans(context.Context, string) ([]any, error) {
	return s.loans, s.loansErr
}

func (s *statementLoanSvc) GetRepaymentQuote(_ context.Context, loanID string) (*RepaymentQuote, error) {
	s.quotedID = loanID
	if s.quoteErr != nil {
		return nil, s.quoteErr
	}
	return s.quote, nil
}

// capturingLoanNotifier records the statement it was asked to send.
type capturingLoanNotifier struct {
	contracts.LoanNotifier
	got  []contracts.LoanNotification
	fail error
}

func (c *capturingLoanNotifier) NotifyLoanStatement(_ context.Context, n contracts.LoanNotification) error {
	if c.fail != nil {
		return c.fail
	}
	c.got = append(c.got, n)
	return nil
}

func newLoansHarness(t *testing.T, loans LoanService, ln contracts.LoanNotifier) *USSDHandler {
	t.Helper()
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(mr.Close)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	sm := NewSessionManager(client, 5*time.Minute)

	reg := NewMenuRegistry()
	NewStandardLoanMenuPreset().Initialize(reg)

	user := &fakeUserSvc{user: map[string]any{"id": "u1"}, accounts: []any{map[string]any{}}}
	return NewUSSDHandler(sm, reg, user, loans, fakeRateSvc{}, &fakePINSvc{hasPIN: true}, nil, ln)
}

func loanRecord(id, ref string, due *time.Time) map[string]any {
	return map[string]any{
		"id":                     id,
		"loan_reference":         &ref,
		"status":                 "disbursed",
		"due_date":               due,
		"delivered_amount_local": int64Ptr(100000),
	}
}

func int64Ptr(v int64) *int64 { return &v }

func loansSession() *Session {
	return &Session{
		SessionID:   "sess-loans",
		PhoneNumber: "254711000111",
		UserID:      "u1",
		CurrentMenu: "my_loans",
		Language:    "en",
		Data:        map[string]any{},
	}
}

// The statement carries reference, amount and due date, and ends the session
// pointing the user at their inbox.
func TestMyLoans_SendsStatementForMostRecentLoan(t *testing.T) {
	due := time.Date(2026, 9, 21, 0, 0, 0, 0, time.UTC)
	svc := &statementLoanSvc{
		// Newest first, as the repository orders by created_at DESC.
		loans: []any{
			loanRecord("id-new", "LR-NEW", &due),
			loanRecord("id-old", "LR-OLD", &due),
		},
		quote: &RepaymentQuote{
			LoanID: "id-new", AmountUSDCStroops: 30100000, AmountLocalCents: 301000, LocalCurrency: "KES",
		},
	}
	ln := &capturingLoanNotifier{}
	h := newLoansHarness(t, svc, ln)

	resp, err := h.handleMyLoans(context.Background(), loansSession())
	if err != nil {
		t.Fatalf("handleMyLoans: %v", err)
	}
	if !strings.HasPrefix(resp, "END ") {
		t.Errorf("expected the session to end, got %q", resp)
	}
	if !strings.Contains(resp, "SMS") {
		t.Errorf("expected the screen to point at the SMS, got %q", resp)
	}

	if len(ln.got) != 1 {
		t.Fatalf("expected one statement, got %d", len(ln.got))
	}
	got := ln.got[0]
	if got.LoanReference != "LR-NEW" {
		t.Errorf("statement is for %q, want the most recent loan LR-NEW", got.LoanReference)
	}
	if svc.quotedID != "id-new" {
		t.Errorf("quoted loan %q, want id-new", svc.quotedID)
	}
	if got.PhoneNumber != "254711000111" {
		t.Errorf("statement addressed to %q", got.PhoneNumber)
	}
	if got.DueDate == nil || !got.DueDate.Equal(due) {
		t.Errorf("DueDate = %v, want %v", got.DueDate, due)
	}
}

// The amount is the live payoff — principal grown by the vault borrow_index
// plus fees — not the sum that was disbursed.
func TestMyLoans_AmountComesFromTheVaultQuoteNotTheDisbursement(t *testing.T) {
	due := time.Now().Add(30 * 24 * time.Hour)
	svc := &statementLoanSvc{
		// Disbursed KES 1,000.00; the quote says KES 3,010.00 is owed.
		loans: []any{loanRecord("id-1", "LR-1", &due)},
		quote: &RepaymentQuote{
			LoanID: "id-1", AmountUSDCStroops: 30100000, AmountLocalCents: 301000, LocalCurrency: "KES",
		},
	}
	ln := &capturingLoanNotifier{}
	h := newLoansHarness(t, svc, ln)

	if _, err := h.handleMyLoans(context.Background(), loansSession()); err != nil {
		t.Fatalf("handleMyLoans: %v", err)
	}
	got := ln.got[0]
	if got.DisplayAmount != 3010.00 {
		t.Errorf("DisplayAmount = %v, want the quoted payoff 3010.00 (not the disbursed 1000.00)", got.DisplayAmount)
	}
	if got.DisplayCurrency != "KES" {
		t.Errorf("DisplayCurrency = %q, want the quote's currency", got.DisplayCurrency)
	}
	if got.Amount != 30100000 {
		t.Errorf("Amount = %d, want the quoted stroops", got.Amount)
	}
}

// The quote hard-fails rather than serving a stale figure, so a borrower is
// never texted a number nobody stands behind.
func TestMyLoans_QuoteFailureSendsNothing(t *testing.T) {
	due := time.Now().Add(24 * time.Hour)
	svc := &statementLoanSvc{
		loans:    []any{loanRecord("id-1", "LR-1", &due)},
		quoteErr: errors.New("read vault borrow_index: rpc down"),
	}
	ln := &capturingLoanNotifier{}
	h := newLoansHarness(t, svc, ln)

	resp, err := h.handleMyLoans(context.Background(), loansSession())
	if err != nil {
		t.Fatalf("handleMyLoans: %v", err)
	}
	if len(ln.got) != 0 {
		t.Errorf("no statement should be sent without a quote, got %+v", ln.got)
	}
	if strings.Contains(resp, "SMS") {
		t.Errorf("must not claim an SMS was sent: %q", resp)
	}
}

func TestMyLoans_NoLoansEndsWithoutSending(t *testing.T) {
	ln := &capturingLoanNotifier{}
	h := newLoansHarness(t, &statementLoanSvc{}, ln)

	resp, err := h.handleMyLoans(context.Background(), loansSession())
	if err != nil {
		t.Fatalf("handleMyLoans: %v", err)
	}
	if len(ln.got) != 0 {
		t.Errorf("nothing to state, but a statement was sent: %+v", ln.got)
	}
	if !strings.Contains(resp, "no loans") {
		t.Errorf("expected the no-loans message, got %q", resp)
	}
}

// A send failure must not tell the user their SMS is on the way.
func TestMyLoans_SendFailureIsSurfaced(t *testing.T) {
	due := time.Now().Add(24 * time.Hour)
	svc := &statementLoanSvc{
		loans: []any{loanRecord("id-1", "LR-1", &due)},
		quote: &RepaymentQuote{LoanID: "id-1", AmountLocalCents: 301000, LocalCurrency: "KES"},
	}
	ln := &capturingLoanNotifier{fail: errors.New("transport down")}
	h := newLoansHarness(t, svc, ln)

	resp, err := h.handleMyLoans(context.Background(), loansSession())
	if err != nil {
		t.Fatalf("handleMyLoans: %v", err)
	}
	if strings.Contains(resp, "SMS") {
		t.Errorf("a failed send must not report success: %q", resp)
	}
}

// A record with no reference gives the borrower nothing to quote at an agent.
func TestMyLoans_RecordWithoutReferenceSendsNothing(t *testing.T) {
	empty := ""
	svc := &statementLoanSvc{loans: []any{map[string]any{
		"id": "id-1", "loan_reference": &empty, "status": "pending",
	}}}
	ln := &capturingLoanNotifier{}
	h := newLoansHarness(t, svc, ln)

	if _, err := h.handleMyLoans(context.Background(), loansSession()); err != nil {
		t.Fatalf("handleMyLoans: %v", err)
	}
	if len(ln.got) != 0 {
		t.Errorf("a reference-less loan must not be stated: %+v", ln.got)
	}
	if svc.quotedID != "" {
		t.Errorf("no quote should be requested for an unusable record, got %q", svc.quotedID)
	}
}

// The session language rides along so the statement is rendered in it.
func TestMyLoans_StatementCarriesSessionLanguage(t *testing.T) {
	due := time.Now().Add(24 * time.Hour)
	svc := &statementLoanSvc{
		loans: []any{loanRecord("id-1", "LR-1", &due)},
		quote: &RepaymentQuote{LoanID: "id-1", AmountLocalCents: 301000, LocalCurrency: "KES"},
	}
	ln := &capturingLoanNotifier{}
	h := newLoansHarness(t, svc, ln)

	session := loansSession()
	session.Language = "sw"
	if _, err := h.handleMyLoans(context.Background(), session); err != nil {
		t.Fatalf("handleMyLoans: %v", err)
	}
	if ln.got[0].Language != "sw" {
		t.Errorf("Language = %q, want sw", ln.got[0].Language)
	}
}

// A loan that has not disbursed has no origination borrow_index, so quoting it
// would fail for a reason that is not an error. Say it is processing instead.
func TestMyLoans_UndisbursedLoanReportsProcessing(t *testing.T) {
	for _, status := range []string{"pending", "approved", "cancelled"} {
		ref := "LR-1"
		svc := &statementLoanSvc{loans: []any{map[string]any{
			"id": "id-1", "loan_reference": &ref, "status": status,
		}}}
		ln := &capturingLoanNotifier{}
		h := newLoansHarness(t, svc, ln)

		resp, err := h.handleMyLoans(context.Background(), loansSession())
		if err != nil {
			t.Fatalf("%s: %v", status, err)
		}
		if svc.quotedID != "" {
			t.Errorf("%s: must not quote a loan with no borrow_index", status)
		}
		if len(ln.got) != 0 {
			t.Errorf("%s: must not send a statement", status)
		}
		if !strings.Contains(resp, "still being processed") {
			t.Errorf("%s: expected the processing message, got %q", status, resp)
		}
		if strings.Contains(resp, "error occurred") {
			t.Errorf("%s: must not read as a failure: %q", status, resp)
		}
	}
}

// Post-disbursement states are quotable, including a settled loan.
func TestMyLoans_SettledStatesAreStillQuotable(t *testing.T) {
	for _, status := range []string{"disbursed", "repaid", "defaulted"} {
		due := time.Now().Add(24 * time.Hour)
		rec := loanRecord("id-1", "LR-1", &due)
		rec["status"] = status
		svc := &statementLoanSvc{
			loans: []any{rec},
			quote: &RepaymentQuote{LoanID: "id-1", AmountLocalCents: 301000, LocalCurrency: "KES"},
		}
		ln := &capturingLoanNotifier{}
		h := newLoansHarness(t, svc, ln)

		if _, err := h.handleMyLoans(context.Background(), loansSession()); err != nil {
			t.Fatalf("%s: %v", status, err)
		}
		if len(ln.got) != 1 {
			t.Errorf("%s: expected a statement, got %d", status, len(ln.got))
		}
	}
}
