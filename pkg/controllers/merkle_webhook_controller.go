package controllers

import (
	"context"

	"github.com/gofiber/fiber/v3"

	"github.com/Shamba-Records-Limited/microvault/pkg/compliance/merkle"
	"github.com/Shamba-Records-Limited/microvault/pkg/middleware"
)

// MerkleEventHandler queues an authenticated Compass notification.
type MerkleEventHandler interface {
	HandleMerkleWebhook(ctx context.Context, body []byte) error
}

// MerkleWebhookController receives Merkle Science Compass notifications.
type MerkleWebhookController struct {
	key     string
	handler MerkleEventHandler
}

// NewMerkleWebhookController builds the controller. key is the webhook
// secret set in Compass; empty refuses every delivery.
func NewMerkleWebhookController(key string, handler MerkleEventHandler) *MerkleWebhookController {
	return &MerkleWebhookController{key: key, handler: handler}
}

// HandleMerkleWebhook authenticates and queues a Compass notification.
// @Description Receive Merkle Science Compass risk and alert notifications
// @Summary Process Merkle Science Webhook
// @Tags Webhooks
// @Accept json
// @Produce json
// @Param X-WEBHOOK-KEY header string true "Webhook secret configured in Compass"
// @Success 200 {string} string "ok"
// @Failure 400 {object} fiber.Error "Body is not JSON"
// @Failure 401 {object} fiber.Error "Webhook key mismatch"
// @Failure 500 {object} fiber.Error "Failed to record the webhook"
// @Failure 503 {object} fiber.Error "Merkle Science webhooks are not configured"
// @Router /api/v1/webhooks/merkle [post]
func (ctrl *MerkleWebhookController) HandleMerkleWebhook(c fiber.Ctx) error {
	if ctrl.key == "" || ctrl.handler == nil {
		return fiber.NewError(fiber.StatusServiceUnavailable, "merkle science webhooks are not configured")
	}
	if !merkle.WebhookKeyValid(ctrl.key, c.Get(merkle.WebhookKeyHeader)) {
		return fiber.NewError(fiber.StatusUnauthorized, "invalid webhook key")
	}
	body := append([]byte(nil), c.Body()...)
	if _, err := merkle.ParseWebhook(body); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid webhook payload")
	}
	if err := ctrl.handler.HandleMerkleWebhook(c.Context(), body); err != nil {
		middleware.NoteError(c, err)
		return fiber.NewError(fiber.StatusInternalServerError, "could not record the webhook")
	}
	return c.SendString("ok")
}
