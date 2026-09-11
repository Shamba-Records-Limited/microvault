package middleware

import (
	"errors"

	"github.com/gofiber/fiber/v2"
)

// Response represents the response format
type Response struct {
	// Status is "success" or "error", derived from Code.
	Status string `json:"status" example:"success"`
	// Code is the HTTP status code of the response.
	Code int `json:"code" example:"200"`
	// Data is the handler's payload on success, or {"error": ...} on failure.
	Data any `json:"data"`
	// Message is a human-readable summary of the status code.
	Message string `json:"message" example:"Request processed successfully"`
}

// FormatResponse middleware formats the response to a standard format.
func FormatResponse() fiber.Handler {
	return func(c *fiber.Ctx) error {
		// Call the next handler and get the data
		err := c.Next()

		var data any
		var errorMessage any
		var message string
		var statusCode int

		// Check if an error occurred
		if err != nil {
			// An error occurred, get the error message from the context's locals
			errorMessage = c.Locals("error")

			// Check if the error is a fiber.Error and get the status code
			var fiberError *fiber.Error
			if errors.As(err, &fiberError) {
				statusCode = fiberError.Code
			} else {
				statusCode = fiber.StatusInternalServerError
			}

			data = map[string]any{
				"error": errorMessage,
			}
		} else {
			// No error occurred, get the data from the context's locals
			data = c.Locals("data")
			statusCode = c.Response().StatusCode()
		}

		// Get the message for the status code
		statusMessages := map[int]string{
			fiber.StatusOK:                  "Request processed successfully",
			fiber.StatusCreated:             "Resource created successfully",
			fiber.StatusBadRequest:          "Bad Request",
			fiber.StatusUnauthorized:        "Unauthorized",
			fiber.StatusForbidden:           "Forbidden",
			fiber.StatusNotFound:            "Resource not found",
			fiber.StatusMethodNotAllowed:    "Method not allowed",
			fiber.StatusUnprocessableEntity: "Validation of request object failed",
			fiber.StatusInternalServerError: "Internal server error",
		}

		message, ok := statusMessages[statusCode]
		if !ok {
			message = "unknown status code"
		}

		// Determine the status based on the status code
		status := "success"
		if statusCode >= 400 {
			status = "error"
		}

		response := Response{
			Status:  status,
			Code:    statusCode,
			Data:    data,
			Message: message,
		}

		// Send the Response as the response
		return c.Status(response.Code).JSON(response)
	}
}
