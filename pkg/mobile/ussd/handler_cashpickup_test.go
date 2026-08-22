package ussd

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

type errRateSvc struct{}

func (errRateSvc) GetExchangeRate(context.Context, string) (float64, error) {
	return 0, errors.New("rate provider down")
}

// newRateHarness mirrors newHarness but lets a test supply its own RateService.
func newRateHarness(t *testing.T, rates RateService) *USSDHandler {
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
	return NewUSSDHandler(sm, reg, user, fakeLoanSvc{}, rates, &fakePINSvc{hasPIN: true}, nil, nil)
}

func payoutSession(amountCents int64) *Session {
	return &Session{
		SessionID:   "sess-cashpickup",
		PhoneNumber: "254711000111",
		UserID:      "u1",
		CurrentMenu: "payout_method",
		Language:    "en",
		Data: map[string]any{
			"loan_amount_local": amountCents,
			"local_currency":    "KES",
			"loan_duration":     30,
			"product_id":        "p1",
		},
	}
}

// At the fake rate of 130 KES/USD, MoneyGram's 15 USD floor is KES 1,950.
func TestHandlePayoutMethod_CashPickupBelowAnchorMinimum(t *testing.T) {
	h := newRateHarness(t, fakeRateSvc{})
	session := payoutSession(100000) // KES 1,000 ≈ 7.69 USD

	resp, err := h.handlePayoutMethod(context.Background(), session, "1")
	if err != nil {
		t.Fatalf("handlePayoutMethod: %v", err)
	}
	if !strings.HasPrefix(resp, "CON ") {
		t.Errorf("expected the payout menu to be re-rendered, got %q", resp)
	}
	if !strings.Contains(resp, "1950") {
		t.Errorf("expected the KES equivalent of the 15 USD floor, got %q", resp)
	}
	if strings.Contains(resp, "Cash Pickup") {
		t.Errorf("cash pickup must be dropped from the menu, not re-offered: %q", resp)
	}
	if !strings.Contains(resp, "2. Mobile Money") {
		t.Errorf("mobile money must remain selectable on key 2, got %q", resp)
	}
	if _, ok := session.Data["payout_method"]; ok {
		t.Errorf("payout_method must stay unset when the floor is not met, got %v", session.Data["payout_method"])
	}
	if session.CurrentMenu != "payout_method" {
		t.Errorf("expected to stay on payout_method, got %q", session.CurrentMenu)
	}
}

func TestHandlePayoutMethod_CashPickupAboveAnchorMinimum(t *testing.T) {
	h := newRateHarness(t, fakeRateSvc{})
	session := payoutSession(250000) // KES 2,500 ≈ 19.23 USD

	if _, err := h.handlePayoutMethod(context.Background(), session, "1"); err != nil {
		t.Fatalf("handlePayoutMethod: %v", err)
	}
	if session.Data["payout_method"] != "cash_pickup" {
		t.Errorf("expected cash_pickup to be accepted, got %v", session.Data["payout_method"])
	}
}

func TestHandlePayoutMethod_MobileMoneyExemptFromAnchorMinimum(t *testing.T) {
	h := newRateHarness(t, fakeRateSvc{})
	session := payoutSession(100000) // below MoneyGram's floor, irrelevant to mobile money

	if _, err := h.handlePayoutMethod(context.Background(), session, "2"); err != nil {
		t.Fatalf("handlePayoutMethod: %v", err)
	}
	if session.Data["payout_method"] != "mobile_money" {
		t.Errorf("expected mobile_money to be accepted, got %v", session.Data["payout_method"])
	}
}

func TestHandlePayoutMethod_CashPickupFailsOpenWithoutRate(t *testing.T) {
	h := newRateHarness(t, errRateSvc{})
	session := payoutSession(100000)

	if _, err := h.handlePayoutMethod(context.Background(), session, "1"); err != nil {
		t.Fatalf("handlePayoutMethod: %v", err)
	}
	if session.Data["payout_method"] != "cash_pickup" {
		t.Errorf("expected cash_pickup to proceed when no rate is available, got %v", session.Data["payout_method"])
	}
}
