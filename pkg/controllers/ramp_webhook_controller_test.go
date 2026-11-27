package controllers

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Shamba-Records-Limited/microvault/pkg/payment/chipper"
	"github.com/Shamba-Records-Limited/microvault/pkg/payment/fonbnk"
)

type chanChipperHandler struct{ events chan chipper.Event }

func (h *chanChipperHandler) ProcessChipperEvent(_ context.Context, ev chipper.Event) error {
	h.events <- ev
	return nil
}

type chanFonbnkHandler struct{ events chan fonbnk.WebhookEvent }

func (h *chanFonbnkHandler) ProcessFonbnkEvent(_ context.Context, ev fonbnk.WebhookEvent) error {
	h.events <- ev
	return nil
}

func post(t *testing.T, path string, handler fiber.Handler, body []byte, headers map[string]string) int {
	t.Helper()
	app := fiber.New()
	app.Post(path, handler)
	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, path, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := app.Test(req)
	require.NoError(t, err)
	return resp.StatusCode
}

func TestChipperWebhookController(t *testing.T) {
	key := []byte("chipper-signing-key-for-tests!!!")
	verifier, err := chipper.NewWebhookVerifier("whsec_" + base64.StdEncoding.EncodeToString(key))
	require.NoError(t, err)

	body := []byte(`{"event":"payout.completed","data":{"id":"pay_kes_1","status":"completed","externalReference":"loan-1"}}`)
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte("whd_1." + ts + "."))
	mac.Write(body)
	signed := map[string]string{
		chipper.HeaderWebhookID:        "whd_1",
		chipper.HeaderWebhookTimestamp: ts,
		chipper.HeaderWebhookSignature: "v1," + base64.StdEncoding.EncodeToString(mac.Sum(nil)),
	}

	h := &chanChipperHandler{events: make(chan chipper.Event, 1)}
	ctrl := NewChipperWebhookController(verifier, h)
	assert.Equal(t, http.StatusOK, post(t, "/webhooks/chipper", ctrl.Handle, body, signed))
	select {
	case ev := <-h.events:
		assert.Equal(t, chipper.EventPayoutCompleted, ev.Event)
	case <-time.After(2 * time.Second):
		t.Fatal("event was not handled")
	}

	tampered := append(append([]byte{}, body[:len(body)-1]...), []byte(`,"x":1}`)...)
	assert.Equal(t, http.StatusUnauthorized, post(t, "/webhooks/chipper", ctrl.Handle, tampered, signed))
	assert.Equal(t, http.StatusUnauthorized, post(t, "/webhooks/chipper", ctrl.Handle, body, nil))
	assert.Empty(t, h.events, "rejected deliveries are not handled")

	assert.Equal(t, http.StatusOK, post(t, "/webhooks/chipper", NewChipperWebhookController(verifier, nil).Handle, body, signed))
	assert.Equal(t, http.StatusServiceUnavailable, post(t, "/webhooks/chipper", NewChipperWebhookController(nil, h).Handle, body, signed))
}

func TestFonbnkWebhookController(t *testing.T) {
	secret := "fonbnk-secret"
	body := []byte(`{"event":"order-status-change","data":{"order":{"_id":"ord_1","merchantOrderParams":"loan-1","type":"off_ramp","status":"payout_successful"}}}`)
	secretHash := sha256.Sum256([]byte(secret))
	sum := sha256.New()
	sum.Write(body)
	sum.Write([]byte(hex.EncodeToString(secretHash[:])))
	signed := map[string]string{fonbnk.SignatureHeader: hex.EncodeToString(sum.Sum(nil))}

	h := &chanFonbnkHandler{events: make(chan fonbnk.WebhookEvent, 1)}
	ctrl := NewFonbnkWebhookController(secret, h)
	assert.Equal(t, http.StatusOK, post(t, "/webhooks/fonbnk", ctrl.Handle, body, signed))
	select {
	case ev := <-h.events:
		assert.Equal(t, "loan-1", ev.Data.Order.MerchantOrderParams)
	case <-time.After(2 * time.Second):
		t.Fatal("event was not handled")
	}

	assert.Equal(t, http.StatusUnauthorized, post(t, "/webhooks/fonbnk", ctrl.Handle, body, map[string]string{fonbnk.SignatureHeader: "bad"}))
	assert.Equal(t, http.StatusServiceUnavailable, post(t, "/webhooks/fonbnk", NewFonbnkWebhookController("", h).Handle, body, signed))
	assert.Empty(t, h.events)
}
