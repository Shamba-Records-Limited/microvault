package airtelstub

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/samber/lo"
)

// Transaction is one collection the stub holds.
type Transaction struct {
	// ID is the partner's own id, the key an enquiry takes.
	ID string

	// AirtelMoneyID is minted only when Status is TS. It is the key a refund
	// takes, which is why a test can only refund what it first settled.
	AirtelMoneyID string

	MSISDN      string
	Reference   string
	AmountKES   int64
	Status      TransactionStatus
	Code        string
	Refunded    bool
	Signed      bool
	CreatedAt   time.Time
	SettledAt   time.Time
	callbacksAt int
}

type paymentRequest struct {
	Reference  string `json:"reference"`
	Subscriber struct {
		MSISDN string `json:"msisdn"`
	} `json:"subscriber"`
	Transaction struct {
		Amount int64  `json:"amount"`
		ID     string `json:"id"`
	} `json:"transaction"`
}

type refundRequest struct {
	Transaction struct {
		AirtelMoneyID string `json:"airtel_money_id"`
	} `json:"transaction"`
}

func (s *Stub) routeCollection() {
	s.handleAuthed(RoutePayment, pathPayment, s.handlePayment)
	s.handleAuthed(RouteRefund, pathRefund, s.handleRefund)
	s.handleAuthed(RouteEnquiry, pathPayments, s.handleEnquiry)
}

func (s *Stub) handlePayment(w http.ResponseWriter, r *http.Request) {
	if !requireLocaleHeaders(w, r) {
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeEnvelope(w, http.StatusBadRequest, failed("400", "ROUTER003", "Unreadable body"))
		return
	}

	signed, err := s.verifySignature(r, body)
	if err != nil {
		writeEnvelope(w, http.StatusForbidden, failed("403", codeForbidden, "Forbidden"))
		return
	}

	var req paymentRequest
	if err := json.Unmarshal(body, &req); err != nil {
		writeEnvelope(w, http.StatusBadRequest, failed("400", "ROUTER003", "Malformed payment"))
		return
	}
	if req.Transaction.ID == "" {
		writeEnvelope(w, http.StatusBadRequest, failed("400", "ROUTER003", "Missing transaction id"))
		return
	}
	if strings.HasPrefix(req.Subscriber.MSISDN, "254") {
		writeEnvelope(w, http.StatusBadRequest, failed("400", "ESB000036", "Invalid MSISDN: country code must not be sent"))
		return
	}

	s.mu.Lock()
	if _, exists := s.transactions[req.Transaction.ID]; exists {
		s.mu.Unlock()
		writeEnvelope(w, http.StatusBadRequest, failed("400", "ESB000041", "Transaction with this external id already exists"))
		return
	}
	s.mu.Unlock()

	resolved := s.takeOutcome()
	status := TransactionStatus(resolved.status)

	txn := &Transaction{
		ID:        req.Transaction.ID,
		MSISDN:    req.Subscriber.MSISDN,
		Reference: req.Reference,
		AmountKES: req.Transaction.Amount,
		Status:    status,
		Code:      resolved.code,
		Signed:    signed,
		CreatedAt: time.Now().UTC(),
	}
	if status == StatusSuccess {
		txn.AirtelMoneyID = fmt.Sprintf("AM%09d", len(s.order)+1)
		txn.SettledAt = txn.CreatedAt
	}

	s.mu.Lock()
	s.transactions[txn.ID] = txn
	s.order = append(s.order, txn.ID)
	s.mu.Unlock()

	writeJSON(w, http.StatusOK, map[string]any{
		"data": map[string]any{
			"transaction": map[string]any{
				"id":     txn.ID,
				"status": string(txn.Status),
			},
		},
		"status": statusFor(resolved.code),
	})
}

func (s *Stub) handleEnquiry(w http.ResponseWriter, r *http.Request) {
	if !requireLocaleHeaders(w, r) {
		return
	}

	id := strings.TrimPrefix(r.URL.Path, pathPayments)
	if id == "" {
		writeEnvelope(w, http.StatusBadRequest, failed("400", "ROUTER003", "Missing transaction id"))
		return
	}

	s.mu.Lock()
	txn, ok := s.transactions[id]
	s.mu.Unlock()
	if !ok {
		writeEnvelope(w, http.StatusNotFound, failed("404", codeNotFound, "Transaction not found"))
		return
	}

	transaction := map[string]any{
		"id":      txn.ID,
		"message": "Transaction " + string(txn.Status),
		"status":  string(txn.Status),
	}
	// The receipt is disclosed only on success. Adding it unconditionally
	// would let a consumer that reads it on TIP or TF pass here and fail in
	// production, which is the whole point of holding it back.
	if txn.Status == StatusSuccess {
		transaction["airtel_money_id"] = txn.AirtelMoneyID
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"data":   map[string]any{"transaction": transaction},
		"status": statusFor(txn.Code),
	})
}

func (s *Stub) handleRefund(w http.ResponseWriter, r *http.Request) {
	if !requireLocaleHeaders(w, r) {
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeEnvelope(w, http.StatusBadRequest, failed("400", "ROUTER003", "Unreadable body"))
		return
	}
	if _, err := s.verifySignature(r, body); err != nil {
		writeEnvelope(w, http.StatusForbidden, failed("403", codeForbidden, "Forbidden"))
		return
	}

	var req refundRequest
	if err := json.Unmarshal(body, &req); err != nil {
		writeEnvelope(w, http.StatusBadRequest, failed("400", "ROUTER003", "Malformed refund"))
		return
	}
	if req.Transaction.AirtelMoneyID == "" {
		writeEnvelope(w, http.StatusBadRequest, failed("400", "ROUTER003", "Missing airtel money id"))
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	for _, txn := range s.transactions {
		if txn.AirtelMoneyID != req.Transaction.AirtelMoneyID {
			continue
		}
		txn.Refunded = true
		writeJSON(w, http.StatusOK, map[string]any{
			"data": map[string]any{
				"transaction": map[string]any{
					"airtel_money_id": txn.AirtelMoneyID,
					"status":          string(StatusSuccess),
				},
			},
			"status": ok(codeSuccess, "Refund successful"),
		})
		return
	}
	writeEnvelope(w, http.StatusNotFound, failed("404", codeNotFound, "Transaction not found"))
}

// statusFor renders the envelope for a resolution code.
func statusFor(code string) envelope {
	switch code {
	case codeSuccess:
		return ok(code, "Transaction successful")
	case codeAmbiguous, codeInProcess:
		return envelope{
			Code:         "200",
			Message:      "Transaction in progress",
			Success:      false,
			ResponseCode: code,
		}
	default:
		return failed("200", code, "Transaction failed")
	}
}

// verifySignature checks the x-key and x-signature pair.
//
// It decrypts x-key with the stub's private half, re-encrypts the body it
// actually received, and compares. A client that signs a different rendering
// of the payload than the one it sends therefore fails, which an inspection
// of header presence would not catch.
func (s *Stub) verifySignature(r *http.Request, body []byte) (bool, error) {
	sealed := r.Header.Get("x-key")
	signature := r.Header.Get("x-signature")

	if sealed == "" && signature == "" {
		if s.requireSigning {
			return false, fmt.Errorf("airtelstub: signing is required and no signature was sent")
		}
		return false, nil
	}
	if sealed == "" || signature == "" {
		return false, fmt.Errorf("airtelstub: only one of x-key and x-signature was sent")
	}

	rawKey, err := base64.StdEncoding.DecodeString(sealed)
	if err != nil {
		return false, fmt.Errorf("airtelstub: x-key is not base64: %w", err)
	}
	//nolint:staticcheck // SA1019: mirrors the client's PKCS #1 v1.5 seal, which is what Airtel specifies.
	pair, err := rsa.DecryptPKCS1v15(nil, s.privateKey, rawKey)
	if err != nil {
		return false, fmt.Errorf("airtelstub: x-key does not decrypt: %w", err)
	}

	parts := strings.SplitN(string(pair), ":", 2)
	if len(parts) != 2 {
		return false, fmt.Errorf("airtelstub: x-key is not a key:iv pair")
	}
	key, err := base64.StdEncoding.DecodeString(parts[0])
	if err != nil {
		return false, fmt.Errorf("airtelstub: aes key is not base64: %w", err)
	}
	iv, err := base64.StdEncoding.DecodeString(parts[1])
	if err != nil {
		return false, fmt.Errorf("airtelstub: iv is not base64: %w", err)
	}

	expected, err := encryptCBC(body, key, iv)
	if err != nil {
		return false, err
	}
	received, err := base64.StdEncoding.DecodeString(signature)
	if err != nil {
		return false, fmt.Errorf("airtelstub: x-signature is not base64: %w", err)
	}
	if !bytes.Equal(expected, received) {
		return false, fmt.Errorf("airtelstub: x-signature does not match the payload")
	}
	return true, nil
}

func encryptCBC(plaintext, key, iv []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("airtelstub: aes cipher: %w", err)
	}
	if len(iv) != block.BlockSize() {
		return nil, fmt.Errorf("airtelstub: iv is not one block")
	}

	padding := block.BlockSize() - len(plaintext)%block.BlockSize()
	padded := append(append([]byte{}, plaintext...), bytes.Repeat([]byte{byte(padding)}, padding)...)

	out := make([]byte, len(padded))
	cipher.NewCBCEncrypter(block, iv).CryptBlocks(out, padded)
	return out, nil
}

// Transactions returns every transaction the stub holds, in arrival order.
func (s *Stub) Transactions() []Transaction {
	s.mu.Lock()
	defer s.mu.Unlock()

	return lo.FilterMap(s.order, func(id string, _ int) (Transaction, bool) {
		txn, ok := s.transactions[id]
		if !ok {
			return Transaction{}, false
		}
		return *txn, true
	})
}

// Transaction returns one transaction by partner id.
func (s *Stub) Transaction(id string) (Transaction, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	txn, ok := s.transactions[id]
	if !ok {
		return Transaction{}, false
	}
	return *txn, true
}

// Settle resolves an in-flight transaction, minting the receipt. It models
// the payer finally entering their PIN.
func (s *Stub) Settle(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	txn, ok := s.transactions[id]
	if !ok {
		s.t.Fatalf("airtelstub: settle unknown transaction %s", id)
		return
	}
	txn.Status = StatusSuccess
	txn.Code = codeSuccess
	txn.SettledAt = time.Now().UTC()
	if txn.AirtelMoneyID == "" {
		txn.AirtelMoneyID = fmt.Sprintf("AM%09d", len(s.order))
	}
}
