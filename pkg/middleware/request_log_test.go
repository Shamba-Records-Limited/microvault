package middleware_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v2"

	"github.com/Shamba-Records-Limited/microvault/pkg/logging"
	"github.com/Shamba-Records-Limited/microvault/pkg/middleware"
)

func newApp(buf *bytes.Buffer) *fiber.App {
	logger := logging.New(buf, slog.LevelInfo)
	app := fiber.New(fiber.Config{DisableStartupMessage: true})
	app.Use(middleware.RequestID(), middleware.AccessLog(logger, "/health", "/static/"))
	app.Get("/loans/:id", func(c *fiber.Ctx) error {
		logger.InfoContext(c.UserContext(), "inside handler")
		return c.SendString("ok")
	})
	app.Get("/boom", func(*fiber.Ctx) error { return errors.New("boom") })
	app.Get("/health", func(c *fiber.Ctx) error { return c.SendString("ok") })
	app.Get("/static/*", func(c *fiber.Ctx) error { return c.SendString("asset") })
	return app
}

func lines(t *testing.T, buf *bytes.Buffer) []map[string]any {
	t.Helper()
	var out []map[string]any
	for _, l := range strings.Split(strings.TrimSpace(buf.String()), "\n") {
		if l == "" {
			continue
		}
		var m map[string]any
		if err := json.Unmarshal([]byte(l), &m); err != nil {
			t.Fatalf("decode %q: %v", l, err)
		}
		out = append(out, m)
	}
	return out
}

func TestRequestIDReachesHandlerAndAccessLog(t *testing.T) {
	var buf bytes.Buffer
	app := newApp(&buf)

	resp, err := app.Test(httptest.NewRequestWithContext(t.Context(), "GET", "/loans/42", nil))
	if err != nil {
		t.Fatal(err)
	}
	id := resp.Header.Get(middleware.RequestIDHeader)
	if id == "" {
		t.Fatal("no request id on response")
	}

	got := lines(t, &buf)
	if len(got) != 2 {
		t.Fatalf("got %d lines, want 2: %v", len(got), got)
	}
	for _, l := range got {
		if l["request_id"] != id {
			t.Errorf("line %v lacks request_id %s", l, id)
		}
	}
	access := got[1]
	if access["route"] != "/loans/:id" || access["status"] != float64(200) {
		t.Errorf("access line %v", access)
	}
}

func TestInboundRequestIDIsAdoptedWhenWellFormed(t *testing.T) {
	var buf bytes.Buffer
	app := newApp(&buf)

	req := httptest.NewRequestWithContext(t.Context(), "GET", "/loans/1", nil)
	req.Header.Set(middleware.RequestIDHeader, "upstream-123")
	resp, _ := app.Test(req)
	if got := resp.Header.Get(middleware.RequestIDHeader); got != "upstream-123" {
		t.Fatalf("request id = %q", got)
	}

	req = httptest.NewRequestWithContext(t.Context(), "GET", "/loans/1", nil)
	req.Header.Set(middleware.RequestIDHeader, "bad id\nwith newline")
	resp, _ = app.Test(req)
	if got := resp.Header.Get(middleware.RequestIDHeader); got == "" || strings.Contains(got, " ") {
		t.Fatalf("malformed inbound id was adopted: %q", got)
	}
}

func TestHandlerErrorIsLoggedWithFinalStatus(t *testing.T) {
	var buf bytes.Buffer
	app := newApp(&buf)

	resp, _ := app.Test(httptest.NewRequestWithContext(t.Context(), "GET", "/boom", nil))
	if resp.StatusCode != 500 {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	got := lines(t, &buf)
	if len(got) != 1 || got[0]["status"] != float64(500) || got[0]["level"] != "ERROR" || got[0]["error"] != "boom" {
		t.Fatalf("got %v", got)
	}
}

func TestSkippedPathsAreNotLogged(t *testing.T) {
	var buf bytes.Buffer
	app := newApp(&buf)
	_, _ = app.Test(httptest.NewRequestWithContext(t.Context(), "GET", "/health", nil))
	_, _ = app.Test(httptest.NewRequestWithContext(t.Context(), "GET", "/static/css/admin.css", nil))
	if buf.Len() != 0 {
		t.Fatalf("health logged: %s", buf.String())
	}
}
