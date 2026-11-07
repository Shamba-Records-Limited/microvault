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

func limiterApp() *fiber.App {
	app := fiber.New()
	app.Use(generalLimiter())
	app.Get("/api/v1/loans", func(c fiber.Ctx) error { return c.SendString("ok") })
	app.Post("/api/v1/mobile/ussd/:provider", FailedCallbackLimiter(), func(c fiber.Ctx) error {
		if c.Query("token") != "good" {
			return c.SendStatus(fiber.StatusUnauthorized)
		}
		return c.SendString("CON menu")
	})
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

func TestBusyGatewayIsNotLimited(t *testing.T) {
	app := limiterApp()
	for range limiterMax * 5 {
		require.Equal(t, http.StatusOK, statusOf(t, app, http.MethodPost, "/api/v1/mobile/ussd/africastalking?token=good"))
	}
	assert.Equal(t, http.StatusOK, statusOf(t, app, http.MethodGet, "/api/v1/loans"), "callbacks do not use the general allowance")
}

func TestGuessedCallbackTokenIsLimited(t *testing.T) {
	app := limiterApp()
	for range limiterMax {
		require.Equal(t, http.StatusUnauthorized, statusOf(t, app, http.MethodPost, "/api/v1/mobile/ussd/africastalking?token=wrong"))
	}
	assert.Equal(t, http.StatusTooManyRequests, statusOf(t, app, http.MethodPost, "/api/v1/mobile/ussd/africastalking?token=wrong"))
}
