package controllers

import (
	"context"
	"crypto/subtle"
	"log/slog"

	"github.com/gofiber/fiber/v3"

	"github.com/Shamba-Records-Limited/microvault/pkg/mobile/sms"
)

// SMSCallbackController handles SMS callback endpoints.
type SMSCallbackController struct {
	handler       *sms.DeliveryReportHandler
	callbackToken string
}

// NewSMSCallbackController creates a new SMSCallbackController. An empty
// callbackToken rejects every report.
func NewSMSCallbackController(handler *sms.DeliveryReportHandler, callbackToken string) *SMSCallbackController {
	return &SMSCallbackController{handler: handler, callbackToken: callbackToken}
}

// HandleDeliveryReport processes incoming SMS delivery report callbacks from Africa's Talking.
// AT sends form-encoded POST requests with delivery status updates.
// @Description Handle incoming SMS delivery report callbacks from Africa's Talking
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

	report := sms.DeliveryReport{
		ID:            c.FormValue("id"),
		Status:        c.FormValue("status"),
		PhoneNumber:   c.FormValue("phoneNumber"),
		NetworkCode:   c.FormValue("networkCode"),
		FailureReason: c.FormValue("failureReason"),
	}

	if report.ID == "" || report.Status == "" {
		return fiber.NewError(fiber.StatusBadRequest, "missing required fields: id and status")
	}

	go ctrl.handler.HandleReport(context.WithoutCancel(c.Context()), report)

	return c.SendString("ok")
}
