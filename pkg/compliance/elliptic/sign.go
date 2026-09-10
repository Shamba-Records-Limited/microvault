package elliptic

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"strconv"
	"strings"
)

// sign computes Elliptic's request signature: base64 HMAC-SHA256, keyed on
// the base64-decoded secret, over "{timestamp_ms}{METHOD}{lowercased_path}{payload}".
//
// Two things here are easy to get wrong and expensive to discover in
// production — see the source design doc §3:
//   - The path is lowercased for signing but sent as-is on the wire. A
//     Stellar address is uppercase base32, so it must never appear in a
//     path or query — only in the body, which every endpoint here uses.
//   - An empty body signs as the two characters "{}", never "".
func sign(secretB64 string, timestampMs int64, method, path, payload string) (string, error) {
	secret, err := base64.StdEncoding.DecodeString(secretB64)
	if err != nil {
		return "", err
	}
	h := hmac.New(sha256.New, secret)
	h.Write([]byte(strconv.FormatInt(timestampMs, 10) + method + strings.ToLower(path) + payload))
	return base64.StdEncoding.EncodeToString(h.Sum(nil)), nil
}
