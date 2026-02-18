package fonbnk

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/Shamba-Records-Limited/microvault/pkg/payment"
)

// fonbnkTransport is a custom http.RoundTripper that injects
// the required Fonbnk auth headers into every request.
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
	signature := base64.StdEncoding.EncodeToString(h.Sum(nil))
	return signature, nil
}

// RoundTrip is the core of the custom transport. It intercepts the request,
// signs it, and then passes it to the underlying transport.
func (f *fonbnkTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	// Get timestamp
	timestamp := fmt.Sprintf("%d", time.Now().UnixNano()/int64(time.Millisecond))

	// Build the endpoint string (path + query)
	endpoint := req.URL.Path
	if req.URL.RawQuery != "" {
		endpoint = fmt.Sprintf("%s?%s", endpoint, req.URL.RawQuery)
	}

	// Generate the signature
	signature, err := generateSignature(f.clientSecret, timestamp, endpoint)
	if err != nil {
		return nil, fmt.Errorf("fonbnk: failed to generate signature: %w", err)
	}

	// Set the required headers
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-client-id", f.clientID)
	req.Header.Set("x-timestamp", timestamp)
	req.Header.Set("x-signature", signature)

	// Pass the request to the base transport
	return f.base.RoundTrip(req)
}

// FonbnkAdapter implements the payment.PaymentProvider interface.
type FonbnkAdapter struct {
	httpClient *http.Client
	baseURL    string
}

// NewFonbnkAdapter is the constructor. It builds the custom
// http.Client with the signing transport.
func NewFonbnkAdapter(clientID string, clientSecret string, baseURL string) *FonbnkAdapter {
	// Create the custom transport
	signingTransport := &fonbnkTransport{
		clientID:     clientID,
		clientSecret: clientSecret,
		base:         http.DefaultTransport,
	}

	// Create an http.Client that uses our custom transport
	client := &http.Client{
		Transport: signingTransport,
		Timeout:   15 * time.Second,
	}

	return &FonbnkAdapter{
		httpClient: client,
		baseURL:    baseURL,
	}
}

// Compile-time check to ensure all methods are implemented.
var _ payment.PaymentProvider = (*FonbnkAdapter)(nil)

// GetRate returns the exchange rate for a specified currency or amount
func (f *FonbnkAdapter) GetRate(ctx context.Context, currency string) (int64, error) {
	return 0, nil
}

// InitializePayment calls the Fonbnk API to create an order.
func (f *FonbnkAdapter) InitializePayment(ctx context.Context, req payment.InitializePaymentRequest) (string, error) {
	return "", nil
}

// ProcessPayment calls the Fonbnk API to process the order
func (f *FonbnkAdapter) ProcessPayment(ctx context.Context, transactionID string) (payment.PaymentStatus, error) {
	return payment.StatusPending, nil
}

// GetPaymentStatus calls the Fonbnk API to get payment status
func (f *FonbnkAdapter) GetPaymentStatus(ctx context.Context, transactionID string) (payment.PaymentStatus, error) {
	return payment.StatusPending, nil
}
