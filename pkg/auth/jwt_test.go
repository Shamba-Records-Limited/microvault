package auth

import (
	"errors"
	"testing"
	"time"

	"github.com/Shamba-Records-Limited/microvault/pkg/config"
	"github.com/golang-jwt/jwt/v5"
)

func testJWT(exp, refresh time.Duration) *JWTService {
	return NewJWTService(&config.AuthConfig{
		JWTSecret:        "test-secret-key",
		JWTExpiration:    exp,
		JWTRefreshWindow: refresh,
	})
}

func TestJWT_RoundTrip(t *testing.T) {
	s := testJWT(time.Hour, 10*time.Minute)
	pk := "GABC...ADMIN"

	tok, exp, err := s.GenerateToken(pk)
	if err != nil {
		t.Fatalf("GenerateToken: %v", err)
	}
	if !exp.After(time.Now()) {
		t.Error("expiry should be in the future")
	}

	claims, err := s.ValidateToken(tok)
	if err != nil {
		t.Fatalf("ValidateToken: %v", err)
	}
	if claims.AdminPublicKey != pk || claims.Subject != pk {
		t.Errorf("claims mismatch: %+v", claims)
	}
}

func TestJWT_WrongSecret(t *testing.T) {
	tok, _, _ := testJWT(time.Hour, time.Minute).GenerateToken("pk")
	other := NewJWTService(&config.AuthConfig{JWTSecret: "different-secret", JWTExpiration: time.Hour})
	if _, err := other.ValidateToken(tok); !errors.Is(err, ErrInvalidToken) {
		t.Errorf("err = %v, want ErrInvalidToken", err)
	}
}

func TestJWT_Garbage(t *testing.T) {
	s := testJWT(time.Hour, time.Minute)
	if _, err := s.ValidateToken("not.a.jwt"); !errors.Is(err, ErrInvalidToken) {
		t.Errorf("err = %v, want ErrInvalidToken", err)
	}
}

func TestJWT_Expired(t *testing.T) {
	// Negative expiration => token is already expired at issue.
	s := testJWT(-time.Hour, time.Minute)
	tok, _, _ := s.GenerateToken("pk")
	if _, err := s.ValidateToken(tok); !errors.Is(err, ErrInvalidToken) {
		t.Errorf("expired token err = %v, want ErrInvalidToken", err)
	}
}

func TestShouldRefresh(t *testing.T) {
	s := testJWT(time.Hour, 10*time.Minute)

	near := &Claims{RegisteredClaims: jwt.RegisteredClaims{ExpiresAt: jwt.NewNumericDate(time.Now().Add(5 * time.Minute))}}
	if !s.ShouldRefresh(near) {
		t.Error("token expiring within window should refresh")
	}
	far := &Claims{RegisteredClaims: jwt.RegisteredClaims{ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour))}}
	if s.ShouldRefresh(far) {
		t.Error("token far from expiry should not refresh")
	}
	if s.ShouldRefresh(&Claims{}) {
		t.Error("nil ExpiresAt should not refresh")
	}
}
