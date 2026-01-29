package controllers

import (
	"log"

	"github.com/Shamba-Records-Limited/microvault/pkg/mobile/ussd"
	"github.com/gofiber/fiber/v2"
)

// USSDController handles USSD callback endpoints.
type USSDController struct {
	ussdService *ussd.USSDService
}

// NewUSSDController creates a new USSDController with the provided service.
func NewUSSDController(ussdService *ussd.USSDService) *USSDController {
	return &USSDController{
		ussdService: ussdService,
	}
}

// USSDCallbackRequest represents the USSD callback request parameters.
type USSDCallbackRequest struct {
	SessionID   string `form:"sessionId" json:"session_id"`
	PhoneNumber string `form:"phoneNumber" json:"phone_number"`
	Text        string `form:"text" json:"text"`
	ServiceCode string `form:"serviceCode" json:"service_code"`
	NetworkCode string `form:"networkCode" json:"network_code"`
}

// HandleCallback handles incoming USSD callback requests from Africa's Talking.
// @Description Handle USSD callback requests from the USSD gateway
// @Summary USSD Callback Handler
// @Tags USSD
// @Accept application/x-www-form-urlencoded
// @Produce plain
// @Param sessionId formData string true "Session ID"
// @Param phoneNumber formData string true "User's phone number"
// @Param text formData string false "User input text"
// @Param serviceCode formData string true "USSD service code"
// @Param networkCode formData string false "Network code"
// @Success 200 {string} string "USSD response (CON/END)"
// @Failure 500 {object} middleware.Response "Failed to process USSD request"
// @Router /api/v1/mobile/ussd-callback [post]
func (ctrl *USSDController) HandleCallback(c *fiber.Ctx) error {
	// Parse form data from the request
	data := make(map[string]string)

	// Africa's Talking sends data as form fields
	data["sessionId"] = c.FormValue("sessionId")
	data["phoneNumber"] = c.FormValue("phoneNumber")
	data["text"] = c.FormValue("text")
	data["serviceCode"] = c.FormValue("serviceCode")
	data["networkCode"] = c.FormValue("networkCode")

	log.Printf("Request from AT: %v", data)
	// Process the request using the USSD service with the africastalking provider
	response, err := ctrl.ussdService.HandleRequest(c.Context(), "africastalking", data)
	log.Println("USSD request processed:", response)
	if err != nil {
		log.Println("Failed to process USSD request:", err)
		c.Locals("error", err.Error())
		return fiber.NewError(fiber.StatusInternalServerError, "failed to process USSD request")
	}

	// Set response as plain text (Africa's Talking expects plain text response)
	c.Set(fiber.HeaderContentType, fiber.MIMETextPlainCharsetUTF8)
	return c.SendString(response.(string))
}
