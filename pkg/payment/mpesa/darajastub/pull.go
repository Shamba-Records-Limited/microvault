package darajastub

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"
)

// Pull routes.
const (
	RoutePullRegister Route = "pull_register"
	RoutePullQuery    Route = "pull_query"
)

func (s *Stub) routePull() {
	s.handleAuthed(RoutePullRegister, "/pulltransactions/v1/register", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			ShortCode   string `json:"ShortCode"`
			RequestType string `json:"RequestType"`
			CallBackURL string `json:"CallBackURL"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeAPIError(w, http.StatusBadRequest, "400.002.05", "Invalid Request Payload")
			return
		}
		shortcode, err := strconv.ParseUint(req.ShortCode, 10, 64)
		if err != nil || shortcode == 0 {
			writeAPIError(w, http.StatusBadRequest, "400.001", "Invalid ShortCode")
			return
		}

		s.mu.Lock()
		already := s.pullRegistered[uint(shortcode)]
		s.pullRegistered[uint(shortcode)] = true
		s.mu.Unlock()

		// 1001 means the shortcode was already bound, which is a success for
		// anyone whose goal is "be registered".
		status, description := "1000", "Shortcode Registered Successfully"
		if already {
			status, description = "1001", "ShortCode already Registered"
		}
		writeJSON(w, http.StatusOK, map[string]string{
			"ResponseRefID":       "stub-pull-ref",
			"ResponseStatus":      status,
			"ShortCode":           req.ShortCode,
			"ResponseDescription": description,
		})
	})

	s.handleAuthed(RoutePullQuery, "/pulltransactions/v1/query", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			ShortCode   string `json:"ShortCode"`
			StartDate   string `json:"StartDate"`
			EndDate     string `json:"EndDate"`
			OffSetValue string `json:"OffSetValue"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeAPIError(w, http.StatusBadRequest, "400.002.05", "Invalid Request Payload")
			return
		}
		shortcode, _ := strconv.ParseUint(req.ShortCode, 10, 64)
		offset, _ := strconv.Atoi(req.OffSetValue)

		s.mu.Lock()
		stuck := s.pullStuckOffset
		s.mu.Unlock()
		if stuck {
			offset = 0
		}

		s.mu.Lock()
		var matching []Transaction
		for _, tx := range s.transactions {
			if tx.Shortcode == uint(shortcode) && tx.Account == AccountUtility {
				matching = append(matching, tx)
			}
		}
		pageSize := s.pullPageSize
		s.mu.Unlock()

		if offset >= len(matching) {
			// An empty window is 1001 with the string "[[]]" rather than an
			// empty array. It is a success: no payments happened.
			writeJSON(w, http.StatusOK, map[string]any{
				"ResponseRefID":   "stub-pull-ref",
				"ResponseCode":    "1001",
				"ResponseMessage": "No transactions available for the selected time period",
				"Response":        "[[]]",
			})
			return
		}

		end := min(offset+pageSize, len(matching))
		page := make([]map[string]any, 0, end-offset)
		for _, tx := range matching[offset:end] {
			page = append(page, map[string]any{
				"transactionId": tx.ID,
				"trxDate":       time.Now().UTC().Format(time.RFC3339),
				// Unmasked, unlike the C2B confirmation. This is the only route
				// to the payer's real number.
				"msisdn":           tx.MSISDN,
				"transactiontype":  "c2b-pay-bill-debit",
				"billreference":    tx.Reference,
				"amount":           formatBalance(tx.Minor),
				"organizationname": "Microvault Stub",
			})
		}

		writeJSON(w, http.StatusOK, map[string]any{
			"ResponseRefID":   "stub-pull-ref",
			"ResponseCode":    "1000",
			"ResponseMessage": "Success",
			"Response":        []any{page},
		})
	})
}

// WithPullPageSize sets how many transactions a pull query returns per page.
func WithPullPageSize(size int) Option {
	return func(s *Stub) { s.pullPageSize = size }
}

// WithPullStuckOffset makes the query ignore OffSetValue and return the first
// page forever. A pagination walk that trusts Daraja to advance would loop here
// rather than terminate, which is the failure this models.
func WithPullStuckOffset() Option {
	return func(s *Stub) { s.pullStuckOffset = true }
}
