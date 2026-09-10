package elliptic

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const webhookSecret = "c2VjcmV0LWtleS1tYXRlcmlhbA=="

func computeWebhookSignature(t *testing.T, id, timestamp string, body []byte) string {
	t.Helper()
	secret, err := base64.StdEncoding.DecodeString(webhookSecret)
	require.NoError(t, err)
	h := hmac.New(sha256.New, secret)
	h.Write([]byte(id + "." + timestamp + "." + string(body)))
	return "v1," + base64.StdEncoding.EncodeToString(h.Sum(nil))
}

func TestVerifyWebhook(t *testing.T) {
	body := []byte(`{"event":{"type":"wallet.analysis.systemrescreening.completed"}}`)
	id := "msg_01J8Z"
	fresh := strconv.FormatInt(time.Now().Unix(), 10)
	sig := computeWebhookSignature(t, id, fresh, body)

	t.Run("valid signature verifies", func(t *testing.T) {
		err := VerifyWebhook(webhookSecret, id, fresh, sig, body)
		assert.NoError(t, err)
	})

	t.Run("stale timestamp is rejected", func(t *testing.T) {
		stale := strconv.FormatInt(time.Now().Add(-10*time.Minute).Unix(), 10)
		staleSig := computeWebhookSignature(t, id, stale, body)
		err := VerifyWebhook(webhookSecret, id, stale, staleSig, body)
		assert.Error(t, err)
	})

	t.Run("future timestamp beyond tolerance is rejected", func(t *testing.T) {
		future := strconv.FormatInt(time.Now().Add(10*time.Minute).Unix(), 10)
		futureSig := computeWebhookSignature(t, id, future, body)
		err := VerifyWebhook(webhookSecret, id, future, futureSig, body)
		assert.Error(t, err)
	})

	t.Run("wrong signature is rejected", func(t *testing.T) {
		err := VerifyWebhook(webhookSecret, id, fresh, "v1,not-the-right-signature", body)
		assert.Error(t, err)
	})

	t.Run("one of several space-separated signatures matching is accepted", func(t *testing.T) {
		multi := "v1,bogus-signature-one " + sig + " v1,bogus-signature-two"
		err := VerifyWebhook(webhookSecret, id, fresh, multi, body)
		assert.NoError(t, err)
	})

	t.Run("tampered body is rejected", func(t *testing.T) {
		err := VerifyWebhook(webhookSecret, id, fresh, sig, []byte(`{"event":{"type":"tampered"}}`))
		assert.Error(t, err)
	})

	t.Run("missing headers are rejected", func(t *testing.T) {
		assert.Error(t, VerifyWebhook(webhookSecret, "", fresh, sig, body))
		assert.Error(t, VerifyWebhook(webhookSecret, id, "", sig, body))
		assert.Error(t, VerifyWebhook(webhookSecret, id, fresh, "", body))
	})
}
