package controllers

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type recordingMerkleHandler struct {
	bodies [][]byte
	err    error
}

func (r *recordingMerkleHandler) HandleMerkleWebhook(_ context.Context, body []byte) error {
	r.bodies = append(r.bodies, body)
	return r.err
}

func merkleStatus(t *testing.T, ctrl *MerkleWebhookController, key, body string) int {
	t.Helper()
	app := fiber.New()
	app.Post("/webhooks/merkle", ctrl.HandleMerkleWebhook)
	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/webhooks/merkle", bytes.NewReader([]byte(body)))
	req.Header.Set("Content-Type", "application/json")
	if key != "" {
		req.Header.Set("X-WEBHOOK-KEY", key)
	}
	resp, err := app.Test(req)
	require.NoError(t, err)
	return resp.StatusCode
}

func TestMerkleWebhook(t *testing.T) {
	h := &recordingMerkleHandler{}
	ctrl := NewMerkleWebhookController("whkey", h)
	assert.Equal(t, http.StatusOK, merkleStatus(t, ctrl, "whkey", `{"event_type":"alert"}`))
	assert.Len(t, h.bodies, 1)

	assert.Equal(t, http.StatusUnauthorized, merkleStatus(t, ctrl, "wrong", `{}`))
	assert.Equal(t, http.StatusUnauthorized, merkleStatus(t, ctrl, "", `{}`))
	assert.Equal(t, http.StatusBadRequest, merkleStatus(t, ctrl, "whkey", `nope`))
	assert.Len(t, h.bodies, 1, "rejected deliveries are not handled")

	h.err = errors.New("db down")
	assert.Equal(t, http.StatusInternalServerError, merkleStatus(t, ctrl, "whkey", `{}`))

	assert.Equal(t, http.StatusServiceUnavailable, merkleStatus(t, NewMerkleWebhookController("", h), "", `{}`))
}
