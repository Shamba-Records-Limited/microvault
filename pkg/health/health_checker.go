package health

import (
	"context"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/stellar/go-stellar-sdk/clients/rpcclient"

	"github.com/Shamba-Records-Limited/microvault/platform/cache"
	"github.com/Shamba-Records-Limited/microvault/platform/database"
)

// Checker manages health checks for various services
type Checker struct {
	stellarClient *rpcclient.Client
	dbName        string
	cacheName     string
}

// NewChecker creates a new health checker
// stellarClient is optional - pass nil if not needed
func NewChecker(stellarClient *rpcclient.Client, dbName string, cacheName string) *Checker {
	return &Checker{
		stellarClient: stellarClient,
		dbName:        dbName,
		cacheName:     cacheName,
	}
}

// NewCheckerWithoutStellar creates a health checker without Stellar integration
func NewCheckerWithoutStellar(dbName string, cacheName string) *Checker {
	return &Checker{
		stellarClient: nil,
		dbName:        dbName,
		cacheName:     cacheName,
	}
}

// LivenessProbe returns true if the application is alive
// This should always return true if the app is running
func (h *Checker) LivenessProbe(c *fiber.Ctx) bool {
	return true
}

// ReadinessProbe returns true if all critical services are ready
func (h *Checker) ReadinessProbe(c *fiber.Ctx) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	databaseReady := database.Ready(h.dbName)
	cacheReady := cache.Ready(h.cacheName)

	if h.stellarClient == nil {
		return databaseReady && cacheReady
	}
	stellarReady := h.checkStellar(ctx)
	return databaseReady && cacheReady && stellarReady
}

// checkStellar verifies Stellar client connectivity
func (h *Checker) checkStellar(ctx context.Context) bool {
	if h.stellarClient == nil {
		return false
	}

	// Try to get stellar network info
	if _, err := h.stellarClient.GetNetwork(ctx); err != nil {
		return false
	}

	if _, err := h.stellarClient.GetLatestLedger(ctx); err != nil {
		return false
	}

	return true
}

func (h *Checker) HealthHandler(c *fiber.Ctx) error {
	if h.LivenessProbe(c) {
		return c.Status(fiber.StatusOK).JSON(fiber.Map{
			"status":    "alive",
			"timestamp": time.Now(),
		})
	}
	return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{
		"status":    "unhealthy",
		"timestamp": time.Now(),
	})
}

func (h *Checker) ReadyHandler(c *fiber.Ctx) error {
	// Use the existing ReadinessProbe method
	if h.ReadinessProbe(c) {
		if h.stellarClient == nil {
			return c.Status(fiber.StatusOK).JSON(fiber.Map{
				"status":    "ready",
				"timestamp": time.Now(),
				"services": fiber.Map{
					"database": "healthy",
					"cache":    "healthy",
				},
			})
		}
		return c.Status(fiber.StatusOK).JSON(fiber.Map{
			"status":    "ready",
			"timestamp": time.Now(),
			"services": fiber.Map{
				"database": "healthy",
				"cache":    "healthy",
				"stellar":  "healthy",
			},
		})
	}

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	// If not ready, provide detailed status for debugging
	if h.stellarClient == nil {
		return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{
			"status":    "not ready",
			"timestamp": time.Now(),
			"services": fiber.Map{
				"database": func() string {
					if database.Ready(h.dbName) {
						return "healthy"
					}
					return "unhealthy"
				}(),
				"cache": func() string {
					if cache.Ready(h.cacheName) {
						return "healthy"
					}
					return "unhealthy"
				}(),
			},
		})
	}

	return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{
		"status":    "not ready",
		"timestamp": time.Now(),
		"services": fiber.Map{
			"database": func() string {
				if database.Ready(h.dbName) {
					return "healthy"
				}
				return "unhealthy"
			}(),
			"cache": func() string {
				if cache.Ready(h.cacheName) {
					return "healthy"
				}
				return "unhealthy"
			}(),
			"stellar": func() string {
				if h.checkStellar(ctx) {
					return "healthy"
				}
				return "unhealthy"
			}(),
		},
	})
}
