package controllers

import (
	"fmt"
	"log/slog"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/samber/lo"
	"gorm.io/datatypes"

	"github.com/Shamba-Records-Limited/microvault/pkg/config"
	"github.com/Shamba-Records-Limited/microvault/pkg/models"
	"github.com/Shamba-Records-Limited/microvault/pkg/payment/airtel"
	"github.com/Shamba-Records-Limited/microvault/pkg/repository"
)

// AirtelCallbackController handles inbound Airtel Money notifications.
//
// It differs from its Daraja sibling in two ways, both of which come from the
// rail rather than from taste.
//
// Airtel can sign a callback. When callback authentication is enabled, an
// HmacSHA256 arrives alongside the body, and a body carrying a hash that does
// not verify under our key is rejected outright. That is a stronger position
// than Daraja allows — but a verified hash still only proves Airtel sent it,
// not that the payment settled, so nothing here credits anything.
//
// A transaction can report twice. Airtel documents its callback as carrying
// intermediate or final status with nothing in the payload to tell them
// apart, so every callback is recorded — including a TF, which its Daraja
// counterpart deliberately drops — and the repository folds the second
// notification into the first row rather than treating it as a duplicate.
//
// Loan attribution does not happen here. The callback carries no reference,
// only the transaction id we minted, so the row is staged unattributed and
// the credit-side poller — which knows which loan that id belongs to — is
// what ties the two together.
type AirtelCallbackController struct {
	repo      repository.AirtelTransactionRepository
	config    config.AirtelConfig
	serverEnv string
	now       func() time.Time
	logger    *slog.Logger
}

// NewAirtelCallbackController wires the controller.
func NewAirtelCallbackController(repo repository.AirtelTransactionRepository, cfg config.AirtelConfig, serverEnv string) *AirtelCallbackController {
	return &AirtelCallbackController{
		repo:      repo,
		config:    cfg,
		serverEnv: serverEnv,
		now:       time.Now,
		logger:    slog.Default().With("component", "airtel_callback"),
	}
}

// allowedCIDR checks the source address against the configured egress ranges.
//
// Airtel does not publish its egress list, so an empty allowlist is the
// expected state in development and a misconfiguration in production. The
// rule matches the Daraja controller's, including failing closed rather than
// defaulting permissive.
func (ctrl *AirtelCallbackController) allowedCIDR(c *fiber.Ctx) error {
	if len(ctrl.config.CallbackAllowedCIDRs) == 0 {
		if ctrl.serverEnv == "production" {
			ctrl.logger.Warn("rejecting airtel callback: no CIDR allowlist configured in production",
				"path", c.Path())
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
	ctrl.logger.Warn("rejecting airtel callback: source not in the allowlist",
		"path", c.Path(), "client_ip", clientIP, "x_forwarded_for", c.Get(fiber.HeaderXForwardedFor),
		"allowed_cidrs", ctrl.config.CallbackAllowedCIDRs)
	return fiber.NewError(fiber.StatusForbidden, "source not permitted")
}

// CollectionCallback records an Airtel collection notification.
// @Description Record an Airtel Money collection notification as an observation for the poller to confirm
// @Summary Record an Airtel collection callback
// @Tags Airtel
// @Accept json
// @Produce json
// @Param slug path string true "Callback slug"
// @Param body body airtel.Callback true "Collection status notification"
// @Success 200 {string} string "Recorded"
// @Failure 400 {object} fiber.Error "Undecodable callback"
// @Failure 403 {object} fiber.Error "Source not permitted, or the hash did not verify"
// @Failure 500 {object} fiber.Error "Failed to record the observation"
// @Router /api/v1/callbacks/airtel/{slug}/collection [post]
func (ctrl *AirtelCallbackController) CollectionCallback(c *fiber.Ctx) error {
	if err := ctrl.allowedCIDR(c); err != nil {
		return err
	}

	body := c.Body()
	callback, err := airtel.ParseCallback(body)
	if err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "could not decode the callback")
	}

	verified, variant, err := ctrl.verifyHash(body, callback)
	if err != nil {
		return err
	}

	status := callback.TransactionStatus()
	tx := models.AirtelTransaction{
		PartnerTxnID: callback.Transaction.ID,
		Source:       models.AirtelSourceCallback,
		StatusCode:   string(status),
		HashVerified: verified,
		HashVariant:  variant,
		TransTime:    ctrl.now(),
		RawPayload:   datatypes.JSON(body),

		// Every callback is staged for an enquiry, including a TS one. A
		// callback is never terminal here: only the enquiry is, and the
		// three-minute floor is Airtel's own.
		NextPollAt: ptrTime(ctrl.now().Add(ctrl.enquiryDelay())),
	}
	if callback.Transaction.AirtelMoneyID != "" {
		tx.AirtelMoneyID = lo.ToPtr(callback.Transaction.AirtelMoneyID)
	}

	if err := ctrl.repo.RecordCallback(c.UserContext(), &tx); err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "could not record the observation")
	}
	return c.SendStatus(fiber.StatusOK)
}

// verifyHash checks the callback's signature when one is configured.
//
// A callback carrying a hash that does not verify is rejected: it is either a
// forgery or our key is wrong, and neither should land a row. A callback with
// no hash is recorded unverified, because callback authentication is
// optional at Airtel's end and a deployment that has not enabled it is not
// misconfigured.
func (ctrl *AirtelCallbackController) verifyHash(body []byte, callback *airtel.Callback) (verified bool, variant *string, err error) {
	if ctrl.config.CallbackHMACKey == "" || callback.Hash == "" {
		return false, nil, nil
	}

	result, verifyErr := airtel.VerifyCallbackHash(body, ctrl.config.CallbackHMACKey)
	if verifyErr != nil {
		ctrl.logger.Warn("could not verify an airtel callback hash",
			"transaction_id", callback.Transaction.ID, "error", verifyErr)
		return false, nil, fiber.NewError(fiber.StatusForbidden, "callback hash could not be verified")
	}
	if !result.Verified {
		ctrl.logger.Warn("rejecting airtel callback: the hash did not verify under any candidate rendering",
			"transaction_id", callback.Transaction.ID)
		return false, nil, fiber.NewError(fiber.StatusForbidden, "callback hash did not verify")
	}

	// Which rendering matched is the answer Airtel's documentation does not
	// give. Recording it per row is how the question gets settled from live
	// traffic instead of inferred.
	return true, lo.ToPtr(string(result.Variant)), nil
}

// enquiryDelay is the wait before the first enquiry, never shorter than
// Airtel's documented floor in production — config validation enforces that,
// and this guards the zero value.
func (ctrl *AirtelCallbackController) enquiryDelay() time.Duration {
	if ctrl.config.EnquiryDelay <= 0 {
		return config.EnquiryDelayFloor
	}
	return ctrl.config.EnquiryDelay
}

// Register mounts the callback route. The slug is the unguessable path
// segment; the route hangs under it so the path cannot be enumerated.
func (ctrl *AirtelCallbackController) Register(app fiber.Router) {
	group := app.Group(fmt.Sprintf("/callbacks/airtel/%s", ctrl.config.CallbackSlug))
	group.Post("/collection", ctrl.CollectionCallback)
}
