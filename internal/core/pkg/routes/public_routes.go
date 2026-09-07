package routes

import (
	"github.com/gofiber/fiber/v2"

	"github.com/Shamba-Records-Limited/microvault/pkg/controllers"
	"github.com/Shamba-Records-Limited/microvault/pkg/middleware"
)

// PublicRoutes func for describe group of public routes.
//
// darajaController may be nil: the M-Pesa callbacks are registered only when
// the integration is configured (a non-empty callback slug). Unauthenticated
// by design — Daraja signs nothing, and the slug plus the source-IP allowlist
// are the controls.
func PublicRoutes(a *fiber.App, authController *controllers.AuthController, ussdController *controllers.USSDController, webhookController *controllers.WebhookController, smsCallbackController *controllers.SMSCallbackController, darajaController *controllers.DarajaCallbackController) {
	// Create routes group.
	route := a.Group("/api/v1")

	// Routes for GET method:
	route.Get("/auth/challenge", middleware.FormatResponse(), authController.GetChallenge)

	// Routes for POST method:
	route.Post("/auth/verify", middleware.FormatResponse(), authController.VerifyChallenge)

	// USSD callback — supports multiple providers via URL param
	route.Post("/mobile/ussd/:provider", ussdController.HandleCallback)

	// SMS delivery report callback — supports multiple providers via URL param
	route.Post("/mobile/sms/:provider/delivery", smsCallbackController.HandleDeliveryReport)

	// Webhook routes (no auth middleware — verified via HMAC signature).
	route.Post("/webhooks/yellowcard", webhookController.HandleYellowCardWebhook)

	// Daraja callbacks — unauthenticated; the unguessable slug and the source
	// IP allowlist are the controls. Daraja signs nothing.
	if darajaController != nil {
		darajaController.Register(route)
	}
}
