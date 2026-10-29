package controllers

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Shamba-Records-Limited/microvault/pkg/compliance/didit"
)

type recordingDiditHandler struct {
	events []*didit.Event
	err    error
}

func (r *recordingDiditHandler) HandleEvent(_ context.Context, ev *didit.Event, _ []byte) error {
	r.events = append(r.events, ev)
	return r.err
}

func diditRequest(t *testing.T, secret string, body []byte, ts time.Time) *http.Request {
	t.Helper()
	canonical, err := didit.Canonical(body)
	require.NoError(t, err)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(canonical)
	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/webhooks/didit", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Timestamp", strconv.FormatInt(ts.Unix(), 10))
	req.Header.Set("X-Signature-V2", hex.EncodeToString(mac.Sum(nil)))
	return req
}

func diditApp(ctrl *DiditWebhookController) *fiber.App {
	app := fiber.New()
	app.Post("/webhooks/didit", ctrl.HandleDiditWebhook)
	return app
}

func TestDiditWebhook(t *testing.T) {
	body := []byte(`{"event":"status.updated","data":{"session_id":"bs_1","session_kind":"business","vendor_data":"cp-1","status":"APPROVED"}}`)
	now := time.Now()

	h := &recordingDiditHandler{}
	ctrl := NewDiditWebhookController("whsec", h)
	resp, err := diditApp(ctrl).Test(diditRequest(t, "whsec", body, now))
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	require.Len(t, h.events, 1)
	assert.Equal(t, "cp-1", h.events[0].Data.VendorData)

	resp, err = diditApp(ctrl).Test(diditRequest(t, "wrong", body, now))
	require.NoError(t, err)
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)

	resp, err = diditApp(ctrl).Test(diditRequest(t, "whsec", body, now.Add(-10*time.Minute)))
	require.NoError(t, err)
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode, "stale deliveries are refused")

	resp, err = diditApp(NewDiditWebhookController("", h)).Test(diditRequest(t, "whsec", body, now))
	require.NoError(t, err)
	assert.Equal(t, http.StatusServiceUnavailable, resp.StatusCode)

	h.err = errors.New("db down")
	resp, err = diditApp(ctrl).Test(diditRequest(t, "whsec", body, now))
	require.NoError(t, err)
	assert.Equal(t, http.StatusInternalServerError, resp.StatusCode, "a 5xx makes Didit retry")
}
