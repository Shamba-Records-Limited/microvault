package fonbnk

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// fonbnkTransport is a custom http.RoundTripper that injects the required
// Fonbnk auth headers into every request.
type fonbnkTransport struct {
	clientID     string
	clientSecret string
	base         http.RoundTripper
}

func padBase64(base64String string) string {
	return base64String + strings.Repeat("=", (4-len(base64String)%4)%4)
}

func generateSignature(clientSecret, timestamp, endpoint string) (string, error) {
	clientSecretPadded := padBase64(clientSecret)
	decodedSecret, err := base64.StdEncoding.DecodeString(clientSecretPadded)
	if err != nil {
		return "", err
	}
	message := fmt.Sprintf("%s:%s", timestamp, endpoint)
	h := hmac.New(sha256.New, decodedSecret)
	h.Write([]byte(message))
	return base64.StdEncoding.EncodeToString(h.Sum(nil)), nil
}

// RoundTrip signs the request and passes it to the base transport.
func (f *fonbnkTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	timestamp := fmt.Sprintf("%d", time.Now().UnixNano()/int64(time.Millisecond))

	endpoint := req.URL.Path
	if req.URL.RawQuery != "" {
		endpoint = fmt.Sprintf("%s?%s", endpoint, req.URL.RawQuery)
	}

	signature, err := generateSignature(f.clientSecret, timestamp, endpoint)
	if err != nil {
		return nil, fmt.Errorf("fonbnk: failed to generate signature: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-client-id", f.clientID)
	req.Header.Set("x-timestamp", timestamp)
	req.Header.Set("x-signature", signature)

	return f.base.RoundTrip(req)
}

// FonbnkAdapter is the low-level HTTP client for the Fonbnk API.
type FonbnkAdapter struct {
	httpClient *http.Client
	baseURL    string
}

// NewFonbnkAdapter builds the custom http.Client with the signing transport.
func NewFonbnkAdapter(clientID string, clientSecret string, baseURL string) *FonbnkAdapter {
	signingTransport := &fonbnkTransport{
		clientID:     clientID,
		clientSecret: clientSecret,
		base:         http.DefaultTransport,
	}

	client := &http.Client{
		Transport: signingTransport,
		Timeout:   15 * time.Second,
	}

	return &FonbnkAdapter{
		httpClient: client,
		baseURL:    baseURL,
	}
}
