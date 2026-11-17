package controllers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Shamba-Records-Limited/microvault/pkg/mobile/sms"
)

func TestDeliveryReportNeedsTheToken(t *testing.T) {
	got := make(chan sms.DeliveryReport, 1)
	handler := sms.NewDeliveryReportHandler(sms.WithReportHandler(func(_ context.Context, r sms.DeliveryReport) { got <- r }))
	form := url.Values{"id": {"ATXid_1"}, "status": {"Success"}}.Encode()

	post := func(token, configured string) int {
		app := fiber.New()
		app.Post("/sms/:provider/delivery", NewSMSCallbackController(handler, configured).HandleDeliveryReport)
		req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/sms/africastalking/delivery?token="+url.QueryEscape(token), strings.NewReader(form))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		resp, err := app.Test(req)
		require.NoError(t, err)
		return resp.StatusCode
	}

	assert.Equal(t, fiber.StatusUnauthorized, post("", "secret"))
	assert.Equal(t, fiber.StatusUnauthorized, post("wrong", "secret"))
	assert.Equal(t, fiber.StatusUnauthorized, post("", ""))
	select {
	case <-got:
		t.Fatal("a rejected report reached the handler")
	default:
	}

	assert.Equal(t, fiber.StatusOK, post("secret", "secret"))
	select {
	case r := <-got:
		assert.Equal(t, "ATXid_1", r.ID)
	case <-time.After(time.Second):
		t.Fatal("the accepted report never reached the handler")
	}
}

func TestDeliveryReportUsesTheProvidersParser(t *testing.T) {
	got := make(chan sms.DeliveryReport, 1)
	handler := sms.NewDeliveryReportHandler(sms.WithReportHandler(func(_ context.Context, r sms.DeliveryReport) { got <- r }))
	parser := func(header func(string) string, body []byte) (sms.DeliveryReport, error) {
		if header("X-Signed") != "yes" {
			return sms.DeliveryReport{}, sms.ErrReportUnauthenticated
		}
		if string(body) != `{"ok":true}` {
			return sms.DeliveryReport{}, assert.AnError
		}
		return sms.DeliveryReport{ID: "m-1", Status: "Success"}, nil
	}
	app := fiber.New()
	app.Post("/sms/:provider/delivery", NewSMSCallbackController(handler, "secret", WithReportParser("mobilesasa", parser)).HandleDeliveryReport)

	post := func(signed, body string) int {
		req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/sms/mobilesasa/delivery?token=secret", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Signed", signed)
		resp, err := app.Test(req)
		require.NoError(t, err)
		return resp.StatusCode
	}

	assert.Equal(t, fiber.StatusUnauthorized, post("no", `{"ok":true}`))
	assert.Equal(t, fiber.StatusBadRequest, post("yes", `{"ok":false}`))
	assert.Equal(t, fiber.StatusOK, post("yes", `{"ok":true}`))
	select {
	case r := <-got:
		assert.Equal(t, "m-1", r.ID)
	case <-time.After(time.Second):
		t.Fatal("the accepted report never reached the handler")
	}
}
