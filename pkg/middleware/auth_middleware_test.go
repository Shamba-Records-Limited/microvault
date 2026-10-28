package middleware

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"

	"github.com/Shamba-Records-Limited/microvault/pkg/auth"
	"github.com/Shamba-Records-Limited/microvault/pkg/config"
)

const (
	adminKey    = "GADMIN"
	operatorKey = "GOPERATOR"
)

func newOperatorApp(t *testing.T, lookup OperatorLookup) (*fiber.App, *auth.JWTService) {
	t.Helper()
	jwtSvc := auth.NewJWTService(&config.AuthConfig{JWTSecret: "test-secret", JWTExpiration: time.Hour})
	m := NewAuthMiddleware(jwtSvc, &config.StellarConfig{AdminPublicKey: adminKey})
	if lookup != nil {
		m = m.WithOperators(lookup)
	}
	app := fiber.New()
	app.Get("/", m.RequireAuth(), func(c fiber.Ctx) error {
		return c.SendString(GetAdminClaims(c).Role)
	})
	return app, jwtSvc
}

func get(t *testing.T, app *fiber.App, jwtSvc *auth.JWTService, signer string) (status int, role string) {
	t.Helper()
	token, _, err := jwtSvc.GenerateTokenWithRole(signer, "")
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", http.NoBody)
	req.Header.Set("Cookie", "admin_token="+token)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	body := make([]byte, 64)
	n, _ := resp.Body.Read(body)
	return resp.StatusCode, string(body[:n])
}

func TestRequireAuth_OperatorsAreReadEachRequest(t *testing.T) {
	active := true
	app, jwtSvc := newOperatorApp(t, func(_ context.Context, address string) (string, bool, error) {
		if address != operatorKey {
			return "", false, nil
		}
		return "compliance", active, nil
	})

	if status, role := get(t, app, jwtSvc, adminKey); status != 200 || role != auth.RoleAdmin {
		t.Errorf("admin: %d %q", status, role)
	}
	if status, role := get(t, app, jwtSvc, operatorKey); status != 200 || role != "compliance" {
		t.Errorf("operator: %d %q", status, role)
	}
	active = false
	if status, _ := get(t, app, jwtSvc, operatorKey); status != fiber.StatusUnauthorized {
		t.Errorf("deactivated operator: %d, want 401 on the next request", status)
	}
	if status, _ := get(t, app, jwtSvc, "GSTRANGER"); status != fiber.StatusUnauthorized {
		t.Errorf("unknown signer: %d", status)
	}
}

func TestRequireAuth_WithoutOperatorsOnlyTheAdminPasses(t *testing.T) {
	app, jwtSvc := newOperatorApp(t, nil)
	if status, _ := get(t, app, jwtSvc, operatorKey); status != fiber.StatusUnauthorized {
		t.Errorf("operator without an operator lookup: %d", status)
	}
}

func TestRequireAuth_LookupFailureIsNotAnAuthFailure(t *testing.T) {
	app, jwtSvc := newOperatorApp(t, func(context.Context, string) (string, bool, error) {
		return "", false, errors.New("db down")
	})
	if status, _ := get(t, app, jwtSvc, operatorKey); status != fiber.StatusServiceUnavailable {
		t.Errorf("lookup failure: %d, want 503", status)
	}
}
