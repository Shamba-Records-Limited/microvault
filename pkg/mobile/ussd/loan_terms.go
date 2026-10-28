package ussd

import (
	"math"
	"strconv"
	"strings"
	"time"
)

// LoanTermsText renders the pre-acceptance disclosure: the maximum APR, the
// most the borrower repays by the due date at that rate, and the late-payment
// tiers. The maximum assumes the rate stays at the cap for the whole term,
// compounded continuously, so the real figure is never higher.
func LoanTermsText(lang, currency string, amount float64, durationDays int, now time.Time, t LoanTerms) string {
	maxRepay := math.Ceil(amount * math.Exp(float64(t.MaxAPRBps)/10_000*float64(durationDays)/365))
	due := now.AddDate(0, 0, durationDays).Format("02/01")
	body := Format(lang, "loan_terms_rate", pct(t.MaxAPRBps), currency, maxRepay, due)
	if late := lateLine(lang, t); late != "" {
		body += "\n" + late
	}
	return body + "\n" + GetLocalizedMessage(lang, "loan_terms_accept")
}

func lateLine(lang string, t LoanTerms) string {
	n := len(t.PenaltyTierDays)
	if n < 2 || (t.LateFeeBps <= 0 && t.DefaultPenaltyBps <= 0) {
		return ""
	}
	days := make([]string, n-1)
	for i, d := range t.PenaltyTierDays[:n-1] {
		days[i] = strconv.Itoa(int(d))
	}
	return Format(lang, "loan_terms_late", pct(t.LateFeeBps), strings.Join(days, ", "),
		pct(t.DefaultPenaltyBps), int(t.PenaltyTierDays[n-1]))
}

// pct renders basis points as a percentage without trailing zeros.
func pct(bps int32) string {
	return strconv.FormatFloat(float64(bps)/100, 'f', -1, 64)
}
