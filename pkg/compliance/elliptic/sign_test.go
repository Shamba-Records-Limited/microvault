package elliptic

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// referenceSign independently reimplements Elliptic's documented algorithm
// (source design doc §3's Go sample), separately from sign() in sign.go, so
// this test catches a divergence rather than confirming sign() against
// itself.
func referenceSign(t *testing.T, secretB64 string, timestampMs int64, method, path, payload string) string {
	t.Helper()
	secret, err := base64.StdEncoding.DecodeString(secretB64)
	require.NoError(t, err)
	h := hmac.New(sha256.New, secret)
	h.Write([]byte(strconv.FormatInt(timestampMs, 10) + method + strings.ToLower(path) + payload))
	return base64.StdEncoding.EncodeToString(h.Sum(nil))
}

func TestSign(t *testing.T) {
	const secret = "c2VjcmV0LWtleS1tYXRlcmlhbA==" // base64("secret-key-material")

	cases := []struct {
		name        string
		timestampMs int64
		method      string
		path        string
		payload     string
	}{
		{"POST with body", 1_757_500_000_000, "POST", "/v2/wallet/synchronous", `{"subject":{"asset":"USDC"}}`},
		{"GET with empty-object payload", 1_757_500_000_000, "GET", "/v2/wallet/count", "{}"},
		{"path with mixed case is lowercased for signing", 1_757_500_000_000, "GET", "/V2/Wallet/COUNT", "{}"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := sign(secret, tc.timestampMs, tc.method, tc.path, tc.payload)
			require.NoError(t, err)
			want := referenceSign(t, secret, tc.timestampMs, tc.method, tc.path, tc.payload)
			assert.Equal(t, want, got)
		})
	}
}

func TestSign_InvalidSecretIsAnError(t *testing.T) {
	_, err := sign("not-valid-base64!!!", 1_757_500_000_000, "GET", "/v2/wallet/count", "{}")
	assert.Error(t, err)
}

func TestSign_TimestampInMillisecondsChangesSignature(t *testing.T) {
	seconds, err := sign("c2VjcmV0", 1_757_500_000, "GET", "/v2/wallet/count", "{}")
	require.NoError(t, err)
	millis, err := sign("c2VjcmV0", 1_757_500_000_000, "GET", "/v2/wallet/count", "{}")
	require.NoError(t, err)
	assert.NotEqual(t, seconds, millis, "a three-digit-short timestamp must not produce the same signature")
}
