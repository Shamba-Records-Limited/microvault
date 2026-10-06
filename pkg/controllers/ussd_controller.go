package controllers

import (
	"crypto/subtle"

	"github.com/gofiber/fiber/v2"

	"github.com/Shamba-Records-Limited/microvault/pkg/middleware"
	"github.com/Shamba-Records-Limited/microvault/pkg/mobile/ussd"
)

// USSDController handles USSD callback endpoints.
type USSDController struct {
	ussdService   *ussd.USSDService
	callbackToken string
}

// NewUSSDController creates a new USSDController. Callbacks must carry
// callbackToken in the token query parameter; an empty token rejects all.
func NewUSSDController(ussdService *ussd.USSDService, callbackToken string) *USSDController {
	return &USSDController{ussdService: ussdService, callbackToken: callbackToken}
}

// HandleCallback handles incoming USSD callback requests from any registered provider.
// @Description Handle USSD callback requests from the USSD gateway. The provider is specified in the URL path.
// @Summary USSD Callback Handler
// @Tags Mobile
// @Accept application/x-www-form-urlencoded
// @Produce plain
// @Param provider path string true "USSD provider name (e.g. africastalking)"
// @Param token query string true "Shared callback token"
// @Param sessionId formData string true "Session ID"
// @Param phoneNumber formData string true "User's phone number"
// @Param text formData string false "User input text"
// @Param serviceCode formData string true "USSD service code"
// @Param networkCode formData string false "Network code"
// @Success 200 {string} string "USSD response (CON/END)"
// @Failure 401 {string} string "END Unauthorized"
// @Failure 500 {string} string "END An error occurred. Please try again."
// @Router /api/v1/mobile/ussd/{provider} [post]
func (ctrl *USSDController) HandleCallback(c *fiber.Ctx) error {
	if ctrl.callbackToken == "" || subtle.ConstantTimeCompare([]byte(c.Query("token")), []byte(ctrl.callbackToken)) != 1 {
		return c.Status(fiber.StatusUnauthorized).SendString("END Unauthorized")
	}
	provider := c.Params("provider")
	data := make(map[string]string)
	for key, value := range c.Request().PostArgs().All() {
		data[string(key)] = string(value)
	}
	resp, err := ctrl.ussdService.HandleRequest(c.UserContext(), provider, data)
	if err != nil {
		middleware.NoteError(c, err)
		return c.Status(fiber.StatusInternalServerError).SendString("END An error occurred. Please try again.")
	}
	text, ok := resp.(string)
	if !ok {
		return c.Status(fiber.StatusInternalServerError).SendString("END An error occurred. Please try again.")
	}
	return c.SendString(text)
}
