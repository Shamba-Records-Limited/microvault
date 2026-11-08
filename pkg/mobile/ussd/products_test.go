package ussd

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/Shamba-Records-Limited/microvault/pkg/notifications"
)

type catalogLoanSvc struct {
	fakeLoanSvc
	products []LoanProductConfig
	err      error
}

func (c catalogLoanSvc) ProductsFor(context.Context, string) ([]LoanProductConfig, error) {
	return c.products, c.err
}

func catalogHarness(t *testing.T, products []LoanProductConfig, err error) *USSDHandler {
	t.Helper()
	h := newBorrowHarness(t)
	h.loanService = catalogLoanSvc{products: products, err: err}
	return h
}

var (
	salary  = LoanProductConfig{ProductID: "p-salary", Name: "Salary", MinAmountCents: 100000, MaxAmountCents: 500000, Currency: "KES", DurationDays: 30, RepaymentSchedule: "lump_sum"}
	harvest = LoanProductConfig{ProductID: "p-harvest", Name: "Harvest", MinAmountCents: 200000, MaxAmountCents: 800000, Currency: "KES", DurationDays: 60, RepaymentSchedule: "lump_sum"}
)

func sessionData(t *testing.T, h *USSDHandler, id string) map[string]any {
	t.Helper()
	s, err := h.sessionManager.GetOrCreateSession(context.Background(), id, pilotPhone, "*384#", "63902")
	if err != nil {
		t.Fatal(err)
	}
	return s.Data
}

func TestCatalog_NoOffersEndsTheSession(t *testing.T) {
	h := catalogHarness(t, nil, nil)
	resp := dial(t, h, "p0", "", "1")
	if !strings.HasPrefix(resp, "END ") || !strings.Contains(resp, "No loan offers") {
		t.Fatalf("want the no-offers screen, got %q", resp)
	}
}

func TestCatalog_OneProductSkipsTheChoice(t *testing.T) {
	h := catalogHarness(t, []LoanProductConfig{salary}, nil)
	resp := dial(t, h, "p1", "", "1")
	if !strings.Contains(resp, "min 1000, max 5000") {
		t.Fatalf("want the amount prompt with the product's limits, got %q", resp)
	}
	resp = dial(t, h, "p1", "6000")
	if !strings.Contains(resp, "5,000") && !strings.Contains(resp, "5000") {
		t.Fatalf("an amount above the capped max is refused, got %q", resp)
	}
}

func TestCatalog_ChoosingAProductUsesItsTerms(t *testing.T) {
	h := catalogHarness(t, []LoanProductConfig{salary, harvest}, nil)
	resp := dial(t, h, "p2", "", "1")
	for _, want := range []string{"Choose a loan", "1. Salary KES 1000-5000", "2. Harvest KES 2000-8000"} {
		if !strings.Contains(resp, want) {
			t.Fatalf("product list missing %q: %q", want, resp)
		}
	}
	resp = dial(t, h, "p2", "9")
	if !strings.Contains(resp, "Choose a loan") {
		t.Fatalf("an invalid choice re-shows the list, got %q", resp)
	}
	resp = dial(t, h, "p2", "2")
	if !strings.Contains(resp, "min 2000, max 8000") {
		t.Fatalf("want the chosen product's limits, got %q", resp)
	}
	dial(t, h, "p2", "7000")
	data := sessionData(t, h, "p2")
	if data["product_id"] != "p-harvest" {
		t.Errorf("product_id = %v, want p-harvest", data["product_id"])
	}
	if got := toInt(data["loan_duration"]); got != 60 {
		t.Errorf("loan_duration = %d, want the chosen product's 60", got)
	}
}

func TestCatalog_BackFromTheListGoesHome(t *testing.T) {
	h := catalogHarness(t, []LoanProductConfig{salary, harvest}, nil)
	dial(t, h, "p3", "", "1")
	resp := dial(t, h, "p3", navBackInput)
	if !strings.Contains(resp, "1.") || strings.Contains(resp, "Choose a loan") {
		t.Fatalf("back from the list should show the main menu, got %q", resp)
	}
	if _, ok := sessionData(t, h, "p3")[productChoicesKey]; ok {
		t.Error("the choices are dropped when leaving the list")
	}
}

func TestCatalog_ErrorShowsTheErrorScreen(t *testing.T) {
	h := catalogHarness(t, nil, errors.New("db down"))
	if resp := dial(t, h, "p4", "", "1"); !strings.HasPrefix(resp, "END ") {
		t.Fatalf("want an error screen, got %q", resp)
	}
}

func TestCatalog_ListIsCappedToOneScreen(t *testing.T) {
	many := make([]LoanProductConfig, 8)
	for i := range many {
		many[i] = salary
		many[i].ProductID = "p" + string(rune('a'+i))
	}
	h := catalogHarness(t, many, nil)
	resp := dial(t, h, "p5", "", "1")
	if strings.Contains(resp, "6. ") {
		t.Fatalf("at most %d products are listed: %q", maxProductChoices, resp)
	}
}

func TestCatalogStrings_AreGSM7(t *testing.T) {
	for _, key := range []string{"loan_choose_product", "loan_no_offers"} {
		for _, lang := range []string{"en", "sw", "fr"} {
			if _, bad, ok := notifications.GSM7Len(GetLocalizedMessage(lang, key)); !ok {
				t.Errorf("%s/%s carries non-GSM character %q", key, lang, bad)
			}
		}
	}
}
