package airtelstub

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"
)

// TransactionStatus mirrors the client's outcome set. It is redeclared rather
// than imported; see the package doc.
type TransactionStatus string

// The values the stub can resolve a transaction to.
const (
	StatusSuccess    TransactionStatus = "TS"
	StatusFailed     TransactionStatus = "TF"
	StatusAmbiguous  TransactionStatus = "TA"
	StatusInProgress TransactionStatus = "TIP"
	StatusExpired    TransactionStatus = "TE"
)

type tokenRequest struct {
	ClientID     string `json:"client_id"`
	ClientSecret string `json:"client_secret"`
	GrantType    string `json:"grant_type"`
}

func (s *Stub) routeAuth() {
	s.handle(pathAuth, func(w http.ResponseWriter, r *http.Request) {
		if f := s.takeFault(RouteAuth); f != nil {
			f.write(w)
			return
		}
		if r.Method != http.MethodPost {
			writeEnvelope(w, http.StatusMethodNotAllowed, failed("405", "ROUTER003", "Method not allowed"))
			return
		}

		var req tokenRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeEnvelope(w, http.StatusBadRequest, failed("400", "ROUTER003", "Malformed token request"))
			return
		}
		if req.GrantType != "client_credentials" {
			writeEnvelope(w, http.StatusBadRequest, failed("400", "ROUTER003", "Unsupported grant type"))
			return
		}
		if !s.credentialsMatch(req) {
			writeEnvelope(w, http.StatusUnauthorized, failed("401", "ROUTER003", "Invalid client credentials"))
			return
		}

		s.mu.Lock()
		s.issued++
		token := fmt.Sprintf("stub-token-%d", s.issued)
		s.currentToken = token
		ttl := s.tokenTTL
		s.mu.Unlock()

		writeJSON(w, http.StatusOK, map[string]any{
			"access_token": token,
			"expires_in":   int64(ttl.Seconds()),
			"token_type":   "bearer",
		})
	})
}

func (s *Stub) credentialsMatch(req tokenRequest) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.clientID != "" && req.ClientID != s.clientID {
		return false
	}
	if s.clientSecret != "" && req.ClientSecret != s.clientSecret {
		return false
	}
	return req.ClientID != "" && req.ClientSecret != ""
}

// authorize enforces the bearer token.
//
// Unlike Daraja, Airtel does not document superseding the previous token on a
// mint, so an older token is accepted while it would still be live. What is
// rejected is a token this stub never issued.
func (s *Stub) authorize(w http.ResponseWriter, r *http.Request) bool {
	header := r.Header.Get("Authorization")
	token := strings.TrimSpace(strings.TrimPrefix(header, "Bearer"))

	s.mu.Lock()
	issued := s.currentToken
	s.mu.Unlock()

	if token == "" || token != issued {
		writeEnvelope(w, http.StatusUnauthorized, failed("401", "ROUTER003", "Invalid access token"))
		return false
	}
	return true
}

func (s *Stub) routeKeys() {
	s.handleAuthed(RouteEncryptionKeys, pathKeys, func(w http.ResponseWriter, r *http.Request) {
		if !requireLocaleHeaders(w, r) {
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"data": map[string]any{
				"key_id":     1,
				"key":        s.publicKey,
				"valid_upto": time.Now().Add(24 * time.Hour).UTC().Format(time.RFC3339),
			},
			"status": ok(codeKeysOK, "Successfully fetched encryption key"),
		})
	})
}

// sharedKeyPair builds the RSA pair the stub signs and unseals with. 1024
// bits, matching what Airtel documents, so a client that assumes a larger
// modulus fails here.
func sharedKeyPair(t testing.TB) (key *rsa.PrivateKey, publicPEM string) {
	t.Helper()

	//nolint:gosec // G403: 1024 bits is what Airtel's own key length specifies; this is a test double for it.
	key, err := rsa.GenerateKey(rand.Reader, 1024)
	if err != nil {
		t.Fatalf("airtelstub: generate rsa key: %v", err)
	}

	der, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	if err != nil {
		t.Fatalf("airtelstub: marshal public key: %v", err)
	}
	encoded := pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der})
	if encoded == nil {
		t.Fatalf("airtelstub: encode public key")
	}
	return key, string(encoded)
}
