package mpesa

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Shamba-Records-Limited/microvault/pkg/payment/mpesa/darajastub"
)

const testShortcode = 174379

func c2bClient(t *testing.T) (*Client, *darajastub.Stub) {
	t.Helper()
	stub := darajastub.New(t, darajastub.WithConsumerCredentials("k", "s"))
	c, err := New(Config{
		Environment: EnvironmentSandbox, ConsumerKey: "k", ConsumerSecret: "s",
		CollectionShortcode: testShortcode, Passkey: "stub-passkey",
		BaseURL: stub.URL(), Certificate: stub.Certificate(),
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return c, stub
}

// validator stands in for our validation controller, which is wiring and does
// not exist yet.
type validator struct {
	url       string
	answer    ValidationResponse
	seen      []*C2BNotification
	unhealthy bool
}

func newValidator(t *testing.T, answer ValidationResponse) *validator {
	t.Helper()
	v := &validator{answer: answer}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		notification, err := ParseC2BNotification(body)
		if err != nil {
			t.Errorf("validator: %v", err)
			return
		}
		v.seen = append(v.seen, notification)
		if v.unhealthy {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		_ = json.NewEncoder(w).Encode(v.answer)
	}))
	t.Cleanup(srv.Close)
	v.url = srv.URL + "/callbacks/daraja/abc/validation"
	return v
}

func register(t *testing.T, c *Client, responseType ResponseType, validationURL, confirmationURL string) {
	t.Helper()
	if _, err := c.RegisterURL(context.Background(), RegisterURLRequest{
		ResponseType:    responseType,
		ValidationURL:   validationURL,
		ConfirmationURL: confirmationURL,
	}); err != nil {
		t.Fatalf("RegisterURL: %v", err)
	}
}

func TestRegisterURL(t *testing.T) {
	c, stub := c2bClient(t)
	register(t, c, ResponseTypeCancelled, "https://x.test/callbacks/daraja/abc/validation", "https://x.test/callbacks/daraja/abc/confirmation")

	validation, confirmation, ok := stub.RegisteredURLs(testShortcode)
	if !ok || validation == "" || confirmation == "" {
		t.Errorf("registered = %q, %q, %v", validation, confirmation, ok)
	}
}

// Registration is effectively one-time in production; a second attempt is a
// duplicate rather than an overwrite.
func TestRegisterURL_SecondAttemptIsRejected(t *testing.T) {
	c, _ := c2bClient(t)
	register(t, c, ResponseTypeCancelled, "", "https://x.test/callbacks/daraja/abc/confirmation")

	_, err := c.RegisterURL(context.Background(), RegisterURLRequest{
		ResponseType:    ResponseTypeCancelled,
		ConfirmationURL: "https://x.test/callbacks/daraja/abc/confirmation",
	})
	if err == nil {
		t.Fatal("a second registration was accepted")
	}
}

func TestRegisterURL_LocalValidation(t *testing.T) {
	cases := map[string]RegisterURLRequest{
		"no response type":  {ConfirmationURL: "https://x.test/callbacks/daraja/c"},
		"bad response type": {ResponseType: "Maybe", ConfirmationURL: "https://x.test/callbacks/daraja/c"},
		"no confirmation":   {ResponseType: ResponseTypeCompleted},
		"blocked word": {
			ResponseType:    ResponseTypeCompleted,
			ConfirmationURL: "https://x.test/webhooks/mpesa/confirmation",
		},
		"blocked validation": {
			ResponseType:    ResponseTypeCompleted,
			ConfirmationURL: "https://x.test/callbacks/daraja/c",
			ValidationURL:   "https://x.test/callbacks/daraja/query",
		},
	}
	for name, req := range cases {
		t.Run(name, func(t *testing.T) {
			c, stub := c2bClient(t)
			if _, err := c.RegisterURL(context.Background(), req); err == nil {
				t.Fatal("expected a local validation error")
			}
			if _, _, ok := stub.RegisteredURLs(testShortcode); ok {
				t.Error("a rejected registration still reached Daraja")
			}
		})
	}
}

func TestC2B_AcceptedPaymentConfirms(t *testing.T) {
	c, stub := c2bClient(t)
	received := make(chan []byte, 4)
	confirmation := callbackTarget(t, received)
	v := newValidator(t, AcceptPayment("our-ref-1"))
	register(t, c, ResponseTypeCancelled, v.url, confirmation)

	if _, err := c.Simulate(context.Background(), SimulateRequest{
		AmountKES: 500, Payer: "254712345678", BillRefNumber: "MV7K3QA9F",
	}); err != nil {
		t.Fatalf("Simulate: %v", err)
	}
	stub.Deliver()

	if len(v.seen) != 1 {
		t.Fatalf("validator saw %d requests", len(v.seen))
	}
	if v.seen[0].BillRefNumber != "MV7K3QA9F" {
		t.Errorf("validation BillRefNumber = %q", v.seen[0].BillRefNumber)
	}

	notification, err := ParseC2BNotification(<-received)
	if err != nil {
		t.Fatalf("ParseC2BNotification: %v", err)
	}
	if notification.TransAmountMinor != 50_000 {
		t.Errorf("amount = %d minor units", notification.TransAmountMinor)
	}
	if notification.ThirdPartyTransID != "our-ref-1" {
		t.Errorf("ThirdPartyTransID = %q; it did not survive validation to confirmation", notification.ThirdPartyTransID)
	}
	if !notification.MSISDN.Masked() {
		t.Errorf("MSISDN %q is not masked; C2B v2 always masks it", notification.MSISDN)
	}
	if got := stub.Balance(testShortcode, darajastub.AccountUtility); got != 50_000 {
		t.Errorf("utility balance = %d", got)
	}
}

// A rejected payment must move nothing and confirm nothing.
func TestC2B_RejectedPaymentDoesNotConfirm(t *testing.T) {
	c, stub := c2bClient(t)
	received := make(chan []byte, 4)
	v := newValidator(t, RejectPayment(ValidationInvalidAccountNumber))
	register(t, c, ResponseTypeCancelled, v.url, callbackTarget(t, received))

	if _, err := c.Simulate(context.Background(), SimulateRequest{
		AmountKES: 500, Payer: "254712345678", BillRefNumber: "NOTOURS",
	}); err != nil {
		t.Fatalf("Simulate: %v", err)
	}

	if stub.Pending() != 0 {
		t.Errorf("a rejected payment queued %d confirmations", stub.Pending())
	}
	if got := stub.Balance(testShortcode, darajastub.AccountUtility); got != 0 {
		t.Errorf("a rejected payment moved the ledger to %d", got)
	}
}

// ResponseType decides what happens during an outage of ours, and the two
// settings have opposite consequences: Completed accepts money we cannot
// attribute, Cancelled refuses money we could have collected.
func TestC2B_ResponseTypeGovernsValidationOutage(t *testing.T) {
	for _, tc := range []struct {
		responseType ResponseType
		wantBalance  int64
	}{
		{ResponseTypeCompleted, 50_000},
		{ResponseTypeCancelled, 0},
	} {
		t.Run(string(tc.responseType), func(t *testing.T) {
			c, stub := c2bClient(t)
			received := make(chan []byte, 4)
			v := newValidator(t, AcceptPayment(""))
			v.unhealthy = true
			register(t, c, tc.responseType, v.url, callbackTarget(t, received))

			if _, err := c.Simulate(context.Background(), SimulateRequest{
				AmountKES: 500, Payer: "254712345678", BillRefNumber: "MV7K3QA9F",
			}); err != nil {
				t.Fatalf("Simulate: %v", err)
			}
			stub.Deliver()

			if got := stub.Balance(testShortcode, darajastub.AccountUtility); got != tc.wantBalance {
				t.Errorf("balance = %d, want %d", got, tc.wantBalance)
			}
		})
	}
}

func TestSimulate_RefusedInProduction(t *testing.T) {
	c, err := New(Config{
		Environment: EnvironmentProduction, ConsumerKey: "k", ConsumerSecret: "s",
		CollectionShortcode: testShortcode,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := c.Simulate(context.Background(), SimulateRequest{AmountKES: 1, Payer: "254712345678"}); err == nil {
		t.Error("simulation was allowed in production")
	}
}

func TestSimulate_LocalValidation(t *testing.T) {
	c, _ := c2bClient(t)
	if _, err := c.Simulate(context.Background(), SimulateRequest{AmountKES: 0, Payer: "254712345678"}); err == nil {
		t.Error("a zero amount was accepted")
	}
	if _, err := c.Simulate(context.Background(), SimulateRequest{AmountKES: 5, Payer: "nope"}); err == nil {
		t.Error("an unusable payer was accepted")
	}
}

func TestParseC2BNotification(t *testing.T) {
	raw := []byte(`{
	  "TransactionType": "Pay Bill", "TransID": "RKL51ZDR4F", "TransTime": "20231121121325",
	  "TransAmount": "5.00", "BusinessShortCode": "600966", "BillRefNumber": " MV7K3QA9F ",
	  "InvoiceNumber": "", "OrgAccountBalance": "25.00", "ThirdPartyTransID": "",
	  "MSISDN": "2547 ***** 126", "FirstName": "NICHOLAS", "MiddleName": "", "LastName": ""
	}`)

	notification, err := ParseC2BNotification(raw)
	if err != nil {
		t.Fatalf("ParseC2BNotification: %v", err)
	}
	if notification.TransAmountMinor != 500 || notification.OrgAccountBalanceMinor != 2500 {
		t.Errorf("amounts = %d, %d", notification.TransAmountMinor, notification.OrgAccountBalanceMinor)
	}
	// The payer types the reference on a handset, so it arrives with whatever
	// spacing they left.
	if notification.BillRefNumber != "MV7K3QA9F" {
		t.Errorf("BillRefNumber = %q, want it trimmed", notification.BillRefNumber)
	}
	if _, ok := notification.PaidAt(); !ok {
		t.Error("TransTime did not parse")
	}
	if !notification.MSISDN.Masked() {
		t.Error("MSISDN did not report itself masked")
	}

	if _, err := ParseC2BNotification([]byte(`{bad`)); err == nil {
		t.Error("expected a decode error")
	}
}

// A validation response with code 0 accepts the payment, so a rejection that
// forgets its code would accept money it meant to refuse.
func TestRejectPayment_NeverAccepts(t *testing.T) {
	if got := RejectPayment(ValidationAccepted); got.ResultCode == ValidationAccepted {
		t.Errorf("RejectPayment(0) produced %+v", got)
	}
	if got := RejectPayment(ValidationInvalidAccountNumber); got.ResultCode != ValidationInvalidAccountNumber || got.ResultDesc != "Rejected" {
		t.Errorf("RejectPayment = %+v", got)
	}
	if got := AcceptPayment("x"); got.ResultCode != ValidationAccepted || got.ThirdPartyTransID != "x" {
		t.Errorf("AcceptPayment = %+v", got)
	}
}
