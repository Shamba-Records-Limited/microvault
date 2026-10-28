package middleware

import (
	"context"
	"time"

	"github.com/gofiber/fiber/v3"

	"github.com/Shamba-Records-Limited/microvault/pkg/auth"
	"github.com/Shamba-Records-Limited/microvault/pkg/config"
)

const AdminClaimsKey = "admin_claims"

// AuthMiddleware handles JWT authentication for protected routes
type AuthMiddleware struct {
	jwtService *auth.JWTService
	config     *config.StellarConfig
	operators  OperatorLookup
}

// OperatorLookup returns the role of an active operator, and false for an
// address that is not one.
type OperatorLookup func(ctx context.Context, address string) (role string, active bool, err error)

// WithOperators lets active operators through as well as the configured
// admin. Each request re-reads the operator, so deactivation is immediate.
func (m *AuthMiddleware) WithOperators(lookup OperatorLookup) *AuthMiddleware {
	m.operators = lookup
	return m
}

// NewAuthMiddleware creates a new auth middleware instance
func NewAuthMiddleware(jwtService *auth.JWTService, config *config.StellarConfig) *AuthMiddleware {
	return &AuthMiddleware{
		jwtService: jwtService,
		config:     config,
	}
}

// RequireAuth is a middleware that validates JWT tokens from cookies
func (m *AuthMiddleware) RequireAuth() fiber.Handler {
	return func(c fiber.Ctx) error {
		// Get token from cookie
		token := c.Cookies("admin_token")
		if token == "" {
			return fiber.NewError(fiber.StatusUnauthorized, "unauthorized: no token")
		}

		// Validate the token
		claims, err := m.jwtService.ValidateToken(token)
		if err != nil {
			// Clear invalid cookie
			m.clearCookie(c)
			return fiber.NewError(fiber.StatusUnauthorized, "unauthorized: invalid token")
		}

		// Verify the token is for the configured admin or an active operator
		role, status := m.authorize(c.Context(), claims.AdminPublicKey)
		if status != 0 {
			m.clearCookie(c)
			return fiber.NewError(status, "unauthorized: not an admin or active operator")
		}
		claims.Role = role

		// Refresh token if within refresh window
		if m.jwtService.ShouldRefresh(claims) {
			newToken, expiresAt, err := m.jwtService.GenerateTokenWithRole(claims.AdminPublicKey, role)
			if err == nil {
				m.setCookie(c, newToken, expiresAt)
			}
		}

		// Store claims in context for downstream handlers
		c.Locals(AdminClaimsKey, claims)

		// Continue to next handler
		return c.Next()
	}
}

// authorize returns the signer's current role, or a non-zero HTTP status.
func (m *AuthMiddleware) authorize(ctx context.Context, address string) (string, int) {
	if address == m.config.AdminPublicKey {
		return auth.RoleAdmin, 0
	}
	if m.operators == nil {
		return "", fiber.StatusUnauthorized
	}
	role, active, err := m.operators(ctx, address)
	switch {
	case err != nil:
		return "", fiber.StatusServiceUnavailable
	case !active:
		return "", fiber.StatusUnauthorized
	}
	return role, 0
}

// setCookie sets the admin_token cookie with secure flags
func (m *AuthMiddleware) setCookie(c fiber.Ctx, token string, expiresAt time.Time) {
	c.Cookie(&fiber.Cookie{
		Name:     "admin_token",
		Value:    token,
		Path:     "/",
		Expires:  expiresAt,
		HTTPOnly: true,     // Not accessible via JavaScript
		Secure:   true,     // HTTPS only
		SameSite: "Strict", // CSRF protection
	})
}

// clearCookie removes the admin_token cookie
func (m *AuthMiddleware) clearCookie(c fiber.Ctx) {
	c.Cookie(&fiber.Cookie{
		Name:     "admin_token",
		Value:    "",
		Path:     "/",
		Expires:  time.Unix(0, 0),
		HTTPOnly: true,
		Secure:   true,
		SameSite: "Strict",
	})
}

// GetAdminClaims retrieves admin claims from the Fiber context
func GetAdminClaims(c fiber.Ctx) *auth.Claims {
	claims, _ := c.Locals(AdminClaimsKey).(*auth.Claims)
	return claims
}
