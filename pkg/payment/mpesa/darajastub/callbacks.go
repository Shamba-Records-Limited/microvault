package darajastub

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
)

// CallbackKind distinguishes the two URLs Daraja is given for an asynchronous
// call. They cannot be told apart by their payload — a timeout body names
// Safaricom's own internal listener rather than the caller's — so the
// distinction has to be which URL received the post.
type CallbackKind string

// The two deliveries.
const (
	CallbackResult  CallbackKind = "result"
	CallbackTimeout CallbackKind = "timeout"
)

type queuedCallback struct {
	route Route
	kind  CallbackKind
	url   string
	body  []byte
}

// Delivery records one callback the stub posted.
type Delivery struct {
	Route  Route
	Kind   CallbackKind
	URL    string
	Body   []byte
	Status int
}

// queue holds a callback until a test flushes it. Nothing is ever delivered on
// a timer, so tests need no sleeps and callback ordering is something a test
// states rather than races for.
func (s *Stub) queue(route Route, kind CallbackKind, url string, body any) {
	encoded, err := json.Marshal(body)
	if err != nil {
		s.t.Fatalf("darajastub: encode %s callback: %v", route, err)
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pending = append(s.pending, queuedCallback{route: route, kind: kind, url: url, body: encoded})
}

// Pending reports how many callbacks are waiting.
func (s *Stub) Pending() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.pending)
}

// Deliver flushes every queued callback, in order, and returns what was sent.
func (s *Stub) Deliver() []Delivery {
	var delivered []Delivery
	for s.Pending() > 0 {
		delivered = append(delivered, s.DeliverNext()...)
	}
	return delivered
}

// DeliverNext flushes exactly one queued callback, so a test can interleave a
// status query with an in-flight notification. Returns two deliveries when
// DuplicateCallbacks is set.
func (s *Stub) DeliverNext() []Delivery {
	s.mu.Lock()
	if len(s.pending) == 0 {
		s.mu.Unlock()
		return nil
	}
	next := s.pending[0]
	s.pending = s.pending[1:]
	repeat := s.duplicate
	s.mu.Unlock()

	deliveries := []Delivery{s.post(next)}
	if repeat {
		deliveries = append(deliveries, s.post(next))
	}
	return deliveries
}

func (s *Stub) post(cb queuedCallback) Delivery {
	delivery := Delivery{Route: cb.route, Kind: cb.kind, URL: cb.url, Body: cb.body}
	if cb.url == "" {
		return delivery
	}

	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, cb.url, bytes.NewReader(cb.body))
	if err != nil {
		s.t.Errorf("darajastub: build %s callback to %s: %v", cb.route, cb.url, err)
		return delivery
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.deliverer.Do(req)
	if err != nil {
		s.t.Errorf("darajastub: deliver %s callback to %s: %v", cb.route, cb.url, err)
		return delivery
	}
	defer resp.Body.Close()

	delivery.Status = resp.StatusCode
	return delivery
}
