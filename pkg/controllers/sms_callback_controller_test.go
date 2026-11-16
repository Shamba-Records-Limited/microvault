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
