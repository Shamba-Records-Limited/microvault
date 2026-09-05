package darajastub

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
)

// C2B routes.
const (
	RouteC2BRegister Route = "c2b_register"
	RouteC2BSimulate Route = "c2b_simulate"
)

type registration struct {
	ResponseType    string
	ValidationURL   string
	ConfirmationURL string
}

var c2bSeq atomic.Int64

func (s *Stub) routeC2B() {
	s.handleAuthed(RouteC2BRegister, "/mpesa/c2b/v2/registerurl", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			ShortCode       string `json:"ShortCode"`
			ResponseType    string `json:"ResponseType"`
			ValidationURL   string `json:"ValidationURL"`
			ConfirmationURL string `json:"ConfirmationURL"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeAPIError(w, http.StatusBadRequest, "400.002.05", "Invalid Request Payload")
			return
		}

		shortcode, err := strconv.ParseUint(req.ShortCode, 10, 64)
		if err != nil || shortcode == 0 {
			writeAPIError(w, http.StatusBadRequest, "400.002.02", "Bad Request - Invalid ShortCode")
			return
		}
		if req.ResponseType != "Completed" && req.ResponseType != "Cancelled" {
			writeAPIError(w, http.StatusBadRequest, "400.002.02", "Bad Request - Invalid ResponseType")
			return
		}
		for _, url := range []string{req.ConfirmationURL, req.ValidationURL} {
			if url == "" {
				continue
			}
			if err := rejectBlockedURL(url); err != nil {
				writeAPIError(w, http.StatusBadRequest, "400.002.02", "Bad Request - Invalid URL")
				return
			}
		}

		s.mu.Lock()
		_, already := s.registrations[uint(shortcode)]
		if !already {
			s.registrations[uint(shortcode)] = registration{
				ResponseType:    req.ResponseType,
				ValidationURL:   req.ValidationURL,
				ConfirmationURL: req.ConfirmationURL,
			}
		}
		s.mu.Unlock()

		// Registration is effectively one-time: Safaricom must delete the
		// existing URLs before a shortcode can be registered again.
		if already {
			writeAPIError(w, http.StatusInternalServerError, "500.003.1001", "Urls are already registered.")
			return
		}

		writeJSON(w, http.StatusOK, map[string]string{
			"OriginatorCoversationID": fmt.Sprintf("stub-%d", c2bSeq.Add(1)),
			"ResponseCode":            "0",
			"ResponseDescription":     "Success. Request accepted for processing",
		})
	})

	s.handleAuthed(RouteC2BSimulate, "/mpesa/c2b/v2/simulate", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			ShortCode     string `json:"ShortCode"`
			CommandID     string `json:"CommandID"`
			Amount        string `json:"Amount"`
			Msisdn        string `json:"Msisdn"`
			BillRefNumber string `json:"BillRefNumber"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeAPIError(w, http.StatusBadRequest, "400.002.05", "Invalid Request Payload")
			return
		}
		shortcode, _ := strconv.ParseUint(req.ShortCode, 10, 64)
		amount, err := strconv.ParseInt(req.Amount, 10, 64)
		if err != nil || amount <= 0 {
			writeAPIError(w, http.StatusBadRequest, "400.002.02", "Bad Request - Invalid Amount")
			return
		}

		writeJSON(w, http.StatusOK, map[string]string{
			"OriginatorCoversationID": fmt.Sprintf("stub-%d", c2bSeq.Add(1)),
			"ConversationID":          fmt.Sprintf("AG_stub_%d", c2bSeq.Load()),
			"ResponseCode":            "0",
			"ResponseDescription":     "Accept the service request successfully.",
		})

		s.simulateC2B(r.Context(), uint(shortcode), req.Msisdn, req.BillRefNumber, amount*100)
	})
}

// RegisteredURLs reports the URLs bound to a shortcode.
func (s *Stub) RegisteredURLs(shortcode uint) (validation, confirmation string, ok bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	reg, found := s.registrations[shortcode]
	return reg.ValidationURL, reg.ConfirmationURL, found
}

// SimulateC2B drives a customer payment to a shortcode.
//
// The validation call is synchronous, as Daraja's is: it posts to the
// registered validation URL and waits for the answer before deciding whether
// the payment happens. Only the confirmation is queued.
func (s *Stub) SimulateC2B(shortcode uint, msisdn, billRef string, amountMinor int64) {
	s.simulateC2B(context.Background(), shortcode, msisdn, billRef, amountMinor)
}

func (s *Stub) simulateC2B(ctx context.Context, shortcode uint, msisdn, billRef string, amountMinor int64) {
	s.mu.Lock()
	reg, registered := s.registrations[shortcode]
	s.mu.Unlock()
	if !registered {
		s.t.Errorf("darajastub: shortcode %d has no registered URLs", shortcode)
		return
	}

	transID := fmt.Sprintf("RKL%07d", c2bSeq.Add(1))
	notification := map[string]string{
		"TransactionType":   "Pay Bill",
		"TransID":           transID,
		"TransTime":         time.Now().Format("20060102150405"),
		"TransAmount":       formatBalance(amountMinor),
		"BusinessShortCode": strconv.FormatUint(uint64(shortcode), 10),
		"BillRefNumber":     billRef,
		"InvoiceNumber":     "",
		"OrgAccountBalance": "",
		"ThirdPartyTransID": "",
		"MSISDN":            maskMSISDN(msisdn),
		"FirstName":         "NICHOLAS",
		"MiddleName":        "",
		"LastName":          "",
	}

	accepted, thirdParty := s.validate(ctx, reg, notification)
	if !accepted {
		return
	}

	s.ledger.credit(shortcode, AccountUtility, amountMinor)
	s.record(Transaction{
		ID: transID, Shortcode: shortcode, Account: AccountUtility,
		Minor: amountMinor, MSISDN: msisdn, Reference: billRef, Kind: "c2b",
	})

	confirmation := make(map[string]string, len(notification))
	for key, value := range notification {
		confirmation[key] = value
	}
	confirmation["OrgAccountBalance"] = formatBalance(s.ledger.get(shortcode, AccountUtility))
	confirmation["ThirdPartyTransID"] = thirdParty

	s.queue(RouteC2BSimulate, CallbackResult, reg.ConfirmationURL, confirmation)
}

// validate posts the validation request and applies the registered fallback
// when the endpoint cannot be reached or answers badly.
func (s *Stub) validate(ctx context.Context, reg registration, notification map[string]string) (accepted bool, thirdPartyTransID string) {
	if reg.ValidationURL == "" {
		return true, ""
	}

	encoded, err := json.Marshal(notification)
	if err != nil {
		s.t.Fatalf("darajastub: encode validation request: %v", err)
		return false, ""
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, reg.ValidationURL, bytes.NewReader(encoded))
	if err != nil {
		s.t.Fatalf("darajastub: build validation request: %v", err)
		return false, ""
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.deliverer.Do(req)
	if err != nil {
		return reg.ResponseType == "Completed", ""
	}
	defer resp.Body.Close()

	var answer struct {
		ResultCode        any    `json:"ResultCode"`
		ResultDesc        string `json:"ResultDesc"`
		ThirdPartyTransID string `json:"ThirdPartyTransID"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&answer); err != nil {
		return reg.ResponseType == "Completed", ""
	}

	// Anything other than zero is a rejection, whatever it is. Daraja treats
	// the code as opaque beyond that.
	return fmt.Sprintf("%v", answer.ResultCode) == "0", answer.ThirdPartyTransID
}

// maskMSISDN renders a number the way C2B v2 discloses it: "2547 ***** 126".
func maskMSISDN(msisdn string) string {
	digits := strings.TrimSpace(msisdn)
	if len(digits) < 7 {
		return digits
	}
	return digits[:4] + " ***** " + digits[len(digits)-3:]
}
