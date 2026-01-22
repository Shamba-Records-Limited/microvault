package routes

import (
	"github.com/Shamba-Records-Limited/Microvault/internal/core/app/controllers"
	"github.com/Shamba-Records-Limited/Microvault/pkg/middleware"
	"github.com/gofiber/fiber/v2"
)

// PublicRoutes func for describe group of public routes.
func PublicRoutes(a *fiber.App, authController *controllers.AuthController, ussdController *controllers.USSDController) {
	// Create routes group.
	route := a.Group("/api/v1")

	// Routes for GET method:
	route.Get("/auth/challenge", middleware.FormatResponse(), authController.GetChallenge)

	// Routes for POST method:
	route.Post("/auth/verify", middleware.FormatResponse(), authController.VerifyChallenge)
	route.Post("/mobile/ussd-callback", ussdController.HandleCallback)
}
