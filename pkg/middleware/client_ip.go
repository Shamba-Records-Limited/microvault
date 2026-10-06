package middleware

import (
	"net"
	"strings"

	"github.com/gofiber/fiber/v2"
)

// ClientIP returns the TCP peer address, or the rightmost X-Forwarded-For hop
// when the peer is a configured trusted proxy. Unparseable hops fall back to
// the peer.
func ClientIP(c *fiber.Ctx) string {
	peer := c.Context().RemoteIP().String()
	if !c.App().Config().EnableTrustedProxyCheck || !c.IsProxyTrusted() {
		return peer
	}
	xff := c.Get(fiber.HeaderXForwardedFor)
	if i := strings.LastIndexByte(xff, ','); i >= 0 {
		xff = xff[i+1:]
	}
	if ip := net.ParseIP(strings.TrimSpace(xff)); ip != nil {
		return ip.String()
	}
	return peer
}
