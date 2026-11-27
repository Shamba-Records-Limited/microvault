package chipper

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	pkgErrors "github.com/Shamba-Records-Limited/microvault/pkg/errors"
)

// Webhook event types.
const (
	EventPayoutCompleted     = "payout.completed"
	EventPayoutFailed        = "payout.failed"
	EventAccountCredited     = "account.credited"
	EventCollectionCompleted = "collection.completed"
	EventOrderFundsReceived  = "order.funds_received"
	EventOrderCompleted      = "order.completed"
	EventOrderFailed         = "order.failed"
	EventOrderExpired        = "order.expired"
	EventWebhookTest         = "webhook.test"
)

// Standard Webhooks headers.
const (
	HeaderWebhookID        = "webhook-id"
	HeaderWebhookTimestamp = "webhook-timestamp"
	HeaderWebhookSignature = "webhook-signature"
)

// DefaultWebhookTolerance bounds clock skew and replay age.
const DefaultWebhookTolerance = 5 * time.Minute

const webhookSecretPrefix = "whsec_"

// WebhookVerifier checks Standard Webhooks signatures.
type WebhookVerifier struct {
	key       []byte
	tolerance time.Duration
	now       func() time.Time
}

// NewWebhookVerifier decodes a whsec_ signing secret.
func NewWebhookVerifier(secret string) (*WebhookVerifier, error) {
	errb := chipperErr("new_webhook_verifier")
	raw, ok := strings.CutPrefix(secret, webhookSecretPrefix)
	if !ok || raw == "" {
		return nil, errb.Code(pkgErrors.CodeInvalidConfig).Errorf("webhook secret must start with whsec_")
	}
	key, err := base64.StdEncoding.DecodeString(raw)
	if err != nil {
		return nil, errb.Code(pkgErrors.CodeInvalidConfig).Wrapf(err, "webhook secret is not valid base64")
	}
	return &WebhookVerifier{key: key, tolerance: DefaultWebhookTolerance, now: time.Now}, nil
}

// Verify checks a delivery against the raw body. signature may hold several
// space-separated "v1,<base64>" entries; any match passes.
func (v *WebhookVerifier) Verify(id, timestamp, signature string, body []byte) error {
	errb := chipperErr("verify_webhook").With("webhook_id", id)

	if id == "" || timestamp == "" || signature == "" {
		return errb.Code(pkgErrors.CodeUnauthorized).Errorf("webhook is missing signature headers")
	}
	secs, err := strconv.ParseInt(timestamp, 10, 64)
	if err != nil {
		return errb.Code(pkgErrors.CodeUnauthorized).Errorf("webhook timestamp is not a unix time")
	}
	if skew := v.now().Sub(time.Unix(secs, 0)).Abs(); skew > v.tolerance {
		return errb.Code(pkgErrors.CodeUnauthorized).With("skew", skew.String()).
			Errorf("webhook timestamp is outside the tolerance")
	}

	mac := hmac.New(sha256.New, v.key)
	mac.Write([]byte(id + "." + timestamp + "."))
	mac.Write(body)
	expected := mac.Sum(nil)

	for entry := range strings.FieldsSeq(signature) {
		version, sig, ok := strings.Cut(entry, ",")
		if !ok || version != "v1" {
			continue
		}
		got, err := base64.StdEncoding.DecodeString(sig)
		if err == nil && hmac.Equal(got, expected) {
			return nil
		}
	}
	return errb.Code(pkgErrors.CodeUnauthorized).Errorf("webhook signature does not match")
}

// Event is a webhook delivery. Data is the resource as GET returns it.
type Event struct {
	Event string          `json:"event"`
	Data  json.RawMessage `json:"data"`
}

// ResourceRef is the identity and state of an event's resource. ID plus
// Status is the deduplication key.
type ResourceRef struct {
	ID                string `json:"id"`
	Status            string `json:"status"`
	ExternalReference string `json:"externalReference"`
}

// DedupeKey identifies a delivery independently of retries.
func (r ResourceRef) DedupeKey() string { return r.ID + ":" + r.Status }

// Parse verifies a delivery and decodes it.
func (v *WebhookVerifier) Parse(id, timestamp, signature string, body []byte) (*Event, error) {
	if err := v.Verify(id, timestamp, signature, body); err != nil {
		return nil, err
	}
	var event Event
	if err := json.Unmarshal(body, &event); err != nil {
		return nil, chipperErr("parse_webhook").Code(pkgErrors.CodeDecodeFailed).Wrapf(err, "could not decode the webhook payload")
	}
	return &event, nil
}

// Resource decodes the identity fields of Data.
func (e Event) Resource() (ResourceRef, error) {
	var ref ResourceRef
	if err := json.Unmarshal(e.Data, &ref); err != nil {
		return ResourceRef{}, chipperErr("webhook_resource").With("event", e.Event).
			Code(pkgErrors.CodeDecodeFailed).Wrapf(err, "could not decode the event resource")
	}
	return ref, nil
}

// Payout decodes Data as a payout.
func (e Event) Payout() (*Payout, error) {
	var p Payout
	if err := json.Unmarshal(e.Data, &p); err != nil {
		return nil, chipperErr("webhook_payout").With("event", e.Event).
			Code(pkgErrors.CodeDecodeFailed).Wrapf(err, "could not decode the payout")
	}
	return &p, nil
}

// EventRecord is an entry in the event log, kept even with no subscriber.
type EventRecord struct {
	ID         string          `json:"id"`
	EventType  string          `json:"eventType"`
	ResourceID string          `json:"resourceId"`
	Payload    json.RawMessage `json:"payload"`
	CreatedAt  time.Time       `json:"createdAt"`
}

// EventFilter narrows an event log read.
type EventFilter struct {
	EventType string
	Limit     int
	Cursor    string
}

// ListWebhookEvents reads one page of the event log, newest first.
func (c *Client) ListWebhookEvents(ctx context.Context, f EventFilter) (*Page[EventRecord], error) {
	q := url.Values{}
	setIf(q, "eventType", f.EventType)
	page, _, err := call[Page[EventRecord]](ctx, c, chipperErr("list_webhook_events"), http.MethodGet, "/v1/webhooks/events", pageQuery(q, f.Limit, f.Cursor), nil)
	return page, err
}
