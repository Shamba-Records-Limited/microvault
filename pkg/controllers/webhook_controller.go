package controllers

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"log/slog"

	"github.com/gofiber/fiber/v2"

	"github.com/Shamba-Records-Limited/microvault/pkg/payment/yellowcard"
	"github.com/Shamba-Records-Limited/microvault/pkg/webhook"
)

// WebhookController handles webhook endpoints for payment providers.
type WebhookController struct {
	eventHandler webhook.WebhookEventHandler
	apiKey       string
	secretKey    string
}

// NewWebhookController creates a new WebhookController with the provided
// dependencies. apiKey and secretKey are the YellowCard API credentials.
func NewWebhookController(eventHandler webhook.WebhookEventHandler, apiKey, secretKey string) *WebhookController {
	return &WebhookController{
		eventHandler: eventHandler,
		apiKey:       apiKey,
		secretKey:    secretKey,
	}
}

// HandleYellowCardWebhook processes incoming webhooks from YellowCard.
// @Description Handle incoming webhooks from YellowCard payment service
// @Summary Process YellowCard Payment Webhook
// @Tags Webhooks
// @Accept json
// @Produce json
// @Param X-YC-Signature header string true "HMAC signature for verification"
// @Param body body yellowcard.WebhookEvent true "Webhook event data"
// @Success 200 {string} string "ok"
// @Failure 400 {object} fiber.Error "Invalid webhook payload"
// @Failure 401 {object} fiber.Error "Signature verification failed"
// @Failure 500 {object} fiber.Error "Failed to process webhook"
// @Router /api/v1/webhooks/yellowcard [post]
func (ctrl *WebhookController) HandleYellowCardWebhook(c *fiber.Ctx) error {
	// 1. Verify HMAC signature if the secret key is configured.
	if ctrl.secretKey != "" {
		signature := c.Get("X-YC-Signature")
		if signature == "" {
			return fiber.NewError(fiber.StatusUnauthorized, "missing webhook signature")
		}

		mac := hmac.New(sha256.New, []byte(ctrl.secretKey))
		mac.Write(c.Body())
		expected := base64.StdEncoding.EncodeToString(mac.Sum(nil))

		if !hmac.Equal([]byte(signature), []byte(expected)) {
			slog.ErrorContext(c.UserContext(), "yellowcard webhook: signature mismatch")
			return fiber.NewError(fiber.StatusUnauthorized, "invalid webhook signature")
		}
	}

	// 2. Parse the webhook event.
	var event yellowcard.WebhookEvent
	if err := c.BodyParser(&event); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid webhook payload")
	}

	if ctrl.apiKey != "" && event.APIKey != ctrl.apiKey {
		slog.InfoContext(c.UserContext(), "yellowcard webhook: unrecognised apiKey")
		return fiber.NewError(fiber.StatusUnauthorized, "unrecognised apiKey")
	}

	if event.Event == "" || event.PaymentID == "" {
		return fiber.NewError(fiber.StatusBadRequest, "missing required webhook fields")
	}

	// 3. Process the event asynchronously (return 200 quickly to YC).
	if ctrl.eventHandler == nil {
		slog.InfoContext(c.UserContext(), "yellowcard webhook: no event handler configured", slog.String("event", event.Event), slog.String("payment_id", event.PaymentID))
		return c.SendString("ok")
	}

	ctx := context.WithoutCancel(c.UserContext())
	go func() {
		if err := ctrl.eventHandler.ProcessYellowCardEvent(ctx, event); err != nil {
			slog.ErrorContext(ctx, "yellowcard webhook: failed to process event", slog.String("event", event.Event), slog.String("payment_id", event.PaymentID), slog.Any("error", err))
		}
	}()

	return c.SendString("ok")
}
