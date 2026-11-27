package controllers

import (
	"context"
	"log/slog"

	"github.com/gofiber/fiber/v3"

	"github.com/Shamba-Records-Limited/microvault/pkg/middleware"
	"github.com/Shamba-Records-Limited/microvault/pkg/payment/chipper"
	"github.com/Shamba-Records-Limited/microvault/pkg/payment/fonbnk"
)

// ChipperEventHandler applies verified Chipper events.
type ChipperEventHandler interface {
	ProcessChipperEvent(ctx context.Context, event chipper.Event) error
}

// FonbnkEventHandler applies verified Fonbnk events.
type FonbnkEventHandler interface {
	ProcessFonbnkEvent(ctx context.Context, event fonbnk.WebhookEvent) error
}

// ChipperWebhookController receives Chipper webhooks.
type ChipperWebhookController struct {
	verifier *chipper.WebhookVerifier
	handler  ChipperEventHandler
}

// NewChipperWebhookController builds the controller. A nil handler
// acknowledges deliveries without acting on them.
func NewChipperWebhookController(verifier *chipper.WebhookVerifier, handler ChipperEventHandler) *ChipperWebhookController {
	return &ChipperWebhookController{verifier: verifier, handler: handler}
}

// Handle verifies a Standard Webhooks delivery and applies it asynchronously.
// @Summary Process Chipper Webhook
// @Tags Webhooks
// @Accept json
// @Produce plain
// @Param webhook-id header string true "Delivery id"
// @Param webhook-timestamp header string true "Unix timestamp"
// @Param webhook-signature header string true "v1 HMAC-SHA256 signature"
// @Success 200 {string} string "ok"
// @Failure 400 {object} fiber.Error "Invalid webhook payload"
// @Failure 401 {object} fiber.Error "Signature verification failed"
// @Failure 503 {object} fiber.Error "Webhook verification not configured"
// @Router /api/v1/webhooks/chipper [post]
func (ctrl *ChipperWebhookController) Handle(c fiber.Ctx) error {
	if ctrl.verifier == nil {
		return fiber.NewError(fiber.StatusServiceUnavailable, "chipper webhooks are not configured")
	}

	body := append([]byte(nil), c.Body()...)
	event, err := ctrl.verifier.Parse(
		c.Get(chipper.HeaderWebhookID),
		c.Get(chipper.HeaderWebhookTimestamp),
		c.Get(chipper.HeaderWebhookSignature),
		body,
	)
	if err != nil {
		middleware.NoteError(c, err)
		return fiber.NewError(fiber.StatusUnauthorized, "invalid webhook signature")
	}
	if event.Event == "" {
		return fiber.NewError(fiber.StatusBadRequest, "missing event type")
	}

	if ctrl.handler == nil {
		slog.InfoContext(c.Context(), "chipper webhook: no handler configured", slog.String("event", event.Event))
		return c.SendString("ok")
	}

	ctx := context.WithoutCancel(c.Context())
	go func(ev chipper.Event) {
		if err := ctrl.handler.ProcessChipperEvent(ctx, ev); err != nil {
			slog.ErrorContext(ctx, "chipper webhook: failed to process event", slog.String("event", ev.Event), slog.Any("error", err))
		}
	}(*event)

	return c.SendString("ok")
}

// FonbnkWebhookController receives Fonbnk webhooks.
type FonbnkWebhookController struct {
	secret  string
	handler FonbnkEventHandler
}

// NewFonbnkWebhookController builds the controller. A nil handler
// acknowledges deliveries without acting on them.
func NewFonbnkWebhookController(secret string, handler FonbnkEventHandler) *FonbnkWebhookController {
	return &FonbnkWebhookController{secret: secret, handler: handler}
}

// Handle verifies a Fonbnk delivery and applies it asynchronously.
// @Summary Process Fonbnk Webhook
// @Tags Webhooks
// @Accept json
// @Produce plain
// @Param x-signature header string true "Webhook signature"
// @Success 200 {string} string "ok"
// @Failure 401 {object} fiber.Error "Signature verification failed"
// @Failure 503 {object} fiber.Error "Webhook verification not configured"
// @Router /api/v1/webhooks/fonbnk [post]
func (ctrl *FonbnkWebhookController) Handle(c fiber.Ctx) error {
	if ctrl.secret == "" {
		return fiber.NewError(fiber.StatusServiceUnavailable, "fonbnk webhooks are not configured")
	}

	body := append([]byte(nil), c.Body()...)
	event, err := fonbnk.ParseWebhook(body, c.Get(fonbnk.SignatureHeader), ctrl.secret)
	if err != nil {
		middleware.NoteError(c, err)
		return fiber.NewError(fiber.StatusUnauthorized, "invalid webhook signature")
	}

	if ctrl.handler == nil {
		slog.InfoContext(c.Context(), "fonbnk webhook: no handler configured", slog.String("event", event.Event))
		return c.SendString("ok")
	}

	ctx := context.WithoutCancel(c.Context())
	go func(ev fonbnk.WebhookEvent) {
		if err := ctrl.handler.ProcessFonbnkEvent(ctx, ev); err != nil {
			slog.ErrorContext(ctx, "fonbnk webhook: failed to process event", slog.String("order_id", ev.Data.Order.ID), slog.Any("error", err))
		}
	}(*event)

	return c.SendString("ok")
}
