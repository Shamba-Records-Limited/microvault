package auth

import (
	"fmt"
	"time"

	"github.com/Shamba-Records-Limited/Microvault/pkg/config"
	"github.com/golang-jwt/jwt/v5"
)

// Claims represents the JWT token payload containing user authentication information.
// It embeds the standard JWT registered claims and adds the admin's Stellar public key.
type Claims struct {
	AdminPublicKey string `json:"admin_pk"`
	jwt.RegisteredClaims
}

// JWTService provides JWT token generation and validation functionality.
// It uses HMAC-SHA256 signing and includes configurable expiration and refresh windows.
type JWTService struct {
	config *config.AuthConfig
}

// NewJWTService creates a new JWTService instance with the provided authentication configuration.
func NewJWTService(config *config.AuthConfig) *JWTService {
	return &JWTService{config: config}
}

// GenerateToken creates a new JWT token for the specified admin public key.
// The token is signed using HMAC-SHA256 and includes standard claims (issuer, subject, timestamps).
// Returns the signed token string, expiration time, and any error encountered during generation.
func (s *JWTService) GenerateToken(adminPublicKey string) (string, time.Time, error) {
	now := time.Now()
	expiresAt := now.Add(s.config.JWTExpiration)

	claims := &Claims{
		AdminPublicKey: adminPublicKey,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(expiresAt),
			IssuedAt:  jwt.NewNumericDate(now),
			NotBefore: jwt.NewNumericDate(now),
			Issuer:    "admin-auth",
			Subject:   adminPublicKey,
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenString, err := token.SignedString([]byte(s.config.JWTSecret))
	if err != nil {
		return "", time.Time{}, fmt.Errorf("failed to sign token: %w", err)
	}

	return tokenString, expiresAt, nil
}

// ValidateToken parses and validates a JWT token string.
// It verifies the signature using HMAC-SHA256 and ensures the token hasn't expired.
// Returns the extracted claims if valid, or an error if validation fails.
func (s *JWTService) ValidateToken(tokenString string) (*Claims, error) {
	token, err := jwt.ParseWithClaims(tokenString, &Claims{}, func(token *jwt.Token) (interface{}, error) {
		// Verify signing method is HMAC
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, ErrUnexpectedSigningMethod
		}
		return []byte(s.config.JWTSecret), nil
	})

	if err != nil {
		return nil, ErrInvalidToken
	}

	claims, ok := token.Claims.(*Claims)
	if !ok || !token.Valid {
		return nil, ErrInvalidTokenClaims
	}

	return claims, nil
}

// ShouldRefresh determines if a token should be refreshed based on its remaining lifetime.
// Returns true if the token will expire within the configured refresh window, allowing
// clients to obtain a new token before the current one expires.
func (s *JWTService) ShouldRefresh(claims *Claims) bool {
	if claims.ExpiresAt == nil {
		return false
	}
	return time.Until(claims.ExpiresAt.Time) < s.config.JWTRefreshWindow
}
