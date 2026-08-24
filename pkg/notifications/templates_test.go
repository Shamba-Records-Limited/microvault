package notifications

import (
	"reflect"
	"strings"
	"testing"

	"github.com/Shamba-Records-Limited/microvault/pkg/contracts"
)

// renderLoan renders every field of a loan set against the sentinel, keyed by
// field name.
func renderLoan(t *LoanTemplates) map[string]string {
	n := SentinelLoanNotification()
	v := reflect.ValueOf(t).Elem()
	out := make(map[string]string, v.NumField())
	for i := range v.NumField() {
		out[v.Type().Field(i).Name] = v.Field(i).Interface().(LoanMessage)(n)
	}
	return out
}

// renderAccount does the same for an account set.
func renderAccount(t *AccountTemplates) map[string]string {
	n := SentinelAccountNotification()
	v := reflect.ValueOf(t).Elem()
	out := make(map[string]string, v.NumField())
	for i := range v.NumField() {
		out[v.Type().Field(i).Name] = v.Field(i).Interface().(AccountMessage)(n)
	}
	return out
}

// Every template must encode as GSM-7. One accented rune outside the alphabet
// silently halves the segment budget for the whole message.
func TestTemplatesAreGSM7(t *testing.T) {
	check := func(lang, field, text string) {
		if _, bad, ok := GSM7Len(text); !ok {
			t.Errorf("%s %s: %q is outside GSM 03.38:\n%s", lang, field, bad, text)
		}
	}
	for lang, tmpl := range localizedLoanTemplates() {
		for field, text := range renderLoan(tmpl) {
			check(lang, field, text)
		}
	}
	for lang, tmpl := range localizedAccountTemplates() {
		for field, text := range renderAccount(tmpl) {
			check(lang, field, text)
		}
	}
}

// The repayment initiation SMS carries a short link the borrower must open to
// pay. Same constraint as the cash-pickup one: a link split across concatenated
// segments breaks both wrapping and auto-linkification in SMS inboxes.
func TestRepaymentInitiatedFitsOneSegment(t *testing.T) {
	n := SentinelLoanNotification()
	for lang, tmpl := range localizedLoanTemplates() {
		text := tmpl.RepaymentInitiated(n)
		septets, _, ok := GSM7Len(text)
		if !ok {
			t.Errorf("%s: RepaymentInitiated is not GSM-7", lang)
			continue
		}
		if got := Segments(septets); got != 1 {
			t.Errorf("%s: RepaymentInitiated needs %d segments (%d septets), want 1:\n%s",
				lang, got, septets, text)
		}
	}
}

// The cash-pickup initiation SMS must stay within a single GSM-7 segment so the
// short-link URL is never split across concatenated parts — splitting breaks
// both wrapping and auto-linkification in SMS inboxes.
func TestCashPickupInitiatedFitsOneSegment(t *testing.T) {
	n := SentinelLoanNotification()

	for lang, tmpl := range localizedLoanTemplates() {
		msg := tmpl.CashPickupInitiated(n)

		septets, bad, ok := GSM7Len(msg)
		if !ok {
			t.Errorf("%s: %q is outside GSM 03.38:\n%s", lang, bad, msg)
			continue
		}
		if Segments(septets) > 1 {
			t.Errorf("%s: message is %d septets, exceeds one GSM-7 segment (160):\n%s", lang, septets, msg)
		}

		// The URL must close the message on its own line: linkification is most
		// reliable when nothing follows the link, and Google Messages only
		// offers a preview when the link starts or ends the body.
		if !strings.HasSuffix(msg, "\n"+n.InteractiveURL) {
			t.Errorf("%s: URL does not end the message on its own line:\n%s", lang, msg)
		}
	}
}

// The platform ships copy for other people's products, so it must not name
// this one or quote a USSD code that only exists in one deployment.
func TestDefaultTemplatesCarryNoBrandOrDialString(t *testing.T) {
	banned := []string{"Shamba", "microvault.com", "*789", "*384", "#"}

	scan := func(lang, field, text string) {
		for _, token := range banned {
			if strings.Contains(text, token) {
				t.Errorf("%s %s: default copy contains %q, which belongs to the builder:\n%s",
					lang, field, token, text)
			}
		}
	}
	for lang, tmpl := range localizedLoanTemplates() {
		for field, text := range renderLoan(tmpl) {
			scan(lang, field, text)
		}
	}
	for lang, tmpl := range localizedAccountTemplates() {
		for field, text := range renderAccount(tmpl) {
			scan(lang, field, text)
		}
	}
}

func TestSegments(t *testing.T) {
	cases := []struct {
		septets int
		want    int
	}{
		{0, 0}, {1, 1}, {160, 1}, {161, 2}, {306, 2}, {307, 3},
	}
	for _, c := range cases {
		if got := Segments(c.septets); got != c.want {
			t.Errorf("Segments(%d) = %d, want %d", c.septets, got, c.want)
		}
	}
}

func TestGSM7Len_RejectsNonGSM(t *testing.T) {
	// "ç" is the cedilla that previously broke registration in French copy.
	if _, bad, ok := GSM7Len("Français"); ok || bad != 'ç' {
		t.Errorf("GSM7Len(\"Français\") = (_, %q, %v), want (_, 'ç', false)", bad, ok)
	}
	// The extension table costs two septets per rune.
	if n, _, ok := GSM7Len("[]"); !ok || n != 4 {
		t.Errorf("GSM7Len(\"[]\") = (%d, _, %v), want (4, _, true)", n, ok)
	}
}

func TestDaysUntilDue_NilDueDate(t *testing.T) {
	if got := DaysUntilDue(contracts.LoanNotification{}); got != 0 {
		t.Errorf("DaysUntilDue with no due date = %d, want 0", got)
	}
}
