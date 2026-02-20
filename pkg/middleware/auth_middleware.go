package middleware

import (
	"time"

	"github.com/Shamba-Records-Limited/microvault/pkg/auth"
	"github.com/Shamba-Records-Limited/microvault/pkg/config"
	"github.com/gofiber/fiber/v2"
)

const AdminClaimsKey = "admin_claims"

// AuthMiddleware handles JWT authentication for protected routes
type AuthMiddleware struct {
	jwtService *auth.JWTService
	config     *config.StellarConfig
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
	return func(c *fiber.Ctx) error {
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

		// Verify the token is for the configured admin
		if claims.AdminPublicKey != m.config.AdminPublicKey {
			m.clearCookie(c)
			return fiber.NewError(fiber.StatusUnauthorized, "unauthorized: wrong admin")
		}

		// Refresh token if within refresh window
		if m.jwtService.ShouldRefresh(claims) {
			newToken, expiresAt, err := m.jwtService.GenerateToken(claims.AdminPublicKey)
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

// setCookie sets the admin_token cookie with secure flags
func (m *AuthMiddleware) setCookie(c *fiber.Ctx, token string, expiresAt time.Time) {
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
func (m *AuthMiddleware) clearCookie(c *fiber.Ctx) {
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
func GetAdminClaims(c *fiber.Ctx) *auth.Claims {
	claims, _ := c.Locals(AdminClaimsKey).(*auth.Claims)
	return claims
}
