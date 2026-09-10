package darajastub

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
)

// Express routes.
const (
	RouteExpress      Route = "express"
	RouteExpressQuery Route = "express_query"
)

// WithPasskey sets the passkey the stub expects the Express password to be
// derived from.
func WithPasskey(passkey string) Option {
	return func(s *Stub) { s.passkey = passkey }
}

// Checkout is one in-flight M-Pesa Express prompt.
type Checkout struct {
	MerchantRequestID string
	CheckoutRequestID string
	Shortcode         uint
	AmountMinor       int64
	Payer             string
	Reference         string
	CallbackURL       string
	ResultCode        int64
	Resolved          bool
	Receipt           string
}

var checkoutSeq atomic.Int64

type expressRequest struct {
	BusinessShortCode string `json:"BusinessShortCode"`
	Password          string `json:"Password"`
	Timestamp         string `json:"Timestamp"`
	TransactionType   string `json:"TransactionType"`
	Amount            string `json:"Amount"`
	PartyA            string `json:"PartyA"`
	PartyB            string `json:"PartyB"`
	PhoneNumber       string `json:"PhoneNumber"`
	CallBackURL       string `json:"CallBackURL"`
	AccountReference  string `json:"AccountReference"`
	TransactionDesc   string `json:"TransactionDesc"`
}

func (s *Stub) routeExpress() {
	s.handleAuthed(RouteExpress, "/mpesa/stkpush/v1/processrequest", func(w http.ResponseWriter, r *http.Request) {
		var req expressRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeAPIError(w, http.StatusBadRequest, "400.002.05", "Invalid Request Payload")
			return
		}

		shortcode, err := strconv.ParseUint(req.BusinessShortCode, 10, 64)
		if err != nil || shortcode == 0 {
			writeAPIError(w, http.StatusBadRequest, "400.002.02", "Bad Request - Invalid BusinessShortCode")
			return
		}
		if !s.checkExpressPassword(uint(shortcode), req.Timestamp, req.Password) {
			writeAPIError(w, http.StatusInternalServerError, "500.001.1001", "Wrong credentials")
			return
		}

		// Field limits Safaricom documents, enforced so the client's local
		// validation is checked against a second reading of the spec.
		if len(req.AccountReference) > 12 {
			writeAPIError(w, http.StatusBadRequest, "400.002.02", "Bad Request - Invalid AccountReference")
			return
		}
		if len(req.TransactionDesc) > 13 {
			writeAPIError(w, http.StatusBadRequest, "400.002.02", "Bad Request - Invalid TransactionDesc")
			return
		}
		if len(req.PhoneNumber) != 12 || !strings.HasPrefix(req.PhoneNumber, "254") {
			writeAPIError(w, http.StatusBadRequest, "400.002.02", "Bad Request - Invalid PhoneNumber")
			return
		}
		amount, err := strconv.ParseInt(req.Amount, 10, 64)
		if err != nil || amount <= 0 {
			writeAPIError(w, http.StatusBadRequest, "400.002.02", "Bad Request - Invalid Amount")
			return
		}
		if err := rejectBlockedURL(req.CallBackURL); err != nil {
			writeAPIError(w, http.StatusBadRequest, "400.002.02", "Bad Request - Invalid CallBackURL")
			return
		}

		next := checkoutSeq.Add(1)
		checkout := &Checkout{
			MerchantRequestID: fmt.Sprintf("%d-stub-%d", next, next),
			CheckoutRequestID: fmt.Sprintf("ws_CO_stub%06d", next),
			Shortcode:         uint(shortcode),
			AmountMinor:       amount * 100,
			Payer:             req.PhoneNumber,
			Reference:         req.AccountReference,
			CallbackURL:       req.CallBackURL,
		}

		s.mu.Lock()
		s.checkouts[checkout.CheckoutRequestID] = checkout
		s.mu.Unlock()

		writeJSON(w, http.StatusOK, map[string]string{
			"MerchantRequestID":   checkout.MerchantRequestID,
			"CheckoutRequestID":   checkout.CheckoutRequestID,
			"ResponseCode":        "0",
			"ResponseDescription": "Success. Request accepted for processing",
			"CustomerMessage":     "Success. Request accepted for processing",
		})
	})

	s.handleAuthed(RouteExpressQuery, "/mpesa/stkpushquery/v1/query", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			CheckoutRequestID string `json:"CheckoutRequestID"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)

		s.mu.Lock()
		checkout, ok := s.checkouts[req.CheckoutRequestID]
		s.mu.Unlock()
		if !ok {
			writeAPIError(w, http.StatusBadRequest, "400.002.02", "Bad Request - Invalid CheckoutRequestID")
			return
		}

		// Daraja is widely reported to error rather than report "pending"
		// while a prompt is still on the handset. Modelling that is the point:
		// a poller treating the error as terminal loses the payment.
		if !checkout.Resolved {
			writeAPIError(w, http.StatusInternalServerError, "500.001.1001", "The transaction is being processed")
			return
		}

		writeJSON(w, http.StatusOK, map[string]any{
			"ResponseCode":        "0",
			"ResponseDescription": "The service request has been accepted successfully",
			"MerchantRequestID":   checkout.MerchantRequestID,
			"CheckoutRequestID":   checkout.CheckoutRequestID,
			"ResultCode":          strconv.FormatInt(checkout.ResultCode, 10),
			"ResultDesc":          expressResultDesc(checkout.ResultCode),
		})
	})
}

func (s *Stub) checkExpressPassword(shortcode uint, timestamp, presented string) bool {
	s.mu.Lock()
	passkey := s.passkey
	s.mu.Unlock()

	expected := base64.StdEncoding.EncodeToString(
		[]byte(strconv.FormatUint(uint64(shortcode), 10) + passkey + timestamp))
	return presented == expected
}

// CompleteSTK resolves a prompt as paid, crediting the shortcode's Utility
// account and queueing the success callback.
func (s *Stub) CompleteSTK(checkoutRequestID string) { s.resolveSTK(checkoutRequestID, 0) }

// CancelSTK resolves a prompt as cancelled by the customer.
func (s *Stub) CancelSTK(checkoutRequestID string) { s.resolveSTK(checkoutRequestID, 1032) }

// TimeoutSTK resolves a prompt as unreachable.
func (s *Stub) TimeoutSTK(checkoutRequestID string) { s.resolveSTK(checkoutRequestID, 1037) }

// ResolveSTK resolves a prompt with any documented result code.
func (s *Stub) ResolveSTK(checkoutRequestID string, resultCode int64) {
	s.resolveSTK(checkoutRequestID, resultCode)
}

func (s *Stub) resolveSTK(checkoutRequestID string, resultCode int64) {
	s.mu.Lock()
	checkout, ok := s.checkouts[checkoutRequestID]
	if !ok {
		s.mu.Unlock()
		s.t.Errorf("darajastub: no such checkout %q", checkoutRequestID)
		return
	}
	checkout.Resolved = true
	checkout.ResultCode = resultCode
	if resultCode == 0 {
		checkout.Receipt = fmt.Sprintf("STB%07d", checkoutSeq.Add(1))
	}
	copied := *checkout
	s.mu.Unlock()

	if resultCode == 0 {
		s.ledger.credit(copied.Shortcode, AccountUtility, copied.AmountMinor)
		s.record(Transaction{
			ID: copied.Receipt, Shortcode: copied.Shortcode, Account: AccountUtility,
			Minor: copied.AmountMinor, MSISDN: copied.Payer, Reference: copied.Reference,
			Kind: "express",
		})
	}
	s.queue(RouteExpress, CallbackResult, copied.CallbackURL, expressCallbackBody(&copied))
}

func expressCallbackBody(checkout *Checkout) map[string]any {
	inner := map[string]any{
		"MerchantRequestID": checkout.MerchantRequestID,
		"CheckoutRequestID": checkout.CheckoutRequestID,
		"ResultCode":        checkout.ResultCode,
		"ResultDesc":        expressResultDesc(checkout.ResultCode),
	}

	// CallbackMetadata is absent entirely on failure, which is the shape that
	// breaks a decoder assuming it is always present.
	if checkout.ResultCode == 0 {
		payer, _ := strconv.ParseInt(checkout.Payer, 10, 64)
		inner["CallbackMetadata"] = map[string]any{
			"Item": []map[string]any{
				{"Name": "Amount", "Value": float64(checkout.AmountMinor) / 100},
				{"Name": "MpesaReceiptNumber", "Value": checkout.Receipt},
				{"Name": "TransactionDate", "Value": time.Now().Format("20060102150405")},
				{"Name": "PhoneNumber", "Value": payer},
			},
		}
	}
	return map[string]any{"Body": map[string]any{"stkCallback": inner}}
}

func expressResultDesc(resultCode int64) string {
	switch resultCode {
	case 0:
		return "The service request is processed successfully."
	case 1:
		return "The balance is insufficient for the transaction."
	case 1032:
		return "Request cancelled by user."
	case 1037:
		return "DS timeout user cannot be reached."
	}
	return "The service request failed."
}

// Checkouts returns every prompt the stub has seen.
func (s *Stub) Checkouts() []Checkout {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Checkout, 0, len(s.checkouts))
	for _, checkout := range s.checkouts {
		out = append(out, *checkout)
	}
	return out
}

// rejectBlockedURL mirrors Daraja's refusal of URLs containing certain words,
// so the client's own assertion is checked against a second implementation.
func rejectBlockedURL(value string) error {
	lower := strings.ToLower(value)
	for _, word := range []string{"mpesa", "safaricom", "exe", "exec", "cmd", "sql", "query"} {
		if strings.Contains(lower, word) {
			return fmt.Errorf("darajastub: blocked word %q", word)
		}
	}
	return nil
}
