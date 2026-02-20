package controllers

import (
	"github.com/Shamba-Records-Limited/microvault/pkg/mobile/ussd"
	"github.com/gofiber/fiber/v2"
)

// USSDController handles USSD callback endpoints.
type USSDController struct {
	ussdService *ussd.USSDService
}

// NewUSSDController creates a new USSDController with the provided USSD service.
func NewUSSDController(ussdService *ussd.USSDService) *USSDController {
	return &USSDController{ussdService: ussdService}
}

// HandleCallback handles incoming USSD callback requests from any registered provider.
// @Description Handle USSD callback requests from the USSD gateway. The provider is specified in the URL path.
// @Summary USSD Callback Handler
// @Tags USSD
// @Accept application/x-www-form-urlencoded
// @Produce plain
// @Param provider path string true "USSD provider name (e.g. africastalking)"
// @Param sessionId formData string true "Session ID"
// @Param phoneNumber formData string true "User's phone number"
// @Param text formData string false "User input text"
// @Param serviceCode formData string true "USSD service code"
// @Param networkCode formData string false "Network code"
// @Success 200 {string} string "USSD response (CON/END)"
// @Failure 500 {string} string "END An error occurred. Please try again."
// @Router /api/v1/mobile/ussd/{provider} [post]
func (ctrl *USSDController) HandleCallback(c *fiber.Ctx) error {
	provider := c.Params("provider")
	data := make(map[string]string)
	c.Request().PostArgs().VisitAll(func(key, value []byte) {
		data[string(key)] = string(value)
	})
	resp, err := ctrl.ussdService.HandleRequest(c.Context(), provider, data)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).SendString("END An error occurred. Please try again.")
	}
	return c.SendString(resp.(string))
}
