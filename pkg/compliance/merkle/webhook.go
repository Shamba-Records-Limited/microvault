package merkle

import (
	"crypto/subtle"
	"encoding/json"
	"sort"
	"strings"

	"github.com/stellar/go-stellar-sdk/strkey"
)

// WebhookKeyHeader carries the secret configured on the Compass webhook.
const WebhookKeyHeader = "X-WEBHOOK-KEY"

// WebhookKeyValid reports whether got matches the configured key. An empty
// key never matches.
func WebhookKeyValid(key, got string) bool {
	return key != "" && subtle.ConstantTimeCompare([]byte(key), []byte(got)) == 1
}

// Webhook is what Microvault reads from a Compass notification. Compass does
// not publish the alert payload, so the Stellar addresses are collected from
// anywhere in the body.
type Webhook struct {
	EventType string
	Addresses []string
}

// IsReport reports whether the notification is a report download completion,
// which carries no risk information.
func (w Webhook) IsReport() bool {
	return strings.HasPrefix(w.EventType, "report:")
}

// ParseWebhook decodes a Compass notification body.
func ParseWebhook(body []byte) (Webhook, error) {
	var v any
	if err := json.Unmarshal(body, &v); err != nil {
		return Webhook{}, err
	}
	var w Webhook
	if m, ok := v.(map[string]any); ok {
		for _, k := range []string{"event_type", "event", "type"} {
			if s, ok := m[k].(string); ok && s != "" {
				w.EventType = s
				break
			}
		}
	}
	seen := map[string]bool{}
	collectAddresses(v, seen, &w.Addresses)
	sort.Strings(w.Addresses)
	return w, nil
}

func collectAddresses(v any, seen map[string]bool, out *[]string) {
	switch t := v.(type) {
	case map[string]any:
		for _, x := range t {
			collectAddresses(x, seen, out)
		}
	case []any:
		for _, x := range t {
			collectAddresses(x, seen, out)
		}
	case string:
		s := strings.TrimSpace(t)
		if !seen[s] && strkey.IsValidEd25519PublicKey(s) {
			seen[s] = true
			*out = append(*out, s)
		}
	}
}
