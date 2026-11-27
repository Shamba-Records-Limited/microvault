package controllers

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Shamba-Records-Limited/microvault/pkg/payment/yellowcard"
)

type chanYellowCardHandler struct{ events chan yellowcard.WebhookEvent }

func (h *chanYellowCardHandler) ProcessYellowCardEvent(_ context.Context, ev yellowcard.WebhookEvent) error {
	h.events <- ev
	return nil
}

func ycSign(secret string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return base64.StdEncoding.EncodeToString(mac.Sum(nil))
}

// YellowCard documents executedAt as an ISO string; a payload in that form
// must reach the handler instead of being refused as malformed.
func TestYellowCardWebhook_ISOExecutedAt(t *testing.T) {
	const secret, apiKey = "yc-secret", "yc-key"
	handler := &chanYellowCardHandler{events: make(chan yellowcard.WebhookEvent, 1)}
	ctrl := NewWebhookController(handler, apiKey, secret)

	for name, executedAt := range map[string]string{
		"iso string":   `"2026-11-27T09:15:30.459Z"`,
		"unix seconds": `1764234930`,
	} {
		t.Run(name, func(t *testing.T) {
			body := []byte(`{"id":"pay-1","sequenceId":"seq-1","status":"complete","apiKey":"` + apiKey +
				`","event":"SEND.COMPLETE","executedAt":` + executedAt + `}`)
			status := post(t, "/webhooks/yellowcard", ctrl.HandleYellowCardWebhook, body,
				map[string]string{"X-YC-Signature": ycSign(secret, body)})
			require.Equal(t, http.StatusOK, status)

			select {
			case ev := <-handler.events:
				assert.Equal(t, yellowcard.EventSendComplete, ev.Event)
				assert.Equal(t, "seq-1", ev.SequenceID)
				assert.False(t, ev.ExecutedAt.IsZero())
			case <-time.After(2 * time.Second):
				t.Fatal("event never reached the handler")
			}
		})
	}
}
