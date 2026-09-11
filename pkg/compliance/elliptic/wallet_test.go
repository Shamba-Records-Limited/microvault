package elliptic

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Shamba-Records-Limited/microvault/pkg/compliance"
)

// readAndRestore reads a request body and puts a fresh copy back, so a
// test handler can inspect the raw bytes and still decode them normally.
func readAndRestore(r *http.Request) ([]byte, error) {
	if r.Body == nil {
		return nil, nil
	}
	b, err := io.ReadAll(r.Body)
	if err != nil {
		return nil, err
	}
	r.Body = io.NopCloser(bytes.NewReader(b))
	return b, nil
}

const testSecret = "c2VjcmV0LWtleS1tYXRlcmlhbA=="

func TestScreenAddress_SuccessfulResponse(t *testing.T) {
	const address = "GCUUFSK3P7YMFMYVA3L7LVQ7TCWJK7WUOFMXLX2HYSRCFB37ZGZD4CCX"

	var gotBody walletScreeningRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/wallet/synchronous", r.URL.Path)
		assert.Equal(t, http.MethodPost, r.Method)

		// The signing transport must have signed the exact body bytes sent.
		verifySignedRequest(t, r, testSecret)

		require.NoError(t, json.NewDecoder(r.Body).Decode(&gotBody))

		resp := walletScreeningResponse{
			ID:              "analysis-1",
			ScreeningID:     "screening-1",
			ScreeningSource: "sync",
			ProcessStatus:   "complete",
			RiskScore:       f(5),
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp) //nolint:errcheck
	}))
	defer server.Close()

	client := NewClient(Config{
		APIKey:     "test-key",
		APISecret:  testSecret,
		BaseURL:    server.URL,
		Thresholds: Thresholds{Review: 50, Reject: 80},
	})

	screening, err := client.ScreenAddress(context.Background(), compliance.ScreenRequest{
		Address:           address,
		CustomerReference: "kyb-001",
	})
	require.NoError(t, err)

	assert.Equal(t, stellarAsset, gotBody.Subject.Asset)
	assert.Equal(t, stellarBlockchain, gotBody.Subject.Blockchain)
	assert.Equal(t, "address", gotBody.Subject.Type)
	assert.Equal(t, address, gotBody.Subject.Hash)
	assert.Equal(t, "wallet_exposure", gotBody.Type)
	assert.Equal(t, "kyb-001", gotBody.CustomerReference)

	assert.Equal(t, address, screening.Address)
	assert.Equal(t, compliance.VerdictApproved, screening.Verdict)
	require.NotNil(t, screening.RiskScore)
	assert.Equal(t, 5.0, *screening.RiskScore)
	assert.Equal(t, "analysis-1", screening.AnalysisID)
	assert.Equal(t, "screening-1", screening.ScreeningID)
	assert.NotEmpty(t, screening.Raw)
}

func TestScreenAddress_404IsUnscreenableNotAnError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`{"error":"NotInBlockchain"}`)) //nolint:errcheck
	}))
	defer server.Close()

	client := NewClient(Config{APIKey: "k", APISecret: testSecret, BaseURL: server.URL})

	screening, err := client.ScreenAddress(context.Background(), compliance.ScreenRequest{
		Address: "GCUUFSK3P7YMFMYVA3L7LVQ7TCWJK7WUOFMXLX2HYSRCFB37ZGZD4CCX",
	})
	require.NoError(t, err, "a 404 from elliptic must not surface as an error")
	assert.Equal(t, compliance.VerdictUnscreenable, screening.Verdict)
}

func TestScreenAddress_Unauthorized(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer server.Close()

	client := NewClient(Config{APIKey: "k", APISecret: testSecret, BaseURL: server.URL})
	_, err := client.ScreenAddress(context.Background(), compliance.ScreenRequest{Address: "GCUUFSK3P7YMFMYVA3L7LVQ7TCWJK7WUOFMXLX2HYSRCFB37ZGZD4CCX"})
	require.Error(t, err)
	assert.False(t, errors.Is(err, ErrNotInBlockchain))
}

func TestScreenAddress_EmptyAddressIsRejectedLocally(t *testing.T) {
	client := NewClient(Config{APIKey: "k", APISecret: testSecret, BaseURL: "http://unused.invalid"})
	_, err := client.ScreenAddress(context.Background(), compliance.ScreenRequest{Address: ""})
	assert.Error(t, err)
}

// verifySignedRequest recomputes the expected signature from the request's
// own headers and body and asserts it matches x-access-sign — an
// end-to-end check that the signing transport signs exactly what is sent.
func verifySignedRequest(t *testing.T, r *http.Request, secret string) {
	t.Helper()
	timestamp := r.Header.Get("x-access-timestamp")
	require.NotEmpty(t, timestamp)
	ts, err := strconv.ParseInt(timestamp, 10, 64)
	require.NoError(t, err)

	body, err := readAndRestore(r)
	require.NoError(t, err)
	payload := "{}"
	if len(body) > 0 {
		payload = string(body)
	}

	want, err := sign(secret, ts, r.Method, r.URL.Path, payload)
	require.NoError(t, err)
	assert.Equal(t, want, r.Header.Get("x-access-sign"))
}
