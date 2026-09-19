package ussd

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// carrierLoanSvc adds the provider-pinned prompt surface to the repay fake,
// modelling a builder with both rails configured.
type carrierLoanSvc struct {
	promptingLoanSvc
	via      []string
	viaLoans []string
	viaErr   error
}

func (s *carrierLoanSvc) PromptRepaymentVia(_ context.Context, loanID, _, providerID string) error {
	if s.viaErr != nil {
		return s.viaErr
	}
	s.via = append(s.via, providerID)
	s.viaLoans = append(s.viaLoans, loanID)
	return nil
}

// newCarrierHarness enables both prompt rails.
func newCarrierHarness(t *testing.T, svc *carrierLoanSvc, paybill string) *USSDHandler {
	t.Helper()
	h := newPromptHarness(t, &svc.promptingLoanSvc, paybill)
	h.carrierPrompter = svc
	h.airtelPromptOn = true
	return h
}

func carrierSvc() *carrierLoanSvc {
	svc := &carrierLoanSvc{}
	svc.loans = []any{loanRow("l1", "LN-1", "disbursed")}
	svc.quotes = map[string]*RepaymentQuote{
		"l1": {AmountUSDCStroops: aboveFloor, AmountLocalCents: 645000, LocalCurrency: "KES"},
	}
	return svc
}

// The borrower picks their network. Both prompt rails are offered by name,
// so nothing is inferred from their MSISDN.
func TestRepayRails_OffersBothCarriersByName(t *testing.T) {
	svc := carrierSvc()
	h := newCarrierHarness(t, svc, "247247")

	rails := h.mobileRepayRails(&Session{Data: map[string]any{}})

	keys := make([]string, 0, len(rails))
	for _, r := range rails {
		keys = append(keys, r.key)
	}
	want := []string{"mpesa", "airtel", "paybill"}
	if len(keys) != len(want) {
		t.Fatalf("rails = %v, want %v", keys, want)
	}
	for i := range want {
		if keys[i] != want[i] {
			t.Fatalf("rails = %v, want %v", keys, want)
		}
	}
}

// A builder with only M-Pesa configured must not show an Airtel option the
// registry cannot resolve.
func TestRepayRails_WithholdsAirtelWhenUnconfigured(t *testing.T) {
	svc := &promptingLoanSvc{}
	svc.loans = []any{loanRow("l1", "LN-1", "disbursed")}
	h := newPromptHarness(t, svc, "247247")

	for _, rail := range h.mobileRepayRails(&Session{Data: map[string]any{}}) {
		if rail.key == "airtel" {
			t.Fatal("the airtel rail was offered with no carrier prompter wired")
		}
	}
}

// An open repayment on any rail empties the list: the open one must settle or
// lapse before another is offered.
func TestRepayRails_OpenRepaymentWithholdsBoth(t *testing.T) {
	svc := carrierSvc()
	h := newCarrierHarness(t, svc, "247247")

	session := &Session{Data: map[string]any{"repay_mpesa_status": "initiated"}}
	if rails := h.mobileRepayRails(session); len(rails) != 0 {
		t.Fatalf("rails = %v, want none while a repayment is open", rails)
	}
}

func TestStartAirtelRepayment_PinsTheProvider(t *testing.T) {
	svc := carrierSvc()
	h := newCarrierHarness(t, svc, "247247")
	session := repaySession()

	resp, err := h.startAirtelRepayment(context.Background(), session, repayLoanChoice{
		ID: "l1", Reference: "LN-1", DisplayAmount: "KES 6,450",
	})
	if err != nil {
		t.Fatalf("startAirtelRepayment: %v", err)
	}

	if len(svc.via) != 1 || svc.via[0] != "airtel" {
		t.Fatalf("pinned provider = %v, want [airtel]", svc.via)
	}
	if len(svc.viaLoans) != 1 || svc.viaLoans[0] != "l1" {
		t.Fatalf("prompted loans = %v", svc.viaLoans)
	}
	if len(svc.prompted) != 0 {
		t.Fatal("the airtel rail must not fall through to the unpinned M-Pesa prompt")
	}
	if !strings.Contains(resp, "Airtel Money") {
		t.Fatalf("response does not name the rail: %q", resp)
	}

	// The session records which rail is in flight, so a borrower coming
	// straight back is told about the right one.
	if session.Data["repay_provider"] != "airtel" {
		t.Fatalf("repay_provider = %v, want airtel", session.Data["repay_provider"])
	}
	if session.Data["repay_mpesa_status"] != "initiated" {
		t.Fatalf("repay status = %v, want initiated", session.Data["repay_mpesa_status"])
	}
}

// A refused push must not leave the session claiming a repayment is open, or
// the borrower is locked out of every rail until it lapses.
func TestStartAirtelRepayment_RefusalLeavesNothingInFlight(t *testing.T) {
	svc := carrierSvc()
	svc.viaErr = errors.New("airtel declined")
	h := newCarrierHarness(t, svc, "247247")
	session := repaySession()

	if _, err := h.startAirtelRepayment(context.Background(), session, repayLoanChoice{
		ID: "l1", Reference: "LN-1", DisplayAmount: "KES 6,450",
	}); err != nil {
		t.Fatalf("startAirtelRepayment: %v", err)
	}

	if _, present := session.Data["repay_provider"]; present {
		t.Fatal("a refused push marked the session as in flight")
	}
}

func TestStartAirtelRepayment_NoPrompterIsAnError(t *testing.T) {
	svc := carrierSvc()
	h := newCarrierHarness(t, svc, "247247")
	h.carrierPrompter = nil

	resp, err := h.startAirtelRepayment(context.Background(), repaySession(), repayLoanChoice{ID: "l1"})
	if err != nil {
		t.Fatalf("startAirtelRepayment: %v", err)
	}
	if strings.Contains(resp, "Airtel Money request") {
		t.Fatalf("an unwired rail reported success: %q", resp)
	}
}

// Every string the repay rails render must survive GSM-7, or the screen is
// mangled on a real handset.
func TestRepayRailStrings_AreGSM7(t *testing.T) {
	keys := []string{
		"repay_rail_mpesa", "repay_rail_airtel", "repay_mobile_paybill",
		"repay_mpesa_sent", "repay_airtel_sent", "repay_mpesa_inflight",
	}
	for _, lang := range []string{"en", "sw", "fr"} {
		for _, k := range keys {
			msg := GetLocalizedMessage(lang, k)
			if msg == "" {
				t.Errorf("%s %s: missing translation", lang, k)
				continue
			}
			for _, r := range msg {
				if r > 0x7F && !strings.ContainsRune("àäöñüèéùìòÇØøÅåÆæßÉÄÖÑÜ§¿¡£$¥¤…", r) {
					t.Errorf("%s %s: %q may not survive the USSD display:\n%s", lang, k, r, msg)
				}
			}
		}
	}
}
