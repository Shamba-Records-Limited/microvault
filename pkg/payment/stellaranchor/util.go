package stellaranchor

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"syscall"
	"time"
)

// truncate returns the first n characters of s with an ellipsis appended when
// the string was longer. Used to keep error messages bounded so a misbehaving
// anchor cannot dump unbounded payloads into logs.
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// httpMaxAttempts bounds the transient-error retries in doHTTPWithRetry.
const httpMaxAttempts = 3

// doHTTPWithRetry issues the request built by build, retrying on transient
// connection failures (notably HTTP/2 GOAWAY from recycled keep-alive
// connections). build is called fresh each attempt so the request body is
// rewound. Non-transient errors and HTTP responses return immediately.
func doHTTPWithRetry(ctx context.Context, client *http.Client, build func() (*http.Request, error)) (*http.Response, error) {
	var lastErr error
	for attempt := 1; attempt <= httpMaxAttempts; attempt++ {
		req, err := build()
		if err != nil {
			return nil, err
		}
		resp, err := client.Do(req)
		if err == nil {
			return resp, nil
		}
		lastErr = err
		if !isTransientNetErr(err) || attempt == httpMaxAttempts {
			return nil, err
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(time.Duration(attempt) * 200 * time.Millisecond):
		}
	}
	return nil, lastErr
}

// isTransientNetErr reports whether err is a connection-level failure worth
// retrying — GOAWAY, connection reset, or a truncated response.
func isTransientNetErr(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, syscall.ECONNRESET) {
		return true
	}
	s := err.Error()
	return strings.Contains(s, "GOAWAY") ||
		strings.Contains(s, "connection reset") ||
		strings.Contains(s, "unexpected EOF") ||
		strings.Contains(s, "server closed idle connection") ||
		strings.Contains(s, "broken pipe")
}
