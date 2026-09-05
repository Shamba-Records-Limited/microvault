package darajastub

import "net/http"

// fault is one queued misbehaviour.
type fault struct {
	status  int
	code    string
	message string
	timeout bool
	drop    bool
}

func (f *fault) write(w http.ResponseWriter) {
	if f.drop {
		// Closing without a response models a transport-level ambiguity: the
		// caller cannot tell whether Daraja accepted the request.
		if hijacker, ok := w.(http.Hijacker); ok {
			if conn, _, err := hijacker.Hijack(); err == nil {
				_ = conn.Close()
				return
			}
		}
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	writeAPIError(w, f.status, f.code, f.message)
}

// FailNext makes the next request to route fail with a Daraja error code.
func (s *Stub) FailNext(route Route, status int, code, message string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.faults[route] = &fault{status: status, code: code, message: message}
}

// DropNext makes the next request to route return no response at all, so the
// caller cannot tell whether it was accepted. This is the ambiguity that makes
// a retry dangerous.
func (s *Stub) DropNext(route Route) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.faults[route] = &fault{drop: true}
}

// TimeoutNext makes the next request to route resolve to the caller's
// QueueTimeOutURL instead of its ResultURL. A queue timeout is not a failure,
// and code that treats it as one is how money moves twice.
func (s *Stub) TimeoutNext(route Route) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.faults[route] = &fault{timeout: true}
}

// LockCredential makes every Initiator-bearing call report 8006, as Daraja does
// when the API operator's password is locked.
func (s *Stub) LockCredential() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.credentialLocked = true
}

// DuplicateCallbacks delivers every queued callback twice. Daraja does this in
// practice, and a handler that is not idempotent double-credits.
func (s *Stub) DuplicateCallbacks() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.duplicate = true
}

// takeFault consumes a queued fault for route, if any. A timeout fault is not
// consumed here — it changes where a callback is delivered, not the
// acknowledgement — so it is left for the endpoint to read.
func (s *Stub) takeFault(route Route) *fault {
	s.mu.Lock()
	defer s.mu.Unlock()
	f, ok := s.faults[route]
	if !ok || f.timeout {
		return nil
	}
	delete(s.faults, route)
	return f
}

// takeTimeout reports whether route was told to time out, consuming the fault.
func (s *Stub) takeTimeout(route Route) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	f, ok := s.faults[route]
	if !ok || !f.timeout {
		return false
	}
	delete(s.faults, route)
	return true
}

// isCredentialLocked reports whether LockCredential was called.
func (s *Stub) isCredentialLocked() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.credentialLocked
}
