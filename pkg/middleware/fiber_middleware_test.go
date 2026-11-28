package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

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

func limiterApp() *fiber.App { return limiterAppWith(nil) }

func limiterAppWith(store LimiterStorage) *fiber.App {
	app := fiber.New()
	app.Use(generalLimiter(store), callbackLimiter(store))
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

// mapStorage is a shared in-memory fiber.Storage standing in for Redis.
type mapStorage struct {
	mu sync.Mutex
	m  map[string][]byte
}

func newMapStorage() *mapStorage { return &mapStorage{m: map[string][]byte{}} }

func (s *mapStorage) GetWithContext(_ context.Context, k string) ([]byte, error) { return s.Get(k) }

func (s *mapStorage) Get(k string) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.m[k], nil
}

func (s *mapStorage) SetWithContext(_ context.Context, k string, v []byte, exp time.Duration) error {
	return s.Set(k, v, exp)
}

func (s *mapStorage) Set(k string, v []byte, _ time.Duration) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.m[k] = append([]byte(nil), v...)
	return nil
}

func (s *mapStorage) DeleteWithContext(_ context.Context, k string) error { return s.Delete(k) }

func (s *mapStorage) Delete(k string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.m, k)
	return nil
}

func (s *mapStorage) ResetWithContext(context.Context) error { return s.Reset() }

func (s *mapStorage) Reset() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.m = map[string][]byte{}
	return nil
}

func (s *mapStorage) Close() error { return nil }

// With shared storage two replicas enforce one limit between them, not one
// each, and the two limiters keep separate counts.
func TestLimiter_SharedStorageAcrossReplicas(t *testing.T) {
	stores := map[string]*mapStorage{"general": newMapStorage(), "callback": newMapStorage()}
	shared := LimiterStorage(func(name string) fiber.Storage { return stores[name] })
	a, b := limiterAppWith(shared), limiterAppWith(shared)

	for i := range limiterMax {
		app := a
		if i%2 == 1 {
			app = b
		}
		require.Equal(t, fiber.StatusOK, statusOf(t, app, fiber.MethodGet, "/api/v1/loans"))
	}
	assert.Equal(t, fiber.StatusTooManyRequests, statusOf(t, a, fiber.MethodGet, "/api/v1/loans"))
	assert.Equal(t, fiber.StatusTooManyRequests, statusOf(t, b, fiber.MethodGet, "/api/v1/loans"))
	assert.Empty(t, stores["callback"].m, "the callback limiter counted nothing")
}
