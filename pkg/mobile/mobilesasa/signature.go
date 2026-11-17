package mobilesasa

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"strconv"
	"strings"
	"time"
)

// Callback headers carrying the team's webhook secret.
const (
	HeaderSecret    = "X-MobileSasa-Secret" //nolint:gosec // G101: a header name, not a credential.
	HeaderSignature = "X-MobileSasa-Signature"
)

// SignatureTolerance is how far a signature timestamp may drift from now.
const SignatureTolerance = 5 * time.Minute

// ErrBadSignature is returned for a callback whose headers do not prove it
// came from the gateway.
var ErrBadSignature = errors.New("mobilesasa: callback signature is not valid")

// Verifier checks that a callback was signed with the team's webhook secret.
type Verifier struct {
	secret string
	now    func() time.Time
}

// NewVerifier builds a verifier for secret.
func NewVerifier(secret string) *Verifier {
	return &Verifier{secret: secret, now: time.Now}
}

// Verify checks the raw body against the signature header, falling back to
// the plain secret header when no signature is sent. header reads a request
// header by name.
func (v *Verifier) Verify(header func(string) string, body []byte) error {
	if v == nil || v.secret == "" {
		return ErrBadSignature
	}
	if sig := header(HeaderSignature); sig != "" {
		return v.verifySignature(sig, body)
	}
	if got := header(HeaderSecret); got != "" && subtle.ConstantTimeCompare([]byte(got), []byte(v.secret)) == 1 {
		return nil
	}
	return ErrBadSignature
}

func (v *Verifier) verifySignature(sig string, body []byte) error {
	var ts, mac string
	for part := range strings.SplitSeq(sig, ",") {
		k, val, ok := strings.Cut(strings.TrimSpace(part), "=")
		if !ok {
			continue
		}
		switch k {
		case "t":
			ts = val
		case "v1":
			mac = val
		}
	}
	secs, err := strconv.ParseInt(ts, 10, 64)
	if err != nil || mac == "" {
		return ErrBadSignature
	}
	if d := v.now().Sub(time.Unix(secs, 0)); d > SignatureTolerance || d < -SignatureTolerance {
		return ErrBadSignature
	}
	if !hmac.Equal([]byte(Sign(v.secret, ts, body)), []byte(mac)) {
		return ErrBadSignature
	}
	return nil
}

// Sign returns the v1 signature of body at timestamp ts.
func Sign(secret, ts string, body []byte) string {
	h := hmac.New(sha256.New, []byte(secret))
	h.Write([]byte(ts))
	h.Write([]byte("."))
	h.Write(body)
	return hex.EncodeToString(h.Sum(nil))
}
