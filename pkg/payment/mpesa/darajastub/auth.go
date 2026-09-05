package darajastub

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"net/http"
	"strings"
)

// Daraja's four spellings of a rejected access token. The stub rotates through
// them so a client that recognises only one is caught here rather than in
// production, where three of its APIs would be unable to recover.
var invalidTokenCodes = []string{"404.001.03", "400.003.01", "401.002.01", "401.001"}

func (s *Stub) routeAuth() {
	s.handle("/oauth/v1/generate", func(w http.ResponseWriter, r *http.Request) {
		if f := s.takeFault(RouteAuth); f != nil {
			f.write(w)
			return
		}
		if r.URL.Query().Get("grant_type") != "client_credentials" {
			writeAPIError(w, http.StatusBadRequest, "400.008.02", "Invalid grant type passed")
			return
		}
		if !s.checkBasicAuth(r) {
			writeAPIError(w, http.StatusBadRequest, "400.008.01", "Invalid authentication type passed")
			return
		}

		token := s.mintToken()

		// expires_in is a quoted string on the wire. A client that decodes it
		// as a number fails here rather than at go-live.
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(w, `{"access_token":%q,"expires_in":%q}`, token, fmt.Sprintf("%d", int(s.tokenTTL.Seconds())))
	})
}

func (s *Stub) checkBasicAuth(r *http.Request) bool {
	key, secret, ok := r.BasicAuth()
	if !ok {
		return false
	}
	s.mu.Lock()
	wantKey, wantSecret := s.consumerKey, s.consumerSecret
	s.mu.Unlock()

	if wantKey == "" && wantSecret == "" {
		return key != "" && secret != ""
	}
	return key == wantKey && secret == wantSecret
}

// mintToken issues a token and invalidates the previous one, as Daraja does.
func (s *Stub) mintToken() string {
	buf := make([]byte, 12)
	_, _ = rand.Read(buf)
	token := base64.RawURLEncoding.EncodeToString(buf)

	s.mu.Lock()
	defer s.mu.Unlock()
	s.currentToken = token
	s.issued++
	return token
}

// Mints reports how many tokens have been issued. One mint across concurrent
// callers is what the client's single-flight exists to produce.
func (s *Stub) Mints() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.issued
}

// authorize enforces the bearer token, rejecting a superseded one.
func (s *Stub) authorize(w http.ResponseWriter, r *http.Request) bool {
	presented := strings.TrimSpace(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer"))

	s.mu.Lock()
	current, issued := s.currentToken, s.issued
	s.mu.Unlock()

	if presented == "" || current == "" || presented != current {
		code := invalidTokenCodes[issued%len(invalidTokenCodes)]
		writeAPIError(w, http.StatusUnauthorized, code, "Invalid Access Token")
		return false
	}
	return true
}

// ExpireToken invalidates the live token without minting a replacement, which
// is what another replica minting a token looks like from here.
func (s *Stub) ExpireToken() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.currentToken = ""
}
