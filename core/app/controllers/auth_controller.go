package controllers

import (
	"errors"

	"github.com/Shamba-Records-Limited/Microvault/internal/core/services/auth"
	"github.com/Shamba-Records-Limited/Microvault/internal/core/services/validation"
	"github.com/gofiber/fiber/v2"
)

// AuthController handles authentication endpoints including challenge generation and verification.
type AuthController struct {
	challengeService  auth.ChallengeService
	jwtService        *auth.JWTService
	adminPublicKey    string
	validationService *validation.ValidatorService
}

// NewAuthController creates a new AuthController with the provided services.
func NewAuthController(challengeService auth.ChallengeService, jwtService *auth.JWTService, adminPublicKey string, validationService *validation.ValidatorService) *AuthController {
	return &AuthController{
		challengeService:  challengeService,
		jwtService:        jwtService,
		adminPublicKey:    adminPublicKey,
		validationService: validationService,
	}
}

// ChallengeResponse represents the authentication challenge response.
type ChallengeResponse struct {
	ChallengeID string `json:"challenge_id"`
	Transaction string `json:"transaction"`
	ExpiresAt   int64  `json:"expires_at"`
}

// GetChallenge generates a new authentication challenge.
// @Description Generate a Stellar transaction challenge for authentication
// @Summary Get Authentication Challenge
// @Tags Authentication
// @Accept json
// @Produce json
// @Success 200 {object} ChallengeResponse "Challenge generated successfully"
// @Failure 500 {object} middleware.Response "Failed to generate challenge"
// @Router /api/v1/auth/challenge [get]
func (ctrl *AuthController) GetChallenge(c *fiber.Ctx) error {
	challenge, err := ctrl.challengeService.GenerateChallenge()
	if err != nil {
		c.Locals("error", err.Error())
		return fiber.NewError(fiber.StatusInternalServerError, "failed to generate challenge")
	}

	response := ChallengeResponse{
		ChallengeID: challenge.ID,
		Transaction: challenge.Transaction,
		ExpiresAt:   challenge.ExpiresAt.Unix(),
	}

	c.Locals("data", response)
	c.Status(fiber.StatusOK)
	return nil
}

// VerifyRequest represents the challenge verification request body.
type VerifyRequest struct {
	ChallengeID       string `json:"challenge_id" validate:"required,base64url"`
	SignedTransaction string `json:"signed_transaction" validate:"required,stellar_xdr"`
}

// VerifyResponse represents the challenge verification response.
type VerifyResponse struct {
	Token     string `json:"token"`
	ExpiresAt int64  `json:"expires_at"`
}

// VerifyChallenge verifies a signed challenge and returns a JWT token.
// @Description Verify a signed Stellar transaction challenge and receive a JWT token
// @Summary Verify Challenge
// @Tags Authentication
// @Accept json
// @Produce json
// @Param body body VerifyRequest true "Signed challenge verification"
// @Success 200 {object} VerifyResponse "Challenge verified successfully"
// @Failure 400 {object} middleware.Response "Invalid request body or transaction"
// @Failure 401 {object} middleware.Response "Challenge verification failed"
// @Failure 404 {object} middleware.Response "Challenge not found"
// @Failure 500 {object} middleware.Response "Failed to generate token"
// @Router /api/v1/auth/verify [post]
func (ctrl *AuthController) VerifyChallenge(c *fiber.Ctx) error {
	// Parse request body
	requestBody := new(VerifyRequest)
	if err := c.BodyParser(&requestBody); err != nil {
		c.Locals("error", "invalid request body")
		return fiber.NewError(fiber.StatusBadRequest, "invalid request body")
	}

	// Validate request body using validation service
	fieldErrors, err := ctrl.validationService.Validate(requestBody)
	if err != nil {
		c.Locals("error", err.Error())
		return fiber.NewError(fiber.StatusInternalServerError, "validation failed")
	}

	if fieldErrors != nil {
		c.Locals("error", fieldErrors)
		return fiber.NewError(fiber.StatusBadRequest, "validation failed")
	}

	// Verify the signed challenge
	if err := ctrl.challengeService.VerifySignedChallenge(requestBody.ChallengeID, requestBody.SignedTransaction); err != nil {
		// Map service errors to HTTP status codes
		switch {
		case errors.Is(err, auth.ErrChallengeNotFound):
			c.Locals("error", "challenge not found")
			return fiber.NewError(fiber.StatusNotFound, "challenge not found")
		case errors.Is(err, auth.ErrChallengeExpired):
			c.Locals("error", "challenge expired")
			return fiber.NewError(fiber.StatusUnauthorized, "challenge expired")
		case errors.Is(err, auth.ErrInvalidTransaction):
			c.Locals("error", "invalid transaction")
			return fiber.NewError(fiber.StatusBadRequest, "invalid transaction")
		case errors.Is(err, auth.ErrTransactionMismatch):
			c.Locals("error", "transaction does not match challenge")
			return fiber.NewError(fiber.StatusUnauthorized, "transaction does not match challenge")
		case errors.Is(err, auth.ErrInvalidSignature):
			c.Locals("error", "invalid or missing signature")
			return fiber.NewError(fiber.StatusUnauthorized, "invalid or missing signature")
		default:
			c.Locals("error", err.Error())
			return fiber.NewError(fiber.StatusInternalServerError, "verification failed")
		}
	}

	// Generate JWT token
	token, expiresAt, err := ctrl.jwtService.GenerateToken(ctrl.adminPublicKey)
	if err != nil {
		c.Locals("error", err.Error())
		return fiber.NewError(fiber.StatusInternalServerError, "failed to generate token")
	}

	// Set JWT token in HTTP-only cookie for security
	c.Cookie(&fiber.Cookie{
		Name:     "auth_token",
		Value:    token,
		Expires:  expiresAt,
		HTTPOnly: true,
		Secure:   false, // Set to true in production with HTTPS
		SameSite: "Lax",
		Path:     "/",
	})

	response := VerifyResponse{
		Token:     token,
		ExpiresAt: expiresAt.Unix(),
	}

	c.Locals("data", response)
	c.Status(fiber.StatusOK)
	return nil
}
