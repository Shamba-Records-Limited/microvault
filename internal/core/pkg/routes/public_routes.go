package routes

import (
	"github.com/gofiber/fiber/v2"

	"github.com/Shamba-Records-Limited/microvault/pkg/controllers"
	"github.com/Shamba-Records-Limited/microvault/pkg/middleware"
)

// PublicRoutes func for describe group of public routes.
//
// darajaController and airtelController may each be nil: a rail's callbacks
// are registered only when that integration is configured (a non-empty
// callback slug). Both are unauthenticated by design — Daraja signs nothing,
// and Airtel's signature is optional and configured at their end, so the
// unguessable slug plus the source-IP allowlist are the controls on each.
func PublicRoutes(a *fiber.App, authController *controllers.AuthController, ussdController *controllers.USSDController, webhookController *controllers.WebhookController, smsCallbackController *controllers.SMSCallbackController, darajaController *controllers.DarajaCallbackController, hakikishaController *controllers.DarajaHakikishaController, airtelController *controllers.AirtelCallbackController) {
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

	// Hakikisha inverts the direction: Safaricom authenticates to us. Same
	// nil-when-unconfigured guard as darajaController.
	if hakikishaController != nil {
		hakikishaController.Register(route)
	}

	// Airtel collection callbacks. Unlike Daraja's, these can carry an
	// HmacSHA256 the controller verifies — but a verified hash still only
	// proves Airtel sent it, never that the payment settled.
	if airtelController != nil {
		airtelController.Register(route)
	}
}
