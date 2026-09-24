package airtelstub

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
)

// queuedCallback is one notification waiting to be flushed.
type queuedCallback struct {
	url  string
	body map[string]any
}

// QueueCallback stages a callback for a transaction without sending it.
//
// Nothing is delivered on a timer. A test flushes with Deliver, so "the poller
// enquired before the callback arrived" is something a test states rather than
// races for.
func (s *Stub) QueueCallback(id string, status TransactionStatus) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.queueLocked(id, status)
}

func (s *Stub) queueLocked(id string, status TransactionStatus) {
	txn, ok := s.transactions[id]
	if !ok {
		s.t.Fatalf("airtelstub: callback for unknown transaction %s", id)
		return
	}
	if s.callbackURL == "" {
		s.t.Fatalf("airtelstub: no callback URL is set")
		return
	}

	transaction := map[string]any{
		"id":          txn.ID,
		"message":     "Transaction " + string(status),
		"status_code": string(status),
	}
	// Airtel's callback carries the receipt only when the transaction
	// succeeded, matching the enquiry.
	if status == StatusSuccess {
		transaction["airtel_money_id"] = txn.AirtelMoneyID
	}

	body := map[string]any{"transaction": transaction}
	if s.hmacKey != "" {
		body["hash"] = s.hashFor(body)
	}

	txn.callbacksAt++
	s.pending = append(s.pending, queuedCallback{url: s.callbackURL, body: body})
}

// DeliverIntermediateThenFinal queues two callbacks for one transaction: an
// in-progress report followed by its resolution.
//
// This is the behaviour with no M-Pesa equivalent. Airtel documents the
// callback as carrying intermediate or final status, with nothing in the
// payload to tell them apart, so a handler that treats the first as terminal
// closes a transaction that had not finished.
func (s *Stub) DeliverIntermediateThenFinal(id string, final TransactionStatus) {
	s.mu.Lock()
	s.queueLocked(id, StatusInProgress)
	s.mu.Unlock()

	s.Deliver()

	if final == StatusSuccess {
		s.Settle(id)
	}

	s.mu.Lock()
	s.queueLocked(id, final)
	s.mu.Unlock()

	s.Deliver()
}

// Deliver flushes every queued callback.
func (s *Stub) Deliver() {
	s.mu.Lock()
	queued := s.pending
	s.pending = nil
	s.mu.Unlock()

	for _, cb := range queued {
		encoded, err := json.Marshal(cb.body)
		if err != nil {
			s.t.Errorf("airtelstub: encode callback: %v", err)
			continue
		}

		req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, cb.url, bytes.NewReader(encoded))
		if err != nil {
			s.t.Errorf("airtelstub: build callback to %s: %v", cb.url, err)
			continue
		}
		req.Header.Set("Content-Type", "application/json")

		resp, err := s.deliverer.Do(req)
		if err != nil {
			s.t.Errorf("airtelstub: deliver callback to %s: %v", cb.url, err)
			continue
		}
		_ = resp.Body.Close()
	}
}

// Pending reports how many callbacks are queued but not delivered.
func (s *Stub) Pending() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.pending)
}

// hashFor computes the callback hash under the configured reading of "the
// body": the whole body without a hash member, or the transaction member
// alone. Airtel documents the mechanism without saying which, so both are
// reachable and a test can pin either.
func (s *Stub) hashFor(body map[string]any) string {
	payload := any(body)
	if s.hashOverTransaction {
		payload = body["transaction"]
	}

	encoded, err := json.Marshal(payload)
	if err != nil {
		s.t.Fatalf("airtelstub: encode callback for hashing: %v", err)
		return ""
	}

	mac := hmac.New(sha256.New, []byte(s.hmacKey))
	mac.Write(encoded)
	return base64.StdEncoding.EncodeToString(mac.Sum(nil))
}
