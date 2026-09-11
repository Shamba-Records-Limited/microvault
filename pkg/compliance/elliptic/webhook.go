package elliptic

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"strconv"
	"strings"
	"time"

	pkgErrors "github.com/Shamba-Records-Limited/microvault/pkg/errors"
)

// webhookTolerance is the timestamp freshness window — outside it, a
// signature is rejected as a possible replay even if it matches. Standard
// Webhooks' own reference implementations use five minutes; matched here.
const webhookTolerance = 5 * time.Minute

// VerifyWebhook checks a Standard Webhooks payload against the given
// secret (base64, as issued — the source design doc notes Elliptic ships
// verifier classes for JavaScript, Python and PHP only, so this is a from
// scratch implementation of that spec, not a port of theirs).
//
// id, timestamp and signature are the raw webhook-id, webhook-timestamp
// and webhook-signature header values. signature may contain multiple
// space-separated "v1,<sig>" entries during secret rotation — any match
// verifies.
func VerifyWebhook(secretB64, id, timestamp, signature string, body []byte) error {
	const op = "verify_webhook"

	if id == "" || timestamp == "" || signature == "" {
		return ellipticErr(op).Code(pkgErrors.CodeMissingAccount).
			Errorf("webhook headers are incomplete")
	}

	ts, err := strconv.ParseInt(timestamp, 10, 64)
	if err != nil {
		return ellipticErr(op).Code(pkgErrors.CodeDecodeFailed).
			Wrapf(err, "webhook timestamp is not a valid unix timestamp")
	}
	age := time.Since(time.Unix(ts, 0))
	if age < 0 {
		age = -age
	}
	if age > webhookTolerance {
		return ellipticErr(op).Code(pkgErrors.CodeUnauthorized).
			With("age_seconds", age.Seconds()).
			Errorf("webhook timestamp is outside the freshness window")
	}

	secret, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(secretB64, "whsec_"))
	if err != nil {
		return ellipticErr(op).Code(pkgErrors.CodeBuildFailed).
			Wrapf(err, "webhook secret is not valid base64")
	}

	signedContent := id + "." + timestamp + "." + string(body)
	h := hmac.New(sha256.New, secret)
	h.Write([]byte(signedContent))
	expected := base64.StdEncoding.EncodeToString(h.Sum(nil))

	for _, candidate := range strings.Fields(signature) {
		candidate = strings.TrimPrefix(candidate, "v1,")
		if hmac.Equal([]byte(candidate), []byte(expected)) {
			return nil
		}
	}
	return ellipticErr(op).Code(pkgErrors.CodeUnauthorized).
		Errorf("webhook signature did not match any provided candidate")
}
