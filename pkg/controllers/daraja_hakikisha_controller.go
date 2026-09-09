package controllers

import (
	"context"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/golang-jwt/jwt/v5"

	"github.com/Shamba-Records-Limited/microvault/pkg/config"
	"github.com/Shamba-Records-Limited/microvault/pkg/loanref"
	"github.com/Shamba-Records-Limited/microvault/pkg/payment/mpesa"
	"github.com/Shamba-Records-Limited/microvault/pkg/repository"
)

// DarajaHakikishaController is the OAuth issuer and resolver C2B Hakikisha
// calls against. Every other Daraja-facing route has Safaricom pushing us a
// notification we authenticate by IP and an unguessable path; this one
// inverts it — Safaricom authenticates to us with client_credentials, the way
// we authenticate to every other Daraja API. See pkg/payment/mpesa/hakikisha.go.
//
// Deliberately not built on pkg/auth.JWTService: that issuer is hard-coupled
// to the admin claim shape, and the two token families must share nothing —
// a token minted for one must never verify against the other.
type DarajaHakikishaController struct {
	resolver  mpesa.AccountResolver
	config    config.MpesaConfig
	serverEnv string
	now       func() time.Time
}

// NewDarajaHakikishaController wires the controller.
func NewDarajaHakikishaController(repo repository.MpesaTransactionRepository, cfg config.MpesaConfig, serverEnv string) *DarajaHakikishaController {
	return &DarajaHakikishaController{
		resolver:  hakikishaResolver{repo: repo},
		config:    cfg,
		serverEnv: serverEnv,
		now:       time.Now,
	}
}

// hakikishaResolver wraps GetLoanIDByReference to satisfy
// mpesa.AccountResolver. AccountName is built by the caller from the
// request's own account number, never from the resolved loan ID — see
// Resolve — so this type only ever answers whether one exists.
type hakikishaResolver struct {
	repo repository.MpesaTransactionRepository
}

func (r hakikishaResolver) ResolveAccount(accountNumber string) (accountName string, found bool, err error) {
	// Hakikisha sits in front of a customer holding a handset; bounded well
	// inside whatever timeout Safaricom applies, matching the discipline
	// C2BValidation already uses for the same reason.
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	loanID, err := r.repo.GetLoanIDByReference(ctx, accountNumber)
	if err != nil {
		return "", false, err
	}
	if loanID == "" {
		return "", false, nil
	}
	// The name is unused by design — Resolve builds AccountName itself from
	// the request's own account number, never from anything looked up here.
	return "", true, nil
}

// allowedCIDR mirrors DarajaCallbackController's. Safaricom is the caller on
// this route too, even though the auth model differs from every other one.
func (ctrl *DarajaHakikishaController) allowedCIDR(c *fiber.Ctx) error {
	if len(ctrl.config.CallbackAllowedCIDRs) == 0 {
		if ctrl.serverEnv == "production" {
			return fiber.NewError(fiber.StatusForbidden, "callback allowlist not configured")
		}
		return nil
	}
	clientIP := c.IP()
	for _, cidr := range ctrl.config.CallbackAllowedCIDRs {
		if cidrMatch(clientIP, cidr) {
			return nil
		}
	}
	return fiber.NewError(fiber.StatusForbidden, "source not permitted")
}

// Token issues a short-lived bearer token to a client_credentials caller
// authenticated with HTTP Basic.
// @Description Issue a bearer token for the Hakikisha resolve endpoint
// @Summary Hakikisha OAuth token
// @Tags Daraja
// @Produce json
// @Param slug path string true "Callback slug"
// @Param grant_type query string true "Must be client_credentials"
// @Success 200 {object} map[string]any "access_token, expires_in"
// @Failure 401 {object} map[string]string "errorCode, errorMessage"
// @Failure 403 {object} fiber.Error "Source not permitted"
// @Router /api/v1/callbacks/daraja/{slug}/hakikisha/oauth/token [post]
func (ctrl *DarajaHakikishaController) Token(c *fiber.Ctx) error {
	if err := ctrl.allowedCIDR(c); err != nil {
		return err
	}
	if c.Query("grant_type") != "client_credentials" {
		return hakikishaAuthError(c, "unsupported_grant_type", "grant_type must be client_credentials")
	}

	username, password, ok := parseBasicAuth(c)
	if !ok || !constantTimeEqual(username, ctrl.config.HakikishaUsername) || !constantTimeEqual(password, ctrl.config.HakikishaPassword) {
		return hakikishaAuthError(c, "invalid_client", "invalid credentials")
	}

	const expiresIn = 5 * time.Minute
	claims := jwt.RegisteredClaims{
		Issuer:    "microvault-hakikisha",
		Subject:   ctrl.config.HakikishaUsername,
		IssuedAt:  jwt.NewNumericDate(ctrl.now()),
		ExpiresAt: jwt.NewNumericDate(ctrl.now().Add(expiresIn)),
	}
	signed, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(ctrl.config.HakikishaSigningKey))
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "could not issue a token")
	}

	return c.JSON(fiber.Map{
		"access_token": signed,
		"token_type":   "Bearer",
		"expires_in":   int(expiresIn.Seconds()),
	})
}

// Resolve answers the account-name lookup, bearer-checked against the token
// Token issued.
// @Description Resolve an account number to a display name for the payer's confirmation screen
// @Summary Hakikisha account resolve
// @Tags Daraja
// @Accept json
// @Produce json
// @Param slug path string true "Callback slug"
// @Param body body mpesa.HakikishaRequest true "Resolve request"
// @Success 200 {object} mpesa.HakikishaResponse "Found or not-found answer"
// @Failure 401 {object} map[string]string "errorCode, errorMessage"
// @Failure 403 {object} fiber.Error "Source not permitted"
// @Failure 422 {object} fiber.Error "Undecodable request"
// @Router /api/v1/callbacks/daraja/{slug}/hakikisha/resolve [post]
func (ctrl *DarajaHakikishaController) Resolve(c *fiber.Ctx) error {
	if err := ctrl.allowedCIDR(c); err != nil {
		return err
	}
	if err := ctrl.checkBearer(c); err != nil {
		return err
	}

	req, err := mpesa.ParseHakikishaRequest(c.Body())
	if err != nil {
		return fiber.NewError(fiber.StatusUnprocessableEntity, "could not decode the request")
	}

	// Reference shape first, before any lookup — the same discipline
	// C2BValidation applies, and for the same reason: a malformed reference
	// cannot be ours regardless of what a database round trip would say.
	if !loanref.Validate(ctrl.config.ReferencePrefix, req.AccountNumber) {
		return c.JSON(mpesa.AccountNotFound(req.AccountNumber))
	}

	_, found, err := ctrl.resolver.ResolveAccount(req.AccountNumber)
	if err != nil || !found {
		return c.JSON(mpesa.AccountNotFound(req.AccountNumber))
	}

	// The account name is built from the request's own, already-validated
	// account number — never from anything the resolver looked up — so this
	// can never echo an internal identifier or a borrower's name. See
	// mpesa.HakikishaResponse's doc comment.
	return c.JSON(mpesa.AccountFound(req.AccountNumber, "Microvault Loan "+req.AccountNumber))
}

// checkBearer validates the token minted by Token.
func (ctrl *DarajaHakikishaController) checkBearer(c *fiber.Ctx) error {
	header := c.Get("Authorization")
	const prefix = "Bearer "
	if !strings.HasPrefix(header, prefix) {
		return hakikishaAuthError(c, "invalid_token", "missing bearer token")
	}
	_, err := jwt.Parse(strings.TrimPrefix(header, prefix), func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method %v", t.Header["alg"])
		}
		return []byte(ctrl.config.HakikishaSigningKey), nil
	})
	if err != nil {
		return hakikishaAuthError(c, "invalid_token", "token is invalid or expired")
	}
	return nil
}

// Register mounts the Hakikisha routes under the same slug every other
// Daraja-facing route uses.
func (ctrl *DarajaHakikishaController) Register(app fiber.Router) {
	group := app.Group(fmt.Sprintf("/callbacks/daraja/%s/hakikisha", ctrl.config.CallbackSlug))
	group.Post("/oauth/token", ctrl.Token)
	group.Post("/resolve", ctrl.Resolve)
}

// parseBasicAuth reads the client_credentials pair off the Authorization
// header. Deliberately not net/http's ParseBasicAuth, which the fiber
// context does not expose directly.
func parseBasicAuth(c *fiber.Ctx) (username, password string, ok bool) {
	header := c.Get("Authorization")
	const prefix = "Basic "
	if !strings.HasPrefix(header, prefix) {
		return "", "", false
	}
	decoded, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(header, prefix))
	if err != nil {
		return "", "", false
	}
	user, pass, found := strings.Cut(string(decoded), ":")
	if !found {
		return "", "", false
	}
	return user, pass, true
}

// constantTimeEqual avoids leaking credential length/content through timing.
func constantTimeEqual(a, b string) bool {
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}

// hakikishaAuthError writes Daraja's documented issuer error shape.
func hakikishaAuthError(c *fiber.Ctx, code, message string) error {
	return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
		"errorCode":    code,
		"errorMessage": message,
	})
}
