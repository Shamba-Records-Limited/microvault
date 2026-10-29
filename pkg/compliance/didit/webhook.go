package didit

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// webhookTolerance is Didit's replay window.
const webhookTolerance = 300 * time.Second

// ErrInvalidSignature means no signature on the webhook matched.
var ErrInvalidSignature = errors.New("didit: webhook signature does not match")

// ErrStaleWebhook means X-Timestamp is outside the replay window.
var ErrStaleWebhook = errors.New("didit: webhook timestamp outside the replay window")

// WebhookHeaders are the signature headers of one delivery. The -Previous
// values are sent for 24 hours after the destination's secret is rotated.
type WebhookHeaders struct {
	Timestamp   string
	SignatureV2 string
	PreviousV2  string
	Signature   string
	PreviousSig string
}

// Event is the envelope of a status or data webhook.
type Event struct {
	Event         string `json:"event"`
	WebhookType   string `json:"webhook_type"`
	ApplicationID string `json:"application_id"`
	Data          struct {
		SessionID      string `json:"session_id"`
		SessionKind    string `json:"session_kind"`
		VendorData     string `json:"vendor_data"`
		Status         string `json:"status"`
		PreviousStatus string `json:"previous_status"`
	} `json:"data"`
}

// VerifyWebhook checks a delivery's timestamp and signature against secret
// and returns the decoded envelope. X-Signature-V2 (canonical JSON) is tried
// first, then X-Signature over the raw body; the deprecated Simple signature
// is never accepted because it does not cover the decision.
func VerifyWebhook(secret string, body []byte, h WebhookHeaders, now time.Time) (*Event, error) {
	ts, err := strconv.ParseInt(strings.TrimSpace(h.Timestamp), 10, 64)
	if err != nil {
		return nil, ErrStaleWebhook
	}
	if d := now.Sub(time.Unix(ts, 0)); d > webhookTolerance || d < -webhookTolerance {
		return nil, ErrStaleWebhook
	}

	ok := false
	if h.SignatureV2 != "" || h.PreviousV2 != "" {
		canonical, err := Canonical(body)
		if err == nil {
			ok = matches(secret, canonical, h.SignatureV2, h.PreviousV2)
		}
	}
	if !ok {
		ok = matches(secret, body, h.Signature, h.PreviousSig)
	}
	if !ok {
		return nil, ErrInvalidSignature
	}

	var ev Event
	if err := json.Unmarshal(body, &ev); err != nil {
		return nil, fmt.Errorf("didit: decode webhook: %w", err)
	}
	return &ev, nil
}

func matches(secret string, payload []byte, sigs ...string) bool {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(payload)
	expected := hex.EncodeToString(mac.Sum(nil))
	for _, s := range sigs {
		if s != "" && hmac.Equal([]byte(strings.ToLower(strings.TrimSpace(s))), []byte(expected)) {
			return true
		}
	}
	return false
}

var wholeFloat = regexp.MustCompile(`^-?\d+\.0+$`)

// Canonical re-serialises a JSON body the way Didit signs X-Signature-V2:
// sorted keys, "," and ":" separators, Unicode unescaped, and whole-valued
// floats written as integers. Number literals are otherwise kept as sent.
func Canonical(body []byte) ([]byte, error) {
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	writeCanonical(&buf, v)
	return buf.Bytes(), nil
}

func writeCanonical(buf *bytes.Buffer, v any) {
	switch t := v.(type) {
	case map[string]any:
		keys := make([]string, 0, len(t))
		for k := range t {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		buf.WriteByte('{')
		for i, k := range keys {
			if i > 0 {
				buf.WriteByte(',')
			}
			writeString(buf, k)
			buf.WriteByte(':')
			writeCanonical(buf, t[k])
		}
		buf.WriteByte('}')
	case []any:
		buf.WriteByte('[')
		for i, e := range t {
			if i > 0 {
				buf.WriteByte(',')
			}
			writeCanonical(buf, e)
		}
		buf.WriteByte(']')
	case json.Number:
		s := t.String()
		if wholeFloat.MatchString(s) {
			s = s[:strings.IndexByte(s, '.')]
		}
		buf.WriteString(s)
	case string:
		writeString(buf, t)
	case bool:
		buf.WriteString(strconv.FormatBool(t))
	case nil:
		buf.WriteString("null")
	}
}

// writeString escapes like Python's json.dumps(ensure_ascii=False): quotes,
// backslashes and control characters only.
func writeString(buf *bytes.Buffer, s string) {
	buf.WriteByte('"')
	for _, r := range s {
		switch r {
		case '"':
			buf.WriteString(`\"`)
		case '\\':
			buf.WriteString(`\\`)
		case '\n':
			buf.WriteString(`\n`)
		case '\r':
			buf.WriteString(`\r`)
		case '\t':
			buf.WriteString(`\t`)
		case '\b':
			buf.WriteString(`\b`)
		case '\f':
			buf.WriteString(`\f`)
		default:
			if r < 0x20 {
				fmt.Fprintf(buf, `\u%04x`, r)
			} else if r == utf8.RuneError {
				buf.WriteString(`�`)
			} else {
				buf.WriteRune(r)
			}
		}
	}
	buf.WriteByte('"')
}
