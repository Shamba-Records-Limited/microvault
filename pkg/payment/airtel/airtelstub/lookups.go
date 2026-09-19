package airtelstub

import (
	"net/http"
	"strconv"
	"strings"
	"time"
)

func (s *Stub) routeLookups() {
	s.handleAuthed(RouteUserEnquiry, pathUsers, s.handleUserEnquiry)
	s.handleAuthed(RouteBalance, pathBalance, s.handleBalance)
	s.handleAuthed(RouteSummary, pathSummary, s.handleSummary)
}

// barred and pinless MSISDNs let a test reach the two states that make a
// payment fail before it is worth pushing a prompt for.
var (
	barredMSISDNs  = map[string]bool{"733000001": true}
	pinlessMSISDNs = map[string]bool{"733000002": true}
	unknownMSISDNs = map[string]bool{"733000003": true}
)

func (s *Stub) handleUserEnquiry(w http.ResponseWriter, r *http.Request) {
	if !requireLocaleHeaders(w, r) {
		return
	}

	msisdn := strings.TrimPrefix(r.URL.Path, pathUsers)
	if msisdn == "" {
		writeEnvelope(w, http.StatusBadRequest, failed("400", "ROUTER003", "Missing msisdn"))
		return
	}
	if strings.HasPrefix(msisdn, "254") {
		writeEnvelope(w, http.StatusBadRequest, failed("400", "ESB000036", "Invalid MSISDN: country code must not be sent"))
		return
	}
	if unknownMSISDNs[msisdn] {
		writeEnvelope(w, http.StatusNotFound, failed("404", codeKYCMissing, "User not found"))
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"data": map[string]any{
			"first_name":   "Stub",
			"last_name":    "Subscriber",
			"msisdn":       msisdn,
			"grade":        "SUBSCRIBER",
			"is_barred":    barredMSISDNs[msisdn],
			"is_pin_set":   !pinlessMSISDNs[msisdn],
			"registration": map[string]any{"status": "ACTIVE"},
		},
		"status": ok(codeKYCOK, "Success"),
	})
}

func (s *Stub) handleBalance(w http.ResponseWriter, r *http.Request) {
	if !requireLocaleHeaders(w, r) {
		return
	}

	s.mu.Lock()
	minor := s.balanceMinor
	s.mu.Unlock()

	writeJSON(w, http.StatusOK, map[string]any{
		"data": map[string]any{
			// Formatted with a thousands separator, as Airtel renders it. A
			// client that decodes this into a number fails here.
			"balance":        formatMinor(minor),
			"currency":       "KES",
			"account_status": "ACTIVE",
		},
		"status": ok(codeAccountOK, "Success"),
	})
}

func (s *Stub) handleSummary(w http.ResponseWriter, r *http.Request) {
	if strings.TrimSpace(r.Header.Get("X-Country")) == "" {
		writeEnvelope(w, http.StatusBadRequest, failed("400", "ROUTER003", "Mandatory request header missing: country"))
		return
	}

	query := r.URL.Query()
	from, to := epochParam(query.Get("from")), epochParam(query.Get("to"))

	s.mu.Lock()
	defer s.mu.Unlock()

	entries := make([]map[string]any, 0, len(s.order))
	for _, id := range s.order {
		txn := s.transactions[id]
		if txn == nil || txn.Status != StatusSuccess {
			continue
		}
		if !from.IsZero() && txn.SettledAt.Before(from) {
			continue
		}
		if !to.IsZero() && txn.SettledAt.After(to) {
			continue
		}

		entries = append(entries, map[string]any{
			"charges": map[string]any{"service": 0},
			"payee":   map[string]any{"currency": "KES", "msisdn": "", "name": "Merchant"},
			"payer":   map[string]any{"currency": "KES", "msisdn": txn.MSISDN, "name": "Stub Subscriber"},
			"service": map[string]any{"type": "MERCHPAY"},
			"transaction": map[string]any{
				"airtel_money_id":  txn.AirtelMoneyID,
				"amount":           formatMinor(txn.AmountKES * 100),
				"created_at":       txn.SettledAt.Format(time.RFC3339),
				"id":               txn.ID,
				"reference_number": txn.Reference,
				"status":           string(txn.Status),
			},
		})
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"data":   map[string]any{"count": len(entries), "transactions": entries},
		"status": ok(codeESBSuccess, "Success"),
	})
}

func epochParam(value string) time.Time {
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil || parsed <= 0 {
		return time.Time{}
	}
	return time.Unix(parsed, 0).UTC()
}

// formatMinor renders minor units the way Airtel does: grouped, two decimals.
func formatMinor(minor int64) string {
	whole := minor / 100
	cents := minor % 100
	if cents < 0 {
		cents = -cents
	}

	digits := strconv.FormatInt(whole, 10)
	negative := strings.HasPrefix(digits, "-")
	digits = strings.TrimPrefix(digits, "-")

	var grouped strings.Builder
	for i, r := range digits {
		if i > 0 && (len(digits)-i)%3 == 0 {
			grouped.WriteByte(',')
		}
		grouped.WriteRune(r)
	}

	out := grouped.String() + "." + strconv.FormatInt(cents, 10)
	if cents < 10 {
		out = grouped.String() + ".0" + strconv.FormatInt(cents, 10)
	}
	if negative {
		return "-" + out
	}
	return out
}
