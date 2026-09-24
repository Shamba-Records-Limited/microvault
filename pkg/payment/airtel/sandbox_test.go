//go:build airtel_sandbox

package airtel

import (
	"context"
	"os"
	"testing"
	"time"
)

// The stub is a second implementation of Airtel's specification, so the
// failure mode this suite exists for is our client and our stub sharing a
// misreading. Nothing else can catch that — and this rail was built from a
// transcription of a login-gated portal, which makes a shared misreading more
// likely here than it was for Daraja.
//
// It is not part of CI. Run it by hand, once per phase:
//
//	go test -tags airtel_sandbox -run Sandbox ./pkg/payment/airtel/...
//
// Credentials come from the environment and are never committed.
func sandboxClient(t *testing.T) *Client {
	t.Helper()

	id, secret := os.Getenv("AIRTEL_CLIENT_ID"), os.Getenv("AIRTEL_CLIENT_SECRET")
	if id == "" || secret == "" {
		t.Skip("set AIRTEL_CLIENT_ID and AIRTEL_CLIENT_SECRET to run the sandbox suite")
	}

	c, err := New(Config{
		Environment:     EnvironmentStaging,
		ClientID:        id,
		ClientSecret:    secret,
		Country:         os.Getenv("AIRTEL_COUNTRY"),
		Currency:        os.Getenv("AIRTEL_CURRENCY"),
		SigningEnabled:  os.Getenv("AIRTEL_SIGNING_ENABLED") == "true",
		CallbackHMACKey: os.Getenv("AIRTEL_CALLBACK_HMAC_KEY"),
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return c
}

func TestSandbox_AccessToken(t *testing.T) {
	c := sandboxClient(t)

	token, err := c.AccessToken(context.Background())
	if err != nil {
		t.Fatalf("AccessToken: %v", err)
	}
	if token == "" {
		t.Fatal("staging returned an empty token")
	}
}

// Confirms the 180 second lifetime the portal documents. If staging returns
// something else, tokenSkew and the poller intervals both need revisiting.
func TestSandbox_TokenLifetime(t *testing.T) {
	c := sandboxClient(t)

	if _, err := c.AccessToken(context.Background()); err != nil {
		t.Fatalf("AccessToken: %v", err)
	}
	_, expiresAt, ok := c.tokens.Get(context.Background(), c.tokenKey())
	if !ok {
		t.Fatal("the token was not cached")
	}

	life := time.Until(expiresAt) + tokenSkew
	t.Logf("staging token lifetime: %s (documented: %ds)", life.Round(time.Second), defaultTokenSeconds)
	if life > 2*defaultTokenSeconds*time.Second {
		t.Errorf("token lives %s, far longer than the documented %ds — tokenSkew is tuned for the short life", life, defaultTokenSeconds)
	}
}

// Open question 1: the portal documents transaction.amount as a number and
// never says whether minor units are accepted. The client sends whole
// shillings; if staging accepts 100 as one shilling rather than one hundred,
// every amount on this rail is out by two orders of magnitude.
//
// It pushes a real prompt to a real handset, so it needs a number someone is
// holding and a tiny amount — the same constraint MPESA_PROMPT_AMOUNT_KES
// exists for on the other rail.
func TestSandbox_AmountUnits(t *testing.T) {
	payer := os.Getenv("AIRTEL_TEST_MSISDN")
	if payer == "" {
		t.Skip("set AIRTEL_TEST_MSISDN to a handset you are holding; this pushes a real prompt")
	}

	c := sandboxClient(t)
	resp, err := c.Payment(context.Background(), PaymentRequest{
		Reference:     "MV-UNITS",
		Payer:         payer,
		AmountKES:     1,
		TransactionID: "mv-units-" + time.Now().UTC().Format("20060102150405"),
	})
	if err != nil {
		t.Fatalf("Payment: %v", err)
	}
	t.Logf("prompt accepted: %+v — confirm on the handset whether it asked for KES 1.00 or KES 0.01", resp.Status)
}

// Open question 2: Account's type parameter is labelled a path parameter but
// the documented path has no placeholder and the sample omits it. The client
// sends nothing; this confirms that is accepted.
func TestSandbox_BalanceWithoutTypeParameter(t *testing.T) {
	c := sandboxClient(t)

	resp, err := c.BalanceEnquiry(context.Background())
	if err != nil {
		t.Fatalf("BalanceEnquiry without a type parameter: %v", err)
	}
	minor, err := resp.Minor()
	if err != nil {
		t.Fatalf("the balance did not parse as a formatted string: %v", err)
	}
	t.Logf("balance: %q → %d minor, currency %q", resp.Data.Balance, minor, resp.Data.Currency)
}

// Open question 3: whether the gateway really is header-casing sensitive.
// Go canonicalises outgoing header names, so the client cannot send
// x-country as typed — which means if the gateway is genuinely case
// sensitive, every lowercase-documented endpoint would fail. Transactions
// Summary is the one this package reaches.
func TestSandbox_LowercaseHeaderEndpoint(t *testing.T) {
	c := sandboxClient(t)

	_, err := c.TransactionsSummary(context.Background(), SummaryRequest{
		From:  time.Now().Add(-24 * time.Hour),
		To:    time.Now(),
		Limit: 1,
	})
	if err != nil {
		t.Fatalf("Transactions Summary rejected canonicalised headers — the casing is real and needs a transport-level fix: %v", err)
	}
}

// Confirms the pre-flight reads the two fields it decides on.
func TestSandbox_UserEnquiry(t *testing.T) {
	msisdn := os.Getenv("AIRTEL_TEST_MSISDN")
	if msisdn == "" {
		t.Skip("set AIRTEL_TEST_MSISDN to run the user enquiry")
	}

	c := sandboxClient(t)
	resp, err := c.UserEnquiry(context.Background(), msisdn)
	if err != nil {
		t.Fatalf("UserEnquiry: %v", err)
	}
	t.Logf("barred=%v pin_set=%v registration=%q can_transact=%v",
		resp.Data.IsBarred, resp.Data.IsPINSet, resp.Data.Registration.Status, resp.CanTransact())
}

// Settles which reading of "the callback body" Airtel signs. It needs a
// delivered callback, so it is driven by hand once a tunnel is pointed at a
// recorder.
func TestSandbox_CallbackHashVariant(t *testing.T) {
	raw := os.Getenv("AIRTEL_CAPTURED_CALLBACK")
	if raw == "" {
		t.Skip("set AIRTEL_CAPTURED_CALLBACK to a callback body captured from staging")
	}

	key := os.Getenv("AIRTEL_CALLBACK_HMAC_KEY")
	if key == "" {
		t.Skip("set AIRTEL_CALLBACK_HMAC_KEY to the key from Application Settings")
	}

	result, err := VerifyCallbackHash([]byte(raw), key)
	if err != nil {
		t.Fatalf("VerifyCallbackHash: %v", err)
	}
	if !result.Verified {
		t.Fatal("no candidate rendering matched — Airtel signs something else; capture the body and widen the variants")
	}
	t.Logf("Airtel signs the %q rendering; record it in the vault and narrow VerifyCallbackHash to it", result.Variant)
}
