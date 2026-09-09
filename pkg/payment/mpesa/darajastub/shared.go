package darajastub

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"
)

// Routes for the Initiator-bearing endpoints.
const (
	RouteTransactionStatus Route = "transaction_status"
	RouteAccountBalance    Route = "account_balance"
	RouteReversal          Route = "reversal"
	RouteDynamicQR         Route = "dynamic_qr"
	RouteValidateID        Route = "validate_id"
)

// initiatorRequest is the envelope every Initiator-bearing endpoint shares.
type initiatorRequest struct {
	Initiator              string `json:"Initiator"`
	SecurityCredential     string `json:"SecurityCredential"`
	CommandID              string `json:"CommandID"`
	PartyA                 string `json:"PartyA"`
	IdentifierType         string `json:"IdentifierType"`
	TransactionID          string `json:"TransactionID"`
	OriginalConversationID string `json:"OriginalConversationID"`
	Amount                 string `json:"Amount"`
	ReceiverParty          string `json:"ReceiverParty"`
	RecieverIdentifierType string `json:"RecieverIdentifierType"`
	Remarks                string `json:"Remarks"`
	Occasion               string `json:"Occasion"`
	ResultURL              string `json:"ResultURL"`
	QueueTimeOutURL        string `json:"QueueTimeOutURL"`
}

// decodeInitiator reads the shared envelope and enforces the checks Daraja
// applies before it accepts anything: a decryptable credential, present
// callbacks, and remarks within range.
func (s *Stub) decodeInitiator(w http.ResponseWriter, r *http.Request) (*initiatorRequest, bool) {
	var req initiatorRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIError(w, http.StatusBadRequest, "400.002.05", "Invalid Request Payload")
		return nil, false
	}
	if code, ok := s.checkSecurityCredential(req.SecurityCredential); !ok {
		writeAPIError(w, http.StatusInternalServerError, "500.001.1001",
			fmt.Sprintf("The initiator information is invalid. (%d)", code))
		return nil, false
	}
	if req.ResultURL == "" || req.QueueTimeOutURL == "" {
		writeAPIError(w, http.StatusBadRequest, "400.002.02", "Bad Request - Invalid ResultURL")
		return nil, false
	}
	if len(req.Remarks) < 2 || len(req.Remarks) > 100 {
		writeAPIError(w, http.StatusBadRequest, "400.002.02", "Bad Request - Invalid Remarks")
		return nil, false
	}
	return &req, true
}

// ack writes the synchronous acknowledgement and reports the identifiers the
// result will carry.
func ack(w http.ResponseWriter) (originator, conversation string) {
	next := time.Now().UnixNano()
	originator = fmt.Sprintf("%d-stub-1", next%100000)
	conversation = fmt.Sprintf("AG_stub_%d", next%1000000)

	writeJSON(w, http.StatusOK, map[string]string{
		"OriginatorConversationID": originator,
		"ConversationID":           conversation,
		"ResponseCode":             "0",
		"ResponseDescription":      "Accept the service request successfully.",
	})
	return originator, conversation
}

// resolve queues the outcome, honouring a queued TimeoutNext by delivering to
// the timeout URL instead. The two carry the same envelope, which is exactly
// why the caller must distinguish them by URL.
func (s *Stub) resolve(route Route, req *initiatorRequest, originator, conversation string, body map[string]any) {
	if s.takeTimeout(route) {
		s.queue(route, CallbackTimeout, req.QueueTimeOutURL, map[string]any{
			"Result": map[string]any{
				"ResultType":               0,
				"ResultCode":               1037,
				"ResultDesc":               "The request timed out before processing",
				"OriginatorConversationID": originator,
				"ConversationID":           conversation,
				"TransactionID":            "",
				"ReferenceData": map[string]any{
					"ReferenceItem": map[string]any{
						"Key":   "QueueTimeoutURL",
						"Value": "https://internalsandbox.safaricom.co.ke/mpesa/abresults/v1/submit",
					},
				},
			},
		})
		return
	}
	s.queue(route, CallbackResult, req.ResultURL, body)
}

func resultEnvelope(originator, conversation, transactionID string, code int64, desc string, params []map[string]any) map[string]any {
	result := map[string]any{
		"ResultType":               0,
		"ResultCode":               code,
		"ResultDesc":               desc,
		"OriginatorConversationID": originator,
		"ConversationID":           conversation,
		"TransactionID":            transactionID,
		"ReferenceData": map[string]any{
			"ReferenceItem": map[string]any{"Key": "Occasion", "Value": ""},
		},
	}
	if len(params) > 0 {
		result["ResultParameters"] = map[string]any{"ResultParameter": params}
	}
	return map[string]any{"Result": result}
}

func (s *Stub) routeShared() {
	s.handleAuthed(RouteTransactionStatus, "/mpesa/transactionstatus/v1/query", func(w http.ResponseWriter, r *http.Request) {
		req, ok := s.decodeInitiator(w, r)
		if !ok {
			return
		}
		originator, conversation := ack(w)

		found := s.findTransaction(req.TransactionID)
		code, desc := int64(0), "The service request is processed successfully."
		params := []map[string]any{
			{"Key": "ReceiptNo", "Value": req.TransactionID},
			{"Key": "TransactionStatus", "Value": "Completed"},
			{"Key": "FinalisedTime", "Value": time.Now().Format("20060102150405")},
		}
		if found == nil {
			code, desc = 1, "The transaction does not exist"
			params = nil
		} else {
			params = append(params, map[string]any{"Key": "Amount", "Value": formatBalance(found.Minor)})
		}
		s.resolve(RouteTransactionStatus, req, originator, conversation,
			resultEnvelope(originator, conversation, req.TransactionID, code, desc, params))
	})

	s.handleAuthed(RouteAccountBalance, "/mpesa/accountbalance/v1/query", func(w http.ResponseWriter, r *http.Request) {
		req, ok := s.decodeInitiator(w, r)
		if !ok {
			return
		}
		originator, conversation := ack(w)

		shortcode, _ := strconv.ParseUint(req.PartyA, 10, 64)
		encoded := fmt.Sprintf("Working Account|KES|%s|0.00|0.00|0.00&Utility Account|KES|%s|0.00|0.00|0.00&Charges Paid Account|KES|%s|0.00|0.00|0.00",
			formatBalance(s.ledger.get(uint(shortcode), AccountWorking)),
			formatBalance(s.ledger.get(uint(shortcode), AccountUtility)),
			formatBalance(s.ledger.get(uint(shortcode), AccountChargesPaid)))

		s.resolve(RouteAccountBalance, req, originator, conversation,
			resultEnvelope(originator, conversation, "OA90000000", 0, "The service request is processed successfully",
				[]map[string]any{
					{"Key": "AccountBalance", "Value": encoded},
					{"Key": "BOCompletedTime", "Value": time.Now().Format("20060102150405")},
				}))
	})

	s.handleAuthed(RouteReversal, "/mpesa/reversal/v1/request", func(w http.ResponseWriter, r *http.Request) {
		req, ok := s.decodeInitiator(w, r)
		if !ok {
			return
		}
		if req.RecieverIdentifierType != "11" {
			writeAPIError(w, http.StatusBadRequest, "400.002.02", "Bad Request - Invalid RecieverIdentifierType")
			return
		}
		originator, conversation := ack(w)

		shortcode, _ := strconv.ParseUint(req.ReceiverParty, 10, 64)
		amount, _ := strconv.ParseInt(req.Amount, 10, 64)
		minor := amount * 100

		code, desc := int64(0), "The service request is processed successfully."
		if !s.ledger.debit(uint(shortcode), AccountUtility, minor) {
			code, desc = 1, "The balance is insufficient for the transaction."
		}

		var params []map[string]any
		if code == 0 {
			params = []map[string]any{
				{"Key": "OriginalTransactionID", "Value": req.TransactionID},
				{"Key": "Amount", "Value": formatBalance(minor)},
				{"Key": "Charge", "Value": "0.00"},
				// Unmasked, with a full name: a reversal result discloses the
				// payer a C2B confirmation would not.
				{"Key": "CreditPartyPublicName", "Value": "254705912645 - NICHOLAS JOHN SONGOK"},
			}
		}
		s.resolve(RouteReversal, req, originator, conversation,
			resultEnvelope(originator, conversation, "SKE52PAWR9", code, desc, params))
	})

	s.handleAuthed(RouteDynamicQR, "/mpesa/qrcode/v1/generate", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			MerchantName string `json:"MerchantName"`
			TrxCode      string `json:"TrxCode"`
			CPI          string `json:"CPI"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeAPIError(w, http.StatusBadRequest, "400.002.05", "Invalid Request Payload")
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{
			"ResponseCode":        "00",
			"RequestID":           "stub-qr",
			"ResponseDescription": "QR Code Successfully Generated.",
			// A one-pixel PNG: enough to prove the value round-trips as base64
			// without pretending to be a real code.
			"QRCode": "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAAAAAA6fptVAAAACklEQVR4nGNiAAAABgADNjd8qAAAAABJRU5ErkJggg==",
		})
	})

	s.handleAuthed(RouteValidateID, "/v1/KYC-validation/validateID", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			RequestRefID string `json:"requestRefID"`
			MSISDN       string `json:"msisdn"`
			IDType       string `json:"idType"`
			IDNumber     string `json:"idNumber"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeAPIError(w, http.StatusBadRequest, "400.002.05", "Invalid Request Payload")
			return
		}

		s.mu.Lock()
		matched := s.identities[req.MSISDN] == req.IDNumber
		s.mu.Unlock()

		code, message, status := "4001", "Details do not match", "false"
		if matched {
			code, message, status = "4000", "Details match successfully", "true"
		}
		// status is the string "true"/"false", not a boolean.
		writeJSON(w, http.StatusOK, map[string]string{
			"responseRefID":   req.RequestRefID,
			"responseCode":    code,
			"responseMessage": message,
			"status":          status,
		})
	})
}

// BindIdentity records that an MSISDN is registered under an ID number, so
// ValidateMobileNumber has something to match against.
func (s *Stub) BindIdentity(msisdn, idNumber string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.identities[msisdn] = idNumber
}

func (s *Stub) findTransaction(id string) *Transaction {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.transactions {
		if s.transactions[i].ID == id {
			return &s.transactions[i]
		}
	}
	return nil
}
