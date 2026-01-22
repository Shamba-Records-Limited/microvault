package middleware

import (
	"time"

	"github.com/Shamba-Records-Limited/microvault/pkg/health"
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/cors"
	"github.com/gofiber/fiber/v2/middleware/favicon"
	"github.com/gofiber/fiber/v2/middleware/helmet"
	"github.com/gofiber/fiber/v2/middleware/limiter"
	"github.com/gofiber/fiber/v2/middleware/logger"
	"github.com/gofiber/fiber/v2/middleware/recover"
)

// FiberMiddleware provide Fiber's built-in middlewares.
// See: https://docs.gofiber.io/api/middleware
func FiberMiddleware(a *fiber.App, healthChecker *health.Checker) {
	limiterConfig := limiter.Config{
		Max:        20,
		Expiration: 30 * time.Second,
	}

	helmetConfig := helmet.Config{
		CrossOriginEmbedderPolicy: "false",
		ReferrerPolicy:            "same-origin",
		XFrameOptions:             "DENY",
	}

	corsConfig := cors.Config{
		AllowOrigins:     "http://localhost:3000,http://localhost:8080,http://localhost:8081",
		AllowCredentials: true,
		AllowHeaders:     "Origin,Content-Type,Accept,Authorization",
		AllowMethods:     "GET,POST,PUT,DELETE,PATCH,OPTIONS",
	}

	a.Use(
		// Helmet
		helmet.New(helmetConfig),
		// Panic recovery
		recover.New(),
		// Add CORS to each route with credentials enabled
		cors.New(corsConfig),
		// Limiter
		limiter.New(limiterConfig),
		// Add simple logger.
		logger.New(),
		// Favicon
		favicon.New(favicon.Config{
			File: "./static/favicon.ico",
			URL:  "/favicon.ico",
		}),
	)

	// Register health check routes
	a.Get("/health", healthChecker.HealthHandler)
	a.Get("/ready", healthChecker.ReadyHandler)
}
