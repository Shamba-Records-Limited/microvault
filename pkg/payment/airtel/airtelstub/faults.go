package airtelstub

import "net/http"

// fault is one queued misbehaviour.
type fault struct {
	status  int
	code    string
	message string
	drop    bool
}

func (f *fault) write(w http.ResponseWriter) {
	if f.drop {
		// Closing without a response models a transport-level ambiguity: the
		// caller cannot tell whether Airtel accepted the request, which is
		// exactly the state in which a retry pays twice.
		if hijacker, ok := w.(http.Hijacker); ok {
			if conn, _, err := hijacker.Hijack(); err == nil {
				_ = conn.Close()
				return
			}
		}
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	writeEnvelope(w, f.status, failed(http.StatusText(f.status), f.code, f.message))
}

// FailNext makes the next request to route fail with an Airtel response code.
func (s *Stub) FailNext(route Route, status int, code, message string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.faults[route] = &fault{status: status, code: code, message: message}
}

// DropNext makes the next request to route return no response at all.
func (s *Stub) DropNext(route Route) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.faults[route] = &fault{drop: true}
}

// TimeoutNext makes the next request to route answer 504, which Airtel
// documents as an instruction to enquire rather than retry.
func (s *Stub) TimeoutNext(route Route) {
	s.FailNext(route, http.StatusGatewayTimeout, "ROUTER117", "Request timeout")
}

// AmbiguousNext makes the next payment resolve to the ambiguous code. The
// transaction still exists and still settles; only the acknowledgement is
// unhelpful. A caller that retries instead of enquiring pays twice, and this
// is the fault that proves it does not.
func (s *Stub) AmbiguousNext() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.nextOutcome = &outcome{status: string(StatusInProgress), code: codeAmbiguous}
}

// NextOutcome fixes how the next payment resolves.
func (s *Stub) NextOutcome(status TransactionStatus, code string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.nextOutcome = &outcome{status: string(status), code: code}
}

// takeFault consumes a queued fault for route, if any.
func (s *Stub) takeFault(route Route) *fault {
	s.mu.Lock()
	defer s.mu.Unlock()
	f, ok := s.faults[route]
	if !ok {
		return nil
	}
	delete(s.faults, route)
	return f
}

// takeOutcome consumes the queued outcome, defaulting to success.
func (s *Stub) takeOutcome() outcome {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.nextOutcome == nil {
		return outcome{status: string(StatusSuccess), code: codeSuccess}
	}
	next := *s.nextOutcome
	s.nextOutcome = nil
	return next
}
