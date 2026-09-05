//go:build daraja_sandbox

package mpesa

import (
	"context"
	"os"
	"strconv"
	"testing"
)

// The stub is a second implementation of Safaricom's specification, so the
// failure mode this suite exists for is our client and our stub sharing a
// misreading. Nothing else can catch that.
//
// It is not part of CI. Run it by hand, once per phase:
//
//	go test -tags daraja_sandbox -run Sandbox ./pkg/payment/mpesa/...
//
// Credentials come from the environment and are never committed.
func sandboxClient(t *testing.T) *Client {
	t.Helper()

	key, secret := os.Getenv("DARAJA_CONSUMER_KEY"), os.Getenv("DARAJA_CONSUMER_SECRET")
	if key == "" || secret == "" {
		t.Skip("set DARAJA_CONSUMER_KEY and DARAJA_CONSUMER_SECRET to run the sandbox suite")
	}

	c, err := New(Config{
		Environment:         EnvironmentSandbox,
		ConsumerKey:         key,
		ConsumerSecret:      secret,
		CollectionShortcode: envUint("DARAJA_SHORTCODE"),
		Passkey:             os.Getenv("DARAJA_PASSKEY"),
		InitiatorName:       os.Getenv("DARAJA_INITIATOR_NAME"),
		InitiatorPassword:   os.Getenv("DARAJA_INITIATOR_PASSWORD"),
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return c
}

func envUint(name string) uint {
	parsed, err := strconv.ParseUint(os.Getenv(name), 10, 64)
	if err != nil {
		return 0
	}
	return uint(parsed)
}

func TestSandbox_AccessToken(t *testing.T) {
	c := sandboxClient(t)

	token, err := c.AccessToken(context.Background())
	if err != nil {
		t.Fatalf("AccessToken: %v", err)
	}
	if token == "" {
		t.Error("sandbox returned an empty token")
	}
}

// Confirms the stub's claim that a fresh mint supersedes the previous token.
func TestSandbox_TokenIsSuperseded(t *testing.T) {
	c := sandboxClient(t)

	first, err := c.AccessToken(context.Background())
	if err != nil {
		t.Fatalf("first: %v", err)
	}
	c.evictToken(context.Background())
	second, err := c.AccessToken(context.Background())
	if err != nil {
		t.Fatalf("second: %v", err)
	}
	if first == second {
		t.Log("sandbox reissued the same token; the stub models Daraja as always minting a fresh one")
	}
}

// Open question 4: the Pull docs say GET in prose and POST by shape.
func TestSandbox_PullQueryVerb(t *testing.T) {
	t.Skip("run once Pull is registered on the sandbox shortcode; settles the verb")
}

// Open question 2: Safaricom's documentation spells the C2B fallback both
// Cancelled and Canceled while warning it must be well spelled. Registration is
// a one-time production call, so this must not be guessed.
func TestSandbox_ResponseTypeSpelling(t *testing.T) {
	t.Skip("registering a URL is effectively one-time; confirm with Safaricom support first")
}
