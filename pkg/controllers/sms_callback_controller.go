package controllers

import (
	"context"
	"crypto/subtle"
	"errors"
	"log/slog"

	"github.com/gofiber/fiber/v3"

	"github.com/Shamba-Records-Limited/microvault/pkg/middleware"
	"github.com/Shamba-Records-Limited/microvault/pkg/mobile/sms"
)

// SMSCallbackController handles SMS callback endpoints.
type SMSCallbackController struct {
	handler       *sms.DeliveryReportHandler
	callbackToken string
	parsers       map[string]sms.ReportParser
}

// SMSCallbackOption configures an SMSCallbackController.
type SMSCallbackOption func(*SMSCallbackController)

// WithReportParser reads provider's reports with parser instead of the
// form-encoded default.
func WithReportParser(provider string, parser sms.ReportParser) SMSCallbackOption {
	return func(c *SMSCallbackController) { c.parsers[provider] = parser }
}

// NewSMSCallbackController creates a new SMSCallbackController. An empty
// callbackToken rejects every report.
func NewSMSCallbackController(handler *sms.DeliveryReportHandler, callbackToken string, opts ...SMSCallbackOption) *SMSCallbackController {
	c := &SMSCallbackController{handler: handler, callbackToken: callbackToken, parsers: map[string]sms.ReportParser{}}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// HandleDeliveryReport processes incoming SMS delivery report callbacks.
// Africa's Talking posts form-encoded reports; providers registered with
// WithReportParser are read by their own parser.
// @Description Handle incoming SMS delivery report callbacks (form-encoded from Africa's Talking, JSON from Mobile Sasa)
// @Summary Process SMS Delivery Report
// @Tags Mobile
// @Accept application/x-www-form-urlencoded
// @Produce plain
// @Param provider path string true "SMS provider name (e.g. africastalking)"
// @Param token query string true "Shared callback token"
// @Param id formData string true "AT message ID"
// @Param status formData string true "Delivery status"
// @Param phoneNumber formData string false "Recipient phone number"
// @Param networkCode formData string false "Network code"
// @Param failureReason formData string false "Failure reason (if applicable)"
// @Success 200 {string} string "ok"
// @Failure 400 {object} fiber.Error "Invalid callback payload"
// @Failure 401 {string} string "Unauthorized"
// @Router /api/v1/mobile/sms/{provider}/delivery [post]
func (ctrl *SMSCallbackController) HandleDeliveryReport(c fiber.Ctx) error {
	if ctrl.callbackToken == "" || subtle.ConstantTimeCompare([]byte(c.Query("token")), []byte(ctrl.callbackToken)) != 1 {
		return c.Status(fiber.StatusUnauthorized).SendString("Unauthorized")
	}
	provider := c.Params("provider")

	slog.InfoContext(c.Context(), "sms callback: delivery report received",
		slog.String("provider", provider),
	)

	parse, ok := ctrl.parsers[provider]
	if !ok {
		parse = sms.ParseFormReport
	}
	report, err := parse(func(k string) string { return c.Get(k) }, c.Body())
	if errors.Is(err, sms.ErrReportUnauthenticated) {
		middleware.NoteError(c, err)
		return c.Status(fiber.StatusUnauthorized).SendString("Unauthorized")
	}
	if err != nil {
		middleware.NoteError(c, err)
		return fiber.NewError(fiber.StatusBadRequest, "invalid delivery report")
	}

	go ctrl.handler.HandleReport(context.WithoutCancel(c.Context()), report)

	return c.SendString("ok")
}
