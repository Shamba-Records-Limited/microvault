package middleware

import (
	"log/slog"
	"regexp"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"

	"github.com/Shamba-Records-Limited/microvault/pkg/logging"
)

// RequestIDHeader carries the request ID in and out.
const RequestIDHeader = "X-Request-ID"

var validRequestID = regexp.MustCompile(`^[A-Za-z0-9._-]{1,128}$`)

// RequestID adopts a well-formed inbound X-Request-ID or generates one, echoes
// it on the response and attaches it to the request's user context.
func RequestID() fiber.Handler {
	return func(c *fiber.Ctx) error {
		id := c.Get(RequestIDHeader)
		if !validRequestID.MatchString(id) {
			id = uuid.NewString()
		}
		c.Set(RequestIDHeader, id)
		c.SetUserContext(logging.With(c.UserContext(), slog.String("request_id", id)))
		return c.Next()
	}
}

// AccessLog logs one line per request after the handler chain and the app's
// error handler have run, with the chain's error if it returned one. A path equal to an entry in skip is not logged; an
// entry ending in "/" skips every path under it.
func AccessLog(logger *slog.Logger, skip ...string) fiber.Handler {
	return func(c *fiber.Ctx) error {
		if skipped(c.Path(), skip) {
			return c.Next()
		}
		start := time.Now()
		chainErr := c.Next()
		if chainErr != nil {
			if herr := c.App().ErrorHandler(c, chainErr); herr != nil {
				_ = c.SendStatus(fiber.StatusInternalServerError)
			}
		}
		status := c.Response().StatusCode()
		level := slog.LevelInfo
		if status >= fiber.StatusInternalServerError {
			level = slog.LevelError
		}
		attrs := []slog.Attr{
			slog.String("method", c.Method()),
			slog.String("route", c.Route().Path),
			slog.Int("status", status),
			slog.Int64("latency_ms", time.Since(start).Milliseconds()),
		}
		if chainErr != nil {
			attrs = append(attrs, slog.Any("error", chainErr))
		}
		logger.LogAttrs(c.UserContext(), level, "http request", attrs...)
		return nil
	}
}

func skipped(path string, skip []string) bool {
	for _, p := range skip {
		if path == p || (strings.HasSuffix(p, "/") && strings.HasPrefix(path, p)) {
			return true
		}
	}
	return false
}
