package controllers

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"log"

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
			log.Printf("yellowcard webhook: signature mismatch")
			return fiber.NewError(fiber.StatusUnauthorized, "invalid webhook signature")
		}
	}

	// 2. Parse the webhook event.
	var event yellowcard.WebhookEvent
	if err := c.BodyParser(&event); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid webhook payload")
	}

	if ctrl.apiKey != "" && event.APIKey != ctrl.apiKey {
		log.Printf("yellowcard webhook: unrecognised apiKey")
		return fiber.NewError(fiber.StatusUnauthorized, "unrecognised apiKey")
	}

	if event.Event == "" || event.PaymentID == "" {
		return fiber.NewError(fiber.StatusBadRequest, "missing required webhook fields")
	}

	// 3. Process the event asynchronously (return 200 quickly to YC).
	if ctrl.eventHandler == nil {
		log.Printf("yellowcard webhook: received event %s for payment %s but no event handler configured",
			event.Event, event.PaymentID)
		return c.SendString("ok")
	}

	go func() {
		if err := ctrl.eventHandler.ProcessYellowCardEvent(event); err != nil {
			log.Printf("yellowcard webhook: failed to process event %s for payment %s: %v",
				event.Event, event.PaymentID, err)
		}
	}()

	return c.SendString("ok")
}
