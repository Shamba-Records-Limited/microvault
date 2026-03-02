package routes

import (
	"github.com/Shamba-Records-Limited/microvault/pkg/controllers"
	"github.com/Shamba-Records-Limited/microvault/pkg/middleware"
	"github.com/gofiber/fiber/v2"
)

// PublicRoutes func for describe group of public routes.
func PublicRoutes(a *fiber.App, authController *controllers.AuthController, ussdController *controllers.USSDController, webhookController *controllers.WebhookController, smsCallbackController *controllers.SMSCallbackController) {
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
}
