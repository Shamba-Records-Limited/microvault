package controllers

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/netip"
	"time"

	"gorm.io/datatypes"

	"github.com/gofiber/fiber/v2"

	"github.com/Shamba-Records-Limited/microvault/pkg/config"
	"github.com/Shamba-Records-Limited/microvault/pkg/loanref"
	"github.com/Shamba-Records-Limited/microvault/pkg/models"
	"github.com/Shamba-Records-Limited/microvault/pkg/payment/mpesa"
	"github.com/Shamba-Records-Limited/microvault/pkg/repository"
)

// DarajaCallbackController handles the inbound M-Pesa notifications.
//
// Daraja signs nothing. A callback is never evidence — it is an observation,
// recorded in the staging table, and credited only after the poller confirms it
// independently. The controller's whole job is to land the bytes honestly, in
// the right shape, fast.
type DarajaCallbackController struct {
	repo        repository.MpesaTransactionRepository
	config      config.MpesaConfig
	serverEnv   string
	resolveLoan LoanReferenceResolver
	now         func() time.Time

	// balances, the floors and logger are set by EnableBalanceTracking.
	// balances nil (the default) leaves a balance result recorded generically
	// like every other async result, just not parsed.
	balances             repository.MpesaBalanceRepository
	collectionFloorKES   int64
	disbursementFloorKES int64
	logger               *slog.Logger
}

// LoanReferenceResolver resolves a loan reference to a loan ID, or "" when the
// reference does not resolve. It is injected because the loan lookup lives in
// the credit module.
type LoanReferenceResolver func(ctx context.Context, reference string) (string, error)

// NewDarajaCallbackController wires the controller.
func NewDarajaCallbackController(repo repository.MpesaTransactionRepository, cfg config.MpesaConfig, serverEnv string, resolve LoanReferenceResolver) *DarajaCallbackController {
	return &DarajaCallbackController{
		repo:        repo,
		config:      cfg,
		serverEnv:   serverEnv,
		resolveLoan: resolve,
		now:         time.Now,
	}
}

// EnableBalanceTracking wires persistence and floor alerts for the Account
// Balance async result. Optional and additive — call it after construction
// when a BalancePoller is running; without it the constructor's behaviour is
// unchanged.
func (ctrl *DarajaCallbackController) EnableBalanceTracking(balances repository.MpesaBalanceRepository, collectionFloorKES, disbursementFloorKES int64, logger *slog.Logger) {
	ctrl.balances = balances
	ctrl.collectionFloorKES = collectionFloorKES
	ctrl.disbursementFloorKES = disbursementFloorKES
	if logger == nil {
		logger = slog.Default()
	}
	ctrl.logger = logger.With("component", "daraja_balance_tracking")
}

// allowedCIDR checks the source address against the configured egress ranges.
// An empty list is log-only — safaricom egress is a configuration detail we do
// not fail on in development, but must fail on in production.
func (ctrl *DarajaCallbackController) allowedCIDR(c *fiber.Ctx) error {
	if len(ctrl.config.CallbackAllowedCIDRs) == 0 {
		if ctrl.serverEnv == "production" {
			// Fail closed: a production callback with no allowlist configured is
			// a misconfiguration, not a permissive default.
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

// cidrMatch reports whether ip is within cidr.
func cidrMatch(ip, cidr string) bool {
	prefix, err := netip.ParsePrefix(cidr)
	if err != nil {
		return false
	}
	addr, err := netip.ParseAddr(ip)
	if err != nil {
		return false
	}
	addr = addr.Unmap()
	return prefix.Contains(addr)
}

// STKCallback receives the M-Pesa Express result.
// @Description M-Pesa Express payment callback
// @Summary Record an STK push result
// @Tags Daraja
// @Accept json
// @Produce json
// @Param slug path string true "Callback slug"
// @Param body body mpesa.ExpressCallbackEnvelope true "Express result delivery"
// @Success 200 {string} string "Recorded or dropped"
// @Failure 400 {object} fiber.Error "Undecodable callback"
// @Failure 403 {object} fiber.Error "Source not permitted"
// @Failure 500 {object} fiber.Error "Failed to record the observation"
// @Router /api/v1/callbacks/daraja/{slug}/stk/result [post]
func (ctrl *DarajaCallbackController) STKCallback(c *fiber.Ctx) error {
	if err := ctrl.allowedCIDR(c); err != nil {
		return err
	}

	callback, err := mpesa.ParseExpressCallback(c.Body())
	if err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "could not decode the callback")
	}

	// A failed prompt carries no receipt and moved no money. The staging
	// table is a payment log; a cancelled or declined prompt is not a
	// payment. Acknowledge and drop.
	if !callback.Succeeded() {
		return c.SendStatus(fiber.StatusOK)
	}

	tx := models.MpesaTransaction{
		TransID:           callback.ReceiptNumber,
		Source:            models.MpesaSourceSTKCallback,
		BillRefNumber:     callback.Payer,
		AmountKes:         callback.AmountKES,
		TransTime:         ctrl.now(),
		RawPayload:        datatypes.JSON(c.Body()),
		MerchantRequestID: &callback.MerchantRequestID,
		CheckoutRequestID: &callback.CheckoutRequestID,
		// A success callback is still only an observation. The poller confirms
		// it before anything credits.
		NextPollAt: ptrTime(ctrl.now().Add(ctrl.config.STKPollInterval)),
	}

	if err := ctrl.repo.Record(c.UserContext(), &tx); err != nil {
		if errors.Is(err, repository.ErrMpesaConflict) {
			return c.SendStatus(fiber.StatusOK)
		}
		return fiber.NewError(fiber.StatusInternalServerError, "could not record the observation")
	}
	return c.SendStatus(fiber.StatusOK)
}

// C2BValidation receives the validation callback. Responds inside the budget
// with the accept/reject decision.
// @Description Decide whether to accept an incoming C2B payment. Any answer other than ResultCode 0 rejects it.
// @Summary Validate a C2B payment
// @Tags Daraja
// @Accept json
// @Produce json
// @Param slug path string true "Callback slug"
// @Param body body mpesa.C2BNotificationWire true "Validation notification"
// @Success 200 {object} mpesa.ValidationResponse "Accept/reject decision"
// @Failure 400 {object} fiber.Error "Undecodable notification"
// @Failure 403 {object} fiber.Error "Source not permitted"
// @Router /api/v1/callbacks/daraja/{slug}/c2b/validation [post]
func (ctrl *DarajaCallbackController) C2BValidation(c *fiber.Ctx) error {
	if err := ctrl.allowedCIDR(c); err != nil {
		return err
	}

	notification, err := mpesa.ParseC2BNotification(c.Body())
	if err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "could not decode the notification")
	}

	// Validate the reference shape first — a reference that cannot be ours
	// should be rejected without a database round trip. The check character is
	// derived over the configured prefix, so this must read the same config
	// value the generator used.
	if !loanref.Validate(ctrl.config.ReferencePrefix, notification.BillRefNumber) {
		return c.JSON(mpesa.RejectPayment(mpesa.ValidationInvalidAccountNumber))
	}

	// Shape alone is not enough: the reference must resolve to an open loan.
	// The lookup is a single indexed query, and the 8-second budget is about
	// how long Daraja waits, not how long we take. An internal error here
	// rejects with C2B00016, never 0.
	// An unresolved reference is the payer's mistake (C2B00012); an internal
	// failure is ours (C2B00016). Neither may ever answer 0, which would
	// accept the payment.
	loanID, err := ctrl.resolveLoan(c.UserContext(), notification.BillRefNumber)
	if err != nil {
		return c.JSON(mpesa.RejectPayment(mpesa.ValidationOtherError))
	}
	if loanID == "" {
		return c.JSON(mpesa.RejectPayment(mpesa.ValidationInvalidAccountNumber))
	}
	return c.JSON(mpesa.AcceptPayment(loanID))
}

// C2BConfirmation receives the confirmation callback after a payment settled.
// @Description Record a settled C2B payment as an observation for the poller to confirm
// @Summary Record a C2B confirmation
// @Tags Daraja
// @Accept json
// @Produce json
// @Param slug path string true "Callback slug"
// @Param body body mpesa.C2BNotificationWire true "Confirmation notification"
// @Success 200 {string} string "Recorded"
// @Failure 400 {object} fiber.Error "Undecodable confirmation"
// @Failure 403 {object} fiber.Error "Source not permitted"
// @Failure 500 {object} fiber.Error "Failed to record the observation"
// @Router /api/v1/callbacks/daraja/{slug}/c2b/confirmation [post]
func (ctrl *DarajaCallbackController) C2BConfirmation(c *fiber.Ctx) error {
	if err := ctrl.allowedCIDR(c); err != nil {
		return err
	}

	notification, err := mpesa.ParseC2BNotification(c.Body())
	if err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "could not decode the confirmation")
	}

	tx := models.MpesaTransaction{
		TransID:           notification.TransID,
		Source:            models.MpesaSourceC2BConfirmation,
		BillRefNumber:     notification.BillRefNumber,
		AmountKes:         notification.TransAmountMinor / 100,
		MsidnMasked:       string(notification.MSISDN),
		PayerName:         &notification.FirstName,
		TransTime:         ctrl.now(),
		ThirdPartyTransID: &notification.ThirdPartyTransID,
		RawPayload:        datatypes.JSON(c.Body()),
	}
	if err := ctrl.repo.Record(c.UserContext(), &tx); err != nil {
		if errors.Is(err, repository.ErrMpesaConflict) {
			return c.SendStatus(fiber.StatusOK)
		}
		return fiber.NewError(fiber.StatusInternalServerError, "could not record the observation")
	}
	return c.SendStatus(fiber.StatusOK)
}

// AsyncResult receives the result of a Transaction Status, Account Balance or
// Reversal query. The result URL and the timeout URL are distinct routes —
// they cannot be told apart by their payload.
// @Description Record the result of an asynchronous Daraja query
// @Summary Record an async query result
// @Tags Daraja
// @Accept json
// @Produce json
// @Param slug path string true "Callback slug"
// @Param kind path string true "Query family" Enums(status, balance, reversal)
// @Param body body mpesa.ResultEnvelope true "Result delivery"
// @Success 200 {string} string "Recorded"
// @Failure 400 {object} fiber.Error "Undecodable result"
// @Failure 403 {object} fiber.Error "Source not permitted"
// @Failure 500 {object} fiber.Error "Failed to record the observation"
// @Router /api/v1/callbacks/daraja/{slug}/{kind}/result [post]
func (ctrl *DarajaCallbackController) AsyncResult(c *fiber.Ctx) error {
	if err := ctrl.allowedCIDR(c); err != nil {
		return err
	}
	return ctrl.async(c, mpesa.CallbackResult)
}

// AsyncTimeout receives the queue-timeout delivery. A timeout is never a
// failure — it moves the record to unknown, and the poller resolves it with
// TransactionStatus.
// @Description Record a queue-timeout delivery as unknown for the poller to resolve
// @Summary Record an async queue timeout
// @Tags Daraja
// @Accept json
// @Produce json
// @Param slug path string true "Callback slug"
// @Param kind path string true "Query family" Enums(status, balance, reversal)
// @Param body body mpesa.ResultEnvelope true "Timeout delivery"
// @Success 200 {string} string "Recorded"
// @Failure 400 {object} fiber.Error "Undecodable result"
// @Failure 403 {object} fiber.Error "Source not permitted"
// @Failure 500 {object} fiber.Error "Failed to record the observation"
// @Router /api/v1/callbacks/daraja/{slug}/{kind}/timeout [post]
func (ctrl *DarajaCallbackController) AsyncTimeout(c *fiber.Ctx) error {
	if err := ctrl.allowedCIDR(c); err != nil {
		return err
	}
	return ctrl.async(c, mpesa.CallbackTimeout)
}

func (ctrl *DarajaCallbackController) async(c *fiber.Ctx, kind mpesa.CallbackKind) error {
	callback, err := mpesa.ParseCallback(kind, c.Body())
	if err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "could not decode the result")
	}

	tx := models.MpesaTransaction{
		TransID:    callback.Result.TransactionID,
		Source:     models.MpesaSourceStatusResult,
		TransTime:  ctrl.now(),
		RawPayload: datatypes.JSON(c.Body()),
	}
	if kind == mpesa.CallbackTimeout {
		// Parked as unknown; the poller resolves with TransactionStatus.
		tx.NextPollAt = ptrTime(ctrl.now().Add(ctrl.config.STKPollInterval))
	}

	if err := ctrl.repo.Record(c.UserContext(), &tx); err != nil {
		if errors.Is(err, repository.ErrMpesaConflict) {
			return c.SendStatus(fiber.StatusOK)
		}
		return fiber.NewError(fiber.StatusInternalServerError, "could not record the observation")
	}

	if ctrl.balances != nil && kind == mpesa.CallbackResult && c.Params("kind") == "balance" {
		ctrl.recordBalances(c.UserContext(), callback)
	}

	return c.SendStatus(fiber.StatusOK)
}

// recordBalances persists the parsed balance figures and logs an alert when
// one falls below its configured floor. Best-effort throughout: this is an
// ops signal riding on the same route as the confirm-before-credit path, not
// part of it, so nothing here can change the 200 already decided above.
func (ctrl *DarajaCallbackController) recordBalances(ctx context.Context, callback *mpesa.Callback) {
	balances, ok := callback.Result.Parameters.Balances("AccountBalance")
	if !ok {
		return
	}
	shortcode, err := ctrl.balances.ResolveQuery(ctx, callback.Result.OriginatorConversationID)
	if err != nil {
		ctrl.logger.Warn("could not resolve which shortcode this balance result belongs to",
			"originator_conversation_id", callback.Result.OriginatorConversationID, "error", err)
		return
	}
	floor := ctrl.collectionFloorKES
	if shortcode == ctrl.config.DisbursementShortcode {
		floor = ctrl.disbursementFloorKES
	}

	for _, b := range balances {
		if err := ctrl.balances.RecordBalance(ctx, shortcode, b.Name, b.Currency, b.Available, ctrl.now()); err != nil {
			ctrl.logger.Warn("could not record account balance",
				"shortcode", shortcode, "account", b.Name, "error", err)
			continue
		}
		if floor > 0 && b.Available < floor {
			ctrl.logger.Warn("mpesa account balance below configured floor",
				"shortcode", shortcode, "account", b.Name, "available", b.Available, "floor", floor)
		}
	}
}

func ptrTime(t time.Time) *time.Time { return new(t) }

// Register mounts the callback routes on the given fiber group. The slug is
// the unguessable path segment; routes hang under it so the path cannot be
// enumerated. Note the path carries no blocked word — Daraja rejects URLs
// containing mpesa, safaricom, exe, exec, cmd, sql or query, which the client
// asserts at registration time.
func (ctrl *DarajaCallbackController) Register(app fiber.Router) {
	group := app.Group(fmt.Sprintf("/callbacks/daraja/%s", ctrl.config.CallbackSlug))
	group.Post("/stk/result", ctrl.STKCallback)
	group.Post("/c2b/validation", ctrl.C2BValidation)
	group.Post("/c2b/confirmation", ctrl.C2BConfirmation)
	group.Post("/status/result", ctrl.AsyncResult)
	group.Post("/status/timeout", ctrl.AsyncTimeout)
	group.Post("/balance/result", ctrl.AsyncResult)
	group.Post("/balance/timeout", ctrl.AsyncTimeout)
	group.Post("/reversal/result", ctrl.AsyncResult)
	group.Post("/reversal/timeout", ctrl.AsyncTimeout)
}
