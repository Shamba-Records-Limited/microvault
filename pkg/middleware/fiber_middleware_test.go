package middleware

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var callbackRoutes = []string{
	"/api/v1/mobile/ussd/africastalking",
	"/api/v1/webhooks/yellowcard",
	"/api/v1/webhooks/didit",
	"/api/v1/webhooks/merkle",
	"/api/v1/callbacks/daraja/slug/c2b/confirmation",
	"/api/v1/callbacks/airtel/slug/collection",
}

const deliveryReportRoute = "/api/v1/mobile/sms/africastalking/delivery"

func limiterApp() *fiber.App {
	app := fiber.New()
	app.Use(generalLimiter(), callbackLimiter())
	app.Get("/api/v1/loans", func(c fiber.Ctx) error { return c.SendString("ok") })
	app.Post(deliveryReportRoute, func(c fiber.Ctx) error { return c.SendString("ok") })
	for _, route := range callbackRoutes {
		app.Post(route, func(c fiber.Ctx) error {
			if c.Query("token") != "good" {
				return c.SendStatus(fiber.StatusUnauthorized)
			}
			return c.SendString("ok")
		})
	}
	return app
}

func statusOf(t *testing.T, app *fiber.App, method, target string) int {
	t.Helper()
	resp, err := app.Test(httptest.NewRequestWithContext(t.Context(), method, target, strings.NewReader("")))
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())
	return resp.StatusCode
}

func TestGeneralLimiterLimitsOtherRoutes(t *testing.T) {
	app := limiterApp()
	for range limiterMax {
		require.Equal(t, http.StatusOK, statusOf(t, app, http.MethodGet, "/api/v1/loans"))
	}
	assert.Equal(t, http.StatusTooManyRequests, statusOf(t, app, http.MethodGet, "/api/v1/loans"))
}

func TestFailedNonCallbacksUseTheGeneralAllowance(t *testing.T) {
	app := limiterApp()
	for range limiterMax {
		require.Equal(t, http.StatusNotFound, statusOf(t, app, http.MethodGet, "/api/v1/missing"))
	}
	assert.Equal(t, http.StatusTooManyRequests, statusOf(t, app, http.MethodGet, "/api/v1/missing"))
}

func TestUnauthenticatedDeliveryReportsUseTheGeneralAllowance(t *testing.T) {
	app := limiterApp()
	for range limiterMax {
		require.Equal(t, http.StatusOK, statusOf(t, app, http.MethodPost, deliveryReportRoute))
	}
	assert.Equal(t, http.StatusTooManyRequests, statusOf(t, app, http.MethodPost, deliveryReportRoute))
}

func TestBusyGatewayIsNotLimited(t *testing.T) {
	for _, route := range callbackRoutes {
		t.Run(route, func(t *testing.T) {
			app := limiterApp()
			for range limiterMax * 5 {
				require.Equal(t, http.StatusOK, statusOf(t, app, http.MethodPost, route+"?token=good"))
			}
			assert.Equal(t, http.StatusOK, statusOf(t, app, http.MethodGet, "/api/v1/loans"), "callbacks do not use the general allowance")
		})
	}
}

func TestGuessedCallbackIsLimited(t *testing.T) {
	for _, route := range callbackRoutes {
		t.Run(route, func(t *testing.T) {
			app := limiterApp()
			for range limiterMax {
				require.Equal(t, http.StatusUnauthorized, statusOf(t, app, http.MethodPost, route+"?token=wrong"))
			}
			assert.Equal(t, http.StatusTooManyRequests, statusOf(t, app, http.MethodPost, route+"?token=wrong"))
		})
	}
}
