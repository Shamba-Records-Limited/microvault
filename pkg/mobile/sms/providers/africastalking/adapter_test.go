package africastalking

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Shamba-Records-Limited/microvault/pkg/mobile/sms"
)

const okBody = `{"SMSMessageData":{"Message":"Sent to 1/1","Recipients":[{"statusCode":101,"number":"+254700000000","status":"Success","cost":"KES 0.8000","messageId":"ATXid_1"}]}}`

func testAdapter(t *testing.T, baseURL string, timeout time.Duration) *AfricaTalkingSMSAdapter {
	t.Helper()
	return NewAfricasTalkingSMSAdapter("sandbox", "test-key", baseURL, timeout)
}

func testRequest() sms.SMSRequest {
	return sms.SMSRequest{To: "+254700000000", Message: "hello"}
}

// The failure that started this: the gateway accepts the connection but never
// answers within the budget. Every attempt must be spent before giving up.
func TestSendSMS_RetriesOnResponseTimeout(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		n := atomic.AddInt32(&calls, 1)
		if n < maxSendAttempts {
			time.Sleep(300 * time.Millisecond)
			return
		}
		_, _ = w.Write([]byte(okBody))
	}))
	defer srv.Close()

	a := testAdapter(t, srv.URL, 100*time.Millisecond)
	_, err := a.SendSMS(context.Background(), testRequest())

	require.NoError(t, err)
	assert.Equal(t, int32(maxSendAttempts), atomic.LoadInt32(&calls))
}

func TestSendSMS_RetriesOnServerError(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if atomic.AddInt32(&calls, 1) == 1 {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		_, _ = w.Write([]byte(okBody))
	}))
	defer srv.Close()

	a := testAdapter(t, srv.URL, 2*time.Second)
	_, err := a.SendSMS(context.Background(), testRequest())

	require.NoError(t, err)
	assert.Equal(t, int32(2), atomic.LoadInt32(&calls))
}

func TestSendSMS_RetriesOnRateLimit(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if atomic.AddInt32(&calls, 1) == 1 {
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		_, _ = w.Write([]byte(okBody))
	}))
	defer srv.Close()

	a := testAdapter(t, srv.URL, 2*time.Second)
	_, err := a.SendSMS(context.Background(), testRequest())

	require.NoError(t, err)
	assert.Equal(t, int32(2), atomic.LoadInt32(&calls))
}

// Bad credentials, an unregistered sender ID and exhausted credit all fail
// identically on every attempt; retrying only delays the error.
func TestSendSMS_DoesNotRetryClientRejection(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	a := testAdapter(t, srv.URL, 2*time.Second)
	_, err := a.SendSMS(context.Background(), testRequest())

	require.Error(t, err)
	assert.Contains(t, err.Error(), "401")
	assert.Equal(t, int32(1), atomic.LoadInt32(&calls), "a rejection must not be retried")
}

func TestSendSMS_GivesUpAfterMaxAttempts(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	a := testAdapter(t, srv.URL, 2*time.Second)
	_, err := a.SendSMS(context.Background(), testRequest())

	require.Error(t, err)
	assert.Contains(t, err.Error(), "after 3 attempts")
	assert.Equal(t, int32(maxSendAttempts), atomic.LoadInt32(&calls))
}

// The caller's budget bounds the whole sequence. A cancelled context is the
// caller giving up, so retrying would burn time it no longer has.
func TestSendSMS_StopsWhenCallerContextExpires(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	// Long enough for the first attempt, too short for the 1s backoff.
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	a := testAdapter(t, srv.URL, 2*time.Second)
	_, err := a.SendSMS(ctx, testRequest())

	require.Error(t, err)
	assert.Contains(t, err.Error(), "abandoned")
	assert.Equal(t, int32(1), atomic.LoadInt32(&calls))
}

func TestSendSMS_RequiresRecipient(t *testing.T) {
	a := testAdapter(t, "http://unused", time.Second)
	_, err := a.SendSMS(context.Background(), sms.SMSRequest{Message: "hello"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no recipients")
}

func TestNewAdapter_ZeroTimeoutFallsBackToDefault(t *testing.T) {
	assert.Equal(t, DefaultTimeout, testAdapter(t, "http://unused", 0).httpClient.Timeout)
}
