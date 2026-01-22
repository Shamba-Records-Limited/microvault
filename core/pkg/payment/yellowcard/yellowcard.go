package yellowcard

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/Shamba-Records-Limited/Microvault/internal/core/pkg/payment"
)

// yellowcardTransport is a custom http.RoundTripper that injects
// the required Yellow Card auth headers into every request.
type yellowcardTransport struct {
	apiKey    string
	apiSecret string
	base      http.RoundTripper
}

// RoundTrip intercepts the request, signs it, and passes it on.
func (t *yellowcardTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	// Get timestamp
	date := time.Now().UTC().Format(time.RFC3339)

	// Get path and query
	path := req.URL.Path
	if req.URL.RawQuery != "" {
		path = fmt.Sprintf("%s?%s", path, req.URL.RawQuery)
	}

	// Create HMAC signature
	h := hmac.New(sha256.New, []byte(t.apiSecret))
	h.Write([]byte(date))
	h.Write([]byte(path))
	h.Write([]byte(req.Method))

	// Handle the body (if it exists)
	if req.Body != nil && req.Body != http.NoBody {
		// Read the body
		bodyBytes, err := io.ReadAll(req.Body)
		if err != nil {
			return nil, fmt.Errorf("yellowcard: failed to read request body: %w", err)
		}
		// Put the body back so the transport can read it
		req.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))

		// Hash the body and add to signature
		bodyHmac := sha256.Sum256(bodyBytes)
		bodyBase64 := base64.StdEncoding.EncodeToString(bodyHmac[:])
		h.Write([]byte(bodyBase64[:]))
	}

	// Finalize signature
	signature := base64.StdEncoding.EncodeToString(h.Sum(nil))

	// Set headers
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-YC-Timestamp", date)
	req.Header.Set("Authorization", fmt.Sprintf("YcHmacV1 %s:%s", t.apiKey, signature))

	// Pass to base transport
	return t.base.RoundTrip(req)
}

// YellowcardAdapter implements the payment.PaymentProvider interface.
type YellowcardAdapter struct {
	httpClient *http.Client
	baseURL    string
}

// NewYellowcardAdapter is the constructor. It builds the custom
// http.Client with the signing transport.
func NewYellowcardAdapter(apiKey, apiSecret, baseURL string) *YellowcardAdapter {
	// Create the custom transport
	signingTransport := &yellowcardTransport{
		apiKey:    apiKey,
		apiSecret: apiSecret,
		base:      http.DefaultTransport,
	}

	// Create an http.Client that uses our custom transport
	client := &http.Client{
		Transport: signingTransport,
		Timeout:   10 * time.Second,
	}

	return &YellowcardAdapter{
		httpClient: client,
		baseURL:    baseURL,
	}
}

// Compile-time check to ensure all interface methods are implemented.
var _ payment.PaymentProvider = (*YellowcardAdapter)(nil)

// GetRate returns the exchange rate for a specified currency or amount
func (y *YellowcardAdapter) GetRate(ctx context.Context, currency string) (int64, error) {
	return 0, nil
}

// InitializePayment creates a new payment.
func (y *YellowcardAdapter) InitializePayment(ctx context.Context, request payment.InitializePaymentRequest) (string, error) {
	return "", nil
}

// ProcessPayment processes an existing payment (e.g., capture).
func (y *YellowcardAdapter) ProcessPayment(ctx context.Context, transactionID string) (payment.PaymentStatus, error) {
	return payment.StatusPending, nil
}

// GetPaymentStatus retrieves the status of a payment.
func (y *YellowcardAdapter) GetPaymentStatus(ctx context.Context, transactionID string) (payment.PaymentStatus, error) {
	return payment.StatusPending, nil
}
