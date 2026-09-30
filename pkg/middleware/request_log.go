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

const handledErrorLocal = "access_log_handled_error"

// NoteError records an error the handler dealt with itself — rendered into a
// page or turned into a response — so AccessLog reports it on the request's
// line instead of the handler logging it separately.
func NoteError(c *fiber.Ctx, err error) {
	if err != nil {
		c.Locals(handledErrorLocal, err)
	}
}

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
// error handler have run, with the error recorded by NoteError or, failing
// that, the one the chain returned. The noted error wins because handlers
// usually replace the cause with a generic HTTP error. A request with an
// error and a non-5xx status logs at warn. A path equal to an entry in skip is not logged; an
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
		reqErr, _ := c.Locals(handledErrorLocal).(error)
		if reqErr == nil {
			reqErr = chainErr
		}
		status := c.Response().StatusCode()
		level := slog.LevelInfo
		switch {
		case status >= fiber.StatusInternalServerError:
			level = slog.LevelError
		case reqErr != nil:
			level = slog.LevelWarn
		}
		attrs := []slog.Attr{
			slog.String("method", c.Method()),
			slog.String("route", c.Route().Path),
			slog.Int("status", status),
			slog.Int64("latency_ms", time.Since(start).Milliseconds()),
		}
		if reqErr != nil {
			attrs = append(attrs, slog.Any("error", reqErr))
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
