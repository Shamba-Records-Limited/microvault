package controllers

import (
	"context"
	"errors"
	"time"

	"github.com/gofiber/fiber/v3"

	"github.com/Shamba-Records-Limited/microvault/pkg/compliance/didit"
	"github.com/Shamba-Records-Limited/microvault/pkg/middleware"
)

// DiditEventHandler records a verified Didit webhook.
type DiditEventHandler interface {
	HandleEvent(ctx context.Context, ev *didit.Event, body []byte) error
}

// DiditWebhookController receives Didit's signed status and data webhooks.
type DiditWebhookController struct {
	secret  string
	handler DiditEventHandler
	now     func() time.Time
}

// NewDiditWebhookController builds the controller. secret is the webhook
// destination's secret_shared_key; empty refuses every delivery.
func NewDiditWebhookController(secret string, handler DiditEventHandler) *DiditWebhookController {
	return &DiditWebhookController{secret: secret, handler: handler, now: time.Now}
}

// HandleDiditWebhook verifies and records a Didit webhook.
// @Description Receive Didit verification status and data webhooks
// @Summary Process Didit Webhook
// @Tags Webhooks
// @Accept json
// @Produce json
// @Param X-Signature-V2 header string true "HMAC-SHA256 over the canonical JSON body"
// @Param X-Timestamp header string true "Unix seconds when Didit sent the webhook"
// @Success 200 {string} string "ok"
// @Failure 401 {object} fiber.Error "Signature verification failed"
// @Failure 500 {object} fiber.Error "Failed to record the webhook"
// @Failure 503 {object} fiber.Error "Didit webhooks are not configured"
// @Router /api/v1/webhooks/didit [post]
func (ctrl *DiditWebhookController) HandleDiditWebhook(c fiber.Ctx) error {
	if ctrl.secret == "" || ctrl.handler == nil {
		return fiber.NewError(fiber.StatusServiceUnavailable, "didit webhooks are not configured")
	}
	body := append([]byte(nil), c.Body()...)
	ev, err := didit.VerifyWebhook(ctrl.secret, body, didit.WebhookHeaders{
		Timestamp:   c.Get("X-Timestamp"),
		SignatureV2: c.Get("X-Signature-V2"),
		PreviousV2:  c.Get("X-Signature-V2-Previous"),
		Signature:   c.Get("X-Signature"),
		PreviousSig: c.Get("X-Signature-Previous"),
	}, ctrl.now())
	switch {
	case errors.Is(err, didit.ErrStaleWebhook):
		return fiber.NewError(fiber.StatusUnauthorized, "webhook timestamp is outside the replay window")
	case errors.Is(err, didit.ErrInvalidSignature):
		return fiber.NewError(fiber.StatusUnauthorized, "invalid webhook signature")
	case err != nil:
		middleware.NoteError(c, err)
		return fiber.NewError(fiber.StatusBadRequest, "invalid webhook payload")
	}
	if err := ctrl.handler.HandleEvent(c.Context(), ev, body); err != nil {
		middleware.NoteError(c, err)
		return fiber.NewError(fiber.StatusInternalServerError, "could not record the webhook")
	}
	return c.SendString("ok")
}
