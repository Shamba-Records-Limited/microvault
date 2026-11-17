package controllers

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUSSDCallbackChecksTheProvidersVerifier(t *testing.T) {
	var seenBody string
	verify := func(header func(string) string, body []byte) error {
		seenBody = string(body)
		if header("X-Signed") != "yes" {
			return errors.New("unsigned")
		}
		return nil
	}
	app := fiber.New()
	app.Post("/ussd/:provider", NewUSSDController(nil, "secret", WithRequestVerifier("mobilesasa", verify)).HandleCallback)

	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/ussd/mobilesasa?token=secret", strings.NewReader("sessionId=s1&text="))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := app.Test(req)
	require.NoError(t, err)
	assert.Equal(t, fiber.StatusUnauthorized, resp.StatusCode)
	assert.Equal(t, "sessionId=s1&text=", seenBody, "the verifier sees the raw body")

	req = httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/ussd/mobilesasa?token=wrong", strings.NewReader("sessionId=s1"))
	req.Header.Set("X-Signed", "yes")
	resp, err = app.Test(req)
	require.NoError(t, err)
	assert.Equal(t, fiber.StatusUnauthorized, resp.StatusCode, "a signature does not replace the token")
}
