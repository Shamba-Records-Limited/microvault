package middleware

import (
	"log/slog"
	"strings"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/cors"
	"github.com/gofiber/fiber/v3/middleware/favicon"
	"github.com/gofiber/fiber/v3/middleware/helmet"
	"github.com/gofiber/fiber/v3/middleware/limiter"
	"github.com/gofiber/fiber/v3/middleware/recover"

	"github.com/Shamba-Records-Limited/microvault/pkg/health"
)

const (
	limiterMax        = 20
	limiterExpiration = 30 * time.Second
	ussdCallbackPath  = "/api/v1/mobile/ussd/"
)

// generalLimiter limits each client IP across all routes except USSD
// callbacks, which all arrive from the gateway's few addresses.
func generalLimiter() fiber.Handler {
	return limiter.New(limiter.Config{
		Max:          limiterMax,
		Expiration:   limiterExpiration,
		KeyGenerator: ClientIP,
		Next: func(c fiber.Ctx) bool {
			return strings.HasPrefix(c.Path(), ussdCallbackPath)
		},
	})
}

// FailedCallbackLimiter limits only a client's failed requests, so a busy
// gateway is never throttled and guessing its callback token still is.
func FailedCallbackLimiter() fiber.Handler {
	return limiter.New(limiter.Config{
		Max:                    limiterMax,
		Expiration:             limiterExpiration,
		KeyGenerator:           ClientIP,
		SkipSuccessfulRequests: true,
	})
}

// FiberMiddleware provide Fiber's built-in middlewares.
// See: https://docs.gofiber.io/api/middleware
func FiberMiddleware(a *fiber.App, healthChecker *health.Checker, logger *slog.Logger) {
	helmetConfig := helmet.Config{
		CrossOriginEmbedderPolicy: "false",
		ReferrerPolicy:            "same-origin",
		XFrameOptions:             "DENY",
	}

	corsConfig := cors.Config{
		AllowOrigins:     []string{"http://localhost:3000", "http://localhost:8080", "http://localhost:8081"},
		AllowCredentials: true,
		AllowHeaders:     []string{"Origin", "Content-Type", "Accept", "Authorization"},
		AllowMethods:     []string{"GET", "POST", "PUT", "DELETE", "PATCH", "OPTIONS"},
	}

	a.Use(
		RequestID(),
		Tracing("/health", "/ready"),
		AccessLog(logger, "/health", "/ready"),
		// Helmet
		helmet.New(helmetConfig),
		// Panic recovery
		recover.New(),
		// Add CORS to each route with credentials enabled
		cors.New(corsConfig),
		// Limiter
		generalLimiter(),
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
