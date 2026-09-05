package mpesa

import (
	"context"
	"strings"
	"testing"

	"github.com/Shamba-Records-Limited/microvault/pkg/notifications"
	"github.com/Shamba-Records-Limited/microvault/pkg/payment/mpesa/darajastub"
)

func expressClient(t *testing.T) (*Client, *darajastub.Stub) {
	t.Helper()
	stub := darajastub.New(t,
		darajastub.WithConsumerCredentials("k", "s"),
		darajastub.WithPasskey("stub-passkey"),
	)
	c, err := New(Config{
		Environment:         EnvironmentSandbox,
		ConsumerKey:         "k",
		ConsumerSecret:      "s",
		CollectionShortcode: 174379,
		Passkey:             "stub-passkey",
		BaseURL:             stub.URL(),
		Certificate:         stub.Certificate(),
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return c, stub
}

func validExpress() ExpressRequest {
	return ExpressRequest{
		AmountKES:        150,
		Payer:            "0712345678",
		CallbackURL:      "https://example.test/callbacks/daraja/abc/stk",
		AccountReference: "MV7K3QA9F",
		TransactionDesc:  "Loan repay",
	}
}

// The password is base64(shortcode+passkey+timestamp) and the timestamp is sent
// alongside it, so both must come from one reading of the clock. The stub
// recomputes the password, so a second read fails here.
func TestExpress_PromptAccepted(t *testing.T) {
	c, stub := expressClient(t)

	resp, err := c.Express(context.Background(), validExpress())
	if err != nil {
		t.Fatalf("Express: %v", err)
	}
	if resp.CheckoutRequestID == "" || resp.ResponseCode != "0" {
		t.Errorf("response = %+v", resp)
	}

	checkouts := stub.Checkouts()
	if len(checkouts) != 1 {
		t.Fatalf("stub saw %d checkouts", len(checkouts))
	}
	if checkouts[0].Payer != "254712345678" {
		t.Errorf("payer normalised to %q", checkouts[0].Payer)
	}
	if checkouts[0].AmountMinor != 15_000 {
		t.Errorf("amount = %d minor units", checkouts[0].AmountMinor)
	}
}

func TestExpress_LocalValidationRejectsBeforeCalling(t *testing.T) {
	cases := map[string]func(*ExpressRequest){
		"reference too long":   func(r *ExpressRequest) { r.AccountReference = "THIRTEENCHARS" },
		"reference missing":    func(r *ExpressRequest) { r.AccountReference = "" },
		"description too long": func(r *ExpressRequest) { r.TransactionDesc = "FOURTEENCHARSX" },
		"amount zero":          func(r *ExpressRequest) { r.AmountKES = 0 },
		"amount negative":      func(r *ExpressRequest) { r.AmountKES = -1 },
		"callback missing":     func(r *ExpressRequest) { r.CallbackURL = "" },
		"callback blocked":     func(r *ExpressRequest) { r.CallbackURL = "https://x.test/webhooks/mpesa/stk" },
		"callback relative":    func(r *ExpressRequest) { r.CallbackURL = "/callbacks/daraja" },
		"payer unusable":       func(r *ExpressRequest) { r.Payer = "12" },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			c, stub := expressClient(t)
			req := validExpress()
			mutate(&req)

			if _, err := c.Express(context.Background(), req); err == nil {
				t.Fatal("expected a local validation error")
			}
			if got := len(stub.Checkouts()); got != 0 {
				t.Errorf("a rejected request still reached Daraja: %d checkouts", got)
			}
		})
	}
}

// Exactly twelve characters is legal; thirteen is not. The boundary matters
// because the loan reference format was chosen to sit under it.
func TestExpress_AccountReferenceBoundary(t *testing.T) {
	c, _ := expressClient(t)

	req := validExpress()
	req.AccountReference = strings.Repeat("A", 12)
	if _, err := c.Express(context.Background(), req); err != nil {
		t.Errorf("twelve characters was rejected: %v", err)
	}

	req.AccountReference = strings.Repeat("A", 13)
	if _, err := c.Express(context.Background(), req); err == nil {
		t.Error("thirteen characters was accepted")
	}
}

func TestExpress_SuccessCallback(t *testing.T) {
	c, stub := expressClient(t)
	received := make(chan []byte, 4)
	target := callbackTarget(t, received)

	req := validExpress()
	req.CallbackURL = target
	resp, err := c.Express(context.Background(), req)
	if err != nil {
		t.Fatalf("Express: %v", err)
	}

	stub.CompleteSTK(resp.CheckoutRequestID)
	if stub.Pending() != 1 {
		t.Fatalf("Pending() = %d, want 1", stub.Pending())
	}
	stub.Deliver()

	callback, err := ParseExpressCallback(<-received)
	if err != nil {
		t.Fatalf("ParseExpressCallback: %v", err)
	}
	if !callback.Succeeded() || callback.ReceiptNumber == "" {
		t.Errorf("callback = %+v", callback)
	}
	if callback.AmountKES != 150 || callback.AmountMinor != 15_000 {
		t.Errorf("amount = %d KES / %d minor", callback.AmountKES, callback.AmountMinor)
	}
	if callback.Payer != "254712345678" {
		t.Errorf("payer = %q", callback.Payer)
	}
	if callback.CompletedAt.IsZero() {
		t.Error("TransactionDate did not parse")
	}
	if got := stub.Balance(174379, darajastub.AccountUtility); got != 15_000 {
		t.Errorf("utility balance = %d, want 15000", got)
	}
}

// CallbackMetadata is absent entirely when the customer did not pay, which
// breaks any decoder that assumes it is always there.
func TestExpress_CancelledCallbackHasNoMetadata(t *testing.T) {
	c, stub := expressClient(t)
	received := make(chan []byte, 4)

	req := validExpress()
	req.CallbackURL = callbackTarget(t, received)
	resp, _ := c.Express(context.Background(), req)

	stub.CancelSTK(resp.CheckoutRequestID)
	stub.Deliver()

	callback, err := ParseExpressCallback(<-received)
	if err != nil {
		t.Fatalf("ParseExpressCallback: %v", err)
	}
	if callback.Succeeded() || callback.ResultCode != 1032 {
		t.Errorf("callback = %+v", callback)
	}
	if callback.ReceiptNumber != "" || callback.AmountKES != 0 {
		t.Errorf("failed callback carried payment detail: %+v", callback)
	}
	if got := stub.Balance(174379, darajastub.AccountUtility); got != 0 {
		t.Errorf("a cancelled prompt moved the ledger to %d", got)
	}
}

// Daraja errors rather than reporting "pending" while a prompt is live. The
// poller must read that as "not yet resolved", so this asserts the shape the
// poller will depend on.
func TestExpressQuery_PendingErrorsAndResolvedSucceeds(t *testing.T) {
	c, stub := expressClient(t)
	received := make(chan []byte, 4)

	req := validExpress()
	req.CallbackURL = callbackTarget(t, received)
	resp, _ := c.Express(context.Background(), req)

	if _, err := c.ExpressQuery(context.Background(), resp.CheckoutRequestID, 0); err == nil {
		t.Error("a pending checkout did not error, so the poller contract has changed")
	}

	stub.CompleteSTK(resp.CheckoutRequestID)
	query, err := c.ExpressQuery(context.Background(), resp.CheckoutRequestID, 0)
	if err != nil {
		t.Fatalf("ExpressQuery after resolution: %v", err)
	}
	if query.ResultCode.Int64() != 0 {
		t.Errorf("result code = %d", query.ResultCode.Int64())
	}
}

func TestExpressQuery_Validation(t *testing.T) {
	c, _ := expressClient(t)
	if _, err := c.ExpressQuery(context.Background(), "", 0); err == nil {
		t.Error("expected an error for an empty checkout ID")
	}
}

func TestExpressOutcomeFor(t *testing.T) {
	cases := map[int64]struct{ retryable, operational bool }{
		0:    {false, false},
		1:    {true, false},
		2:    {false, false},
		3:    {false, false},
		4:    {true, false},
		8:    {true, false},
		17:   {true, false},
		1019: {true, false},
		1025: {false, true},
		1032: {true, false},
		1037: {true, false},
		2001: {true, false},
		2028: {false, true},
		8006: {false, true},
	}
	for code, want := range cases {
		got := ExpressOutcomeFor(code)
		if got.Retryable != want.retryable || got.Operational != want.operational {
			t.Errorf("code %d: retryable %v, operational %v; want %v, %v",
				code, got.Retryable, got.Operational, want.retryable, want.operational)
		}
		if got.Message == "" {
			t.Errorf("code %d has no borrower-facing message", code)
		}
	}

	// An undocumented code must not be blamed on the borrower, and must not be
	// retried blindly.
	unknown := ExpressOutcomeFor(999999)
	if unknown.Retryable || !unknown.Operational {
		t.Errorf("unknown code = %+v", unknown)
	}
}

// Outcome messages reach feature phones over USSD and SMS, where a non-GSM
// character is invisible in the simulator and breaks on a real handset.
func TestExpressOutcomes_AreGSM7Safe(t *testing.T) {
	for code, outcome := range expressOutcomes {
		if _, bad, ok := notifications.GSM7Len(outcome.Message); !ok {
			t.Errorf("code %d message contains non-GSM character %q: %s", code, bad, outcome.Message)
		}
	}
	if _, bad, ok := notifications.GSM7Len(ExpressOutcomeFor(999999).Message); !ok {
		t.Errorf("fallback message contains non-GSM character %q", bad)
	}
}

func TestAssertCallbackURL(t *testing.T) {
	// The obvious route for this integration contains a blocked word.
	for _, blocked := range []string{
		"https://x.test/api/v1/webhooks/mpesa/confirmation",
		"https://x.test/safaricom/result",
		"https://x.test/query",
		"https://x.test/cmd",
		"https://x.test/exec",
		"https://x.test/sql",
	} {
		if err := AssertCallbackURL(blocked); err == nil {
			t.Errorf("%s was accepted", blocked)
		}
	}
	for _, allowed := range []string{
		"https://x.test/callbacks/daraja/abc/stk",
		"http://localhost:3000/callbacks/daraja/abc/result",
	} {
		if err := AssertCallbackURL(allowed); err != nil {
			t.Errorf("%s was rejected: %v", allowed, err)
		}
	}
	if err := AssertCallbackURL("not-a-url"); err == nil {
		t.Error("a relative URL was accepted")
	}
}

func TestNormalizeMSISDN(t *testing.T) {
	cases := map[string]string{
		"254712345678":     "254712345678",
		"+254712345678":    "254712345678",
		"0712345678":       "254712345678",
		"712345678":        "254712345678",
		"+254 712 345 678": "254712345678",
		"254110000000":     "254110000000",
		"0110000000":       "254110000000",
	}
	for input, want := range cases {
		got, err := NormalizeMSISDN(input)
		if err != nil {
			t.Errorf("NormalizeMSISDN(%q): %v", input, err)
			continue
		}
		if got != want {
			t.Errorf("NormalizeMSISDN(%q) = %q, want %q", input, got, want)
		}
	}
	for _, bad := range []string{"", "   ", "12", "1234567890123456"} {
		if _, err := NormalizeMSISDN(bad); err == nil {
			t.Errorf("NormalizeMSISDN(%q) did not fail", bad)
		}
	}
}

func TestMaskedMSISDN(t *testing.T) {
	masked := MaskedMSISDN("2547 ***** 126")
	if !masked.Masked() || masked.String() != "2547 ***** 126" {
		t.Errorf("masked = %q", masked)
	}
	if MaskedMSISDN("254712345678").Masked() {
		t.Error("an unmasked value reported itself masked")
	}
}
