package ussd

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"

	"github.com/Shamba-Records-Limited/microvault/pkg/pin"
)

// --- service fakes ---

type fakeUserSvc struct {
	user       any
	accounts   []any
	getErr     error
	registered bool
}

func (f *fakeUserSvc) GetUserWithAccounts(context.Context, string) (any, []any, error) {
	return f.user, f.accounts, f.getErr
}
func (f *fakeUserSvc) RegisterUser(context.Context, *RegisterUserRequest) (any, []any, error) {
	f.registered = true
	return map[string]any{"id": "new-user"}, []any{map[string]any{}}, nil
}
func (f *fakeUserSvc) NationalIDExists(context.Context, string) (bool, error)        { return false, nil }
func (f *fakeUserSvc) UpdateBio(context.Context, string, BioUpdate) error            { return nil }
func (f *fakeUserSvc) GetUserIDByNationalID(context.Context, string) (string, error) { return "", nil }
func (f *fakeUserSvc) RebindMobileNumber(context.Context, string, string) error      { return nil }

type fakeRateSvc struct{}

func (fakeRateSvc) GetExchangeRate(context.Context, string) (float64, error) { return 130, nil }

type fakeLoanSvc struct{}

func (fakeLoanSvc) GetUserLoans(context.Context, string) ([]any, error)    { return nil, nil }
func (fakeLoanSvc) RequestLoan(context.Context, *LoanRequest) (any, error) { return nil, nil }
func (fakeLoanSvc) CheckLoanEligibility(context.Context, string, int64, int) (*LoanApproval, error) {
	return nil, nil
}
func (fakeLoanSvc) GetProductConfig() *LoanProductConfig {
	return &LoanProductConfig{MinAmountCents: 50000, MaxAmountCents: 300000, Currency: "KES", DurationDays: 30}
}
func (fakeLoanSvc) GetRepaymentQuote(context.Context, string) (*RepaymentQuote, error) {
	return nil, nil
}

type fakePINSvc struct {
	hasPIN bool
}

func (f *fakePINSvc) SetPIN(context.Context, string, string) error            { return nil }
func (f *fakePINSvc) VerifyPIN(context.Context, string, string) (bool, error) { return true, nil }
func (f *fakePINSvc) ChangePIN(context.Context, string, string, string) error { return nil }
func (f *fakePINSvc) ResetPIN(context.Context, string, string) error          { return nil }
func (f *fakePINSvc) IsLocked(context.Context, string) (bool, time.Time, error) {
	return false, time.Time{}, nil
}
func (f *fakePINSvc) HasPIN(context.Context, string) (bool, error) { return f.hasPIN, nil }
func (f *fakePINSvc) SetSecurityQuestions(context.Context, string, []pin.QuestionAnswer) error {
	return nil
}
func (f *fakePINSvc) VerifySecurityAnswers(context.Context, string, []pin.QuestionAnswer) (bool, error) {
	return true, nil
}
func (f *fakePINSvc) GetUserQuestionIDs(context.Context, string) ([]int, error) { return nil, nil }
func (f *fakePINSvc) GetRemainingAttempts(context.Context, string) (int, error) { return 3, nil }

// harness wires a handler onto a miniredis-backed session manager.
func newHarness(t *testing.T, user *fakeUserSvc, pinSvc *fakePINSvc) *USSDHandler {
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

	return NewUSSDHandler(sm, reg, user, fakeLoanSvc{}, fakeRateSvc{}, pinSvc, nil, nil)
}

func TestHandleRequest_NewUser_ShowsLanguageMenu(t *testing.T) {
	h := newHarness(t, &fakeUserSvc{getErr: errors.New("not found")}, &fakePINSvc{})
	resp, err := h.HandleRequest(context.Background(), "sess-1", "254711000111", "*384#", "63902", "")
	if err != nil {
		t.Fatalf("HandleRequest: %v", err)
	}
	if !strings.HasPrefix(resp, "CON ") {
		t.Errorf("expected a CON (continue) response, got %q", resp)
	}
}

func TestHandleRequest_RegisteredUser_ShowsMainMenu(t *testing.T) {
	user := &fakeUserSvc{user: map[string]any{"id": "u1"}, accounts: []any{map[string]any{}}}
	h := newHarness(t, user, &fakePINSvc{hasPIN: true})
	resp, err := h.HandleRequest(context.Background(), "sess-2", "254711000111", "*384#", "63902", "")
	if err != nil {
		t.Fatalf("HandleRequest: %v", err)
	}
	if !strings.HasPrefix(resp, "CON ") || !strings.Contains(resp, "Request Loan") {
		t.Errorf("expected main menu, got %q", resp)
	}
}

func TestHandleRequest_RegisteredNoPIN_RoutesToPinCreate(t *testing.T) {
	user := &fakeUserSvc{user: map[string]any{"id": "u1"}, accounts: []any{}}
	h := newHarness(t, user, &fakePINSvc{hasPIN: false})
	resp, err := h.HandleRequest(context.Background(), "sess-3", "254711000111", "*384#", "63902", "")
	if err != nil {
		t.Fatalf("HandleRequest: %v", err)
	}
	// PIN self-heal path: a registered user without a PIN is sent to create one.
	if !strings.HasPrefix(resp, "CON ") {
		t.Errorf("expected a continue response into pin_create, got %q", resp)
	}
}

func TestHandleRequest_MainMenuNavigation(t *testing.T) {
	// Each main-menu option should route to a handler that returns a non-empty
	// USSD response (CON or END) without erroring.
	ctx := context.Background()
	for _, opt := range []string{"1", "2", "3", "4"} {
		user := &fakeUserSvc{user: map[string]any{"id": "u1"}, accounts: []any{map[string]any{}}}
		h := newHarness(t, user, &fakePINSvc{hasPIN: true})
		sess := "nav-" + opt
		if _, err := h.HandleRequest(ctx, sess, "254711000111", "*384#", "63902", ""); err != nil {
			t.Fatalf("opt %s dial-in: %v", opt, err)
		}
		resp, err := h.HandleRequest(ctx, sess, "254711000111", "*384#", "63902", opt)
		if err != nil {
			t.Errorf("opt %s: %v", opt, err)
			continue
		}
		if !strings.HasPrefix(resp, "CON ") && !strings.HasPrefix(resp, "END ") {
			t.Errorf("opt %s: expected CON/END response, got %q", opt, resp)
		}
	}
}

func TestHandleRequest_InvalidMainMenuOption(t *testing.T) {
	user := &fakeUserSvc{user: map[string]any{"id": "u1"}, accounts: []any{map[string]any{}}}
	h := newHarness(t, user, &fakePINSvc{hasPIN: true})
	ctx := context.Background()
	if _, err := h.HandleRequest(ctx, "bad", "254711000111", "*384#", "63902", ""); err != nil {
		t.Fatal(err)
	}
	// An out-of-range selection should not error; it re-prompts or errors gracefully.
	resp, err := h.HandleRequest(ctx, "bad", "254711000111", "*384#", "63902", "9")
	if err != nil {
		t.Fatalf("invalid option errored: %v", err)
	}
	if resp == "" {
		t.Error("invalid option produced an empty response")
	}
}

func TestHandleRequest_FullRegistrationFlow(t *testing.T) {
	user := &fakeUserSvc{getErr: errors.New("not found")} // unregistered
	h := newHarness(t, user, &fakePINSvc{})
	ctx := context.Background()
	const sess = "reg"

	// language_select -> register(name) -> national_id -> pin_create -> pin_confirm
	steps := []string{"", "1", "John Doe", "12345678", "2846", "2846"}
	var last string
	for i, in := range steps {
		resp, err := h.HandleRequest(ctx, sess, "254711000111", "*384#", "63902", in)
		if err != nil {
			t.Fatalf("step %d (input %q): %v", i, in, err)
		}
		last = resp
	}
	if !user.registered {
		t.Errorf("RegisterUser was never called; flow desynced. Final response: %q", last)
	}
}

func TestHandleRequest_SessionPersistsAcrossCalls(t *testing.T) {
	h := newHarness(t, &fakeUserSvc{getErr: errors.New("not found")}, &fakePINSvc{})
	ctx := context.Background()
	// First dial establishes the session (language menu).
	if _, err := h.HandleRequest(ctx, "sess-4", "254711000111", "*384#", "63902", ""); err != nil {
		t.Fatal(err)
	}
	// A follow-up input on the same session should be handled (not error out).
	if _, err := h.HandleRequest(ctx, "sess-4", "254711000111", "*384#", "63902", "1"); err != nil {
		t.Fatalf("second request errored: %v", err)
	}
}
