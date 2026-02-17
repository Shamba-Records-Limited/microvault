package controllers

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"log"

	"github.com/gofiber/fiber/v2"

	"github.com/Shamba-Records-Limited/microvault/internal/core/pkg/payment/yellowcard"
)

// WebhookEventHandler processes incoming YellowCard webhook events.
type WebhookEventHandler interface {
	// ProcessYellowCardEvent handles a single webhook event, mapping it to the
	// appropriate disbursement status update and triggering any side effects.
	ProcessYellowCardEvent(event yellowcard.WebhookEvent) error
}

// WebhookController handles webhook endpoints for payment providers.
type WebhookController struct {
	eventHandler  WebhookEventHandler
	webhookSecret string
}

// NewWebhookController creates a new WebhookController with the provided dependencies.
func NewWebhookController(eventHandler WebhookEventHandler, webhookSecret string) *WebhookController {
	return &WebhookController{
		eventHandler:  eventHandler,
		webhookSecret: webhookSecret,
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
	// 1. Verify HMAC signature if webhook secret is configured.
	if ctrl.webhookSecret != "" {
		signature := c.Get("X-YC-Signature")
		if signature == "" {
			return fiber.NewError(fiber.StatusUnauthorized, "missing webhook signature")
		}

		body := c.Body()
		mac := hmac.New(sha256.New, []byte(ctrl.webhookSecret))
		mac.Write(body)
		expected := hex.EncodeToString(mac.Sum(nil))

		if !hmac.Equal([]byte(signature), []byte(expected)) {
			log.Printf("yellowcard webhook: signature mismatch")
			return fiber.NewError(fiber.StatusUnauthorized, "invalid webhook signature")
		}
	}

	// 2. Parse the webhook event.
	var event yellowcard.WebhookEvent
	if err := c.BodyParser(&event); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid webhook payload")
	}

	if event.Event == "" || event.Data.PaymentID == "" {
		return fiber.NewError(fiber.StatusBadRequest, "missing required webhook fields")
	}

	// 3. Process the event asynchronously (return 200 quickly to YC).
	if ctrl.eventHandler == nil {
		log.Printf("yellowcard webhook: received event %s for payment %s but no event handler configured",
			event.Event, event.Data.PaymentID)
		return c.SendString("ok")
	}

	go func() {
		if err := ctrl.eventHandler.ProcessYellowCardEvent(event); err != nil {
			log.Printf("yellowcard webhook: failed to process event %s for payment %s: %v",
				event.Event, event.Data.PaymentID, err)
		}
	}()

	return c.SendString("ok")
}
