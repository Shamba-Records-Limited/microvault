package airtel

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/Shamba-Records-Limited/microvault/pkg/payment/airtel/airtelstub"
)

// newStubbed builds a client pointed at a fresh stub.
func newStubbed(t *testing.T, opts ...airtelstub.Option) (*Client, *airtelstub.Stub) {
	t.Helper()

	stub := airtelstub.New(t, opts...)
	client, err := New(Config{
		Environment:     EnvironmentStaging,
		ClientID:        "client-id",
		ClientSecret:    "client-secret",
		BaseURL:         stub.URL(),
		CallbackHMACKey: stub.HMACKey(),
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return client, stub
}

func validPayment() PaymentRequest {
	return PaymentRequest{
		Reference:     "MV-LOAN-1",
		Payer:         "0733123456",
		AmountKES:     500,
		TransactionID: "mv-txn-1",
	}
}

func TestPayment_Succeeds(t *testing.T) {
	client, stub := newStubbed(t)

	resp, err := client.Payment(context.Background(), validPayment())
	if err != nil {
		t.Fatalf("Payment: %v", err)
	}
	if !resp.Accepted() {
		t.Fatalf("Payment was not accepted: %+v", resp.Status)
	}

	txn, ok := stub.Transaction("mv-txn-1")
	if !ok {
		t.Fatal("the stub holds no transaction for the id we sent")
	}
	if txn.MSISDN != "733123456" {
		t.Fatalf("msisdn on the wire = %q, want the national form", txn.MSISDN)
	}
	if txn.AmountKES != 500 {
		t.Fatalf("amount on the wire = %d, want 500", txn.AmountKES)
	}
}

func TestPayment_RejectsCountryCodedMSISDN(t *testing.T) {
	client, _ := newStubbed(t)

	req := validPayment()
	req.Payer = "+254733123456"

	if _, err := client.Payment(context.Background(), req); err != nil {
		t.Fatalf("Payment: %v", err)
	}
	// The client normalises rather than refusing; the guard that matters is
	// that the stub, which rejects a country code outright, accepted it.
}

func TestPayment_Validation(t *testing.T) {
	client, _ := newStubbed(t)

	cases := map[string]func(*PaymentRequest){
		"no transaction id": func(r *PaymentRequest) { r.TransactionID = "" },
		"no reference":      func(r *PaymentRequest) { r.Reference = "" },
		"zero amount":       func(r *PaymentRequest) { r.AmountKES = 0 },
		"negative amount":   func(r *PaymentRequest) { r.AmountKES = -1 },
		"long reference":    func(r *PaymentRequest) { r.Reference = "0123456789012345678901234567890" },
		"bad payer":         func(r *PaymentRequest) { r.Payer = "12345" },
	}

	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			req := validPayment()
			mutate(&req)
			if _, err := client.Payment(context.Background(), req); err == nil {
				t.Fatal("expected the request to be refused before the wire")
			}
		})
	}
}

// Resending an id Airtel already holds is a duplicate-transaction error, not
// a second payment. That is the behaviour that makes enquiring safe.
func TestPayment_DuplicateTransactionID(t *testing.T) {
	client, _ := newStubbed(t)

	if _, err := client.Payment(context.Background(), validPayment()); err != nil {
		t.Fatalf("first Payment: %v", err)
	}

	_, err := client.Payment(context.Background(), validPayment())
	if err == nil {
		t.Fatal("expected the duplicate id to be refused")
	}

	var apiErr *AirtelError
	if !errors.As(err, &apiErr) {
		t.Fatalf("error is not an *AirtelError: %v", err)
	}
	if apiErr.ResponseCode() != CodeESBDuplicateExtID {
		t.Fatalf("response code = %q, want %q", apiErr.ResponseCode(), CodeESBDuplicateExtID)
	}
}

// An ambiguous acknowledgement is not an error. A caller handed an error has
// nothing to enquire about, and the documented response is to enquire.
func TestPayment_AmbiguousIsNotAnError(t *testing.T) {
	client, stub := newStubbed(t)
	stub.AmbiguousNext()

	resp, err := client.Payment(context.Background(), validPayment())
	if err != nil {
		t.Fatalf("an ambiguous payment must not surface as an error: %v", err)
	}
	if !resp.Pending() {
		t.Fatalf("Pending() = false for %q", resp.ResponseCode())
	}
	if !resp.Outcome().ShouldEnquire() {
		t.Fatalf("outcome for %q does not ask for an enquiry", resp.ResponseCode())
	}
	if resp.Outcome().Retryable {
		t.Fatal("an ambiguous payment must never be marked retryable")
	}
}

// The receipt exists only on success. A consumer reading it unconditionally
// must fail here rather than in production.
func TestEnquiry_ReceiptOnlyOnSuccess(t *testing.T) {
	cases := map[string]struct {
		status      airtelstub.TransactionStatus
		code        string
		wantReceipt bool

		// acknowledgedFails marks the outcomes Airtel reports as a failed
		// payment on the acknowledgement itself. The transaction still
		// exists and is still enquirable, which is what this asserts.
		acknowledgedFails bool
	}{
		"settled":     {status: airtelstub.StatusSuccess, code: "DP00800001001", wantReceipt: true},
		"in progress": {status: airtelstub.StatusInProgress, code: "DP00800001006"},
		"failed":      {status: airtelstub.StatusFailed, code: "DP00800001008", acknowledgedFails: true},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			client, stub := newStubbed(t)
			stub.NextOutcome(tc.status, tc.code)

			_, err := client.Payment(context.Background(), validPayment())
			switch {
			case tc.acknowledgedFails && err == nil:
				t.Fatalf("a %s acknowledgement must surface as an error", tc.code)
			case !tc.acknowledgedFails && err != nil:
				t.Fatalf("Payment: %v", err)
			}

			resp, err := client.Enquiry(context.Background(), "mv-txn-1")
			if err != nil {
				t.Fatalf("Enquiry: %v", err)
			}

			receipt, ok := resp.Receipt()
			if ok != tc.wantReceipt {
				t.Fatalf("Receipt() ok = %v, want %v (receipt %q)", ok, tc.wantReceipt, receipt)
			}
			if !tc.wantReceipt && receipt != "" {
				t.Fatalf("a non-success enquiry disclosed a receipt: %q", receipt)
			}
		})
	}
}

// A refund is keyed by Airtel's id, an enquiry by ours. A refund is therefore
// impossible until an enquiry or a callback has disclosed the receipt.
func TestRefund_KeyedByAirtelMoneyID(t *testing.T) {
	client, _ := newStubbed(t)

	if _, err := client.Refund(context.Background(), ""); err == nil {
		t.Fatal("a refund without a receipt must be refused before the wire")
	}

	if _, err := client.Payment(context.Background(), validPayment()); err != nil {
		t.Fatalf("Payment: %v", err)
	}
	enquiry, err := client.Enquiry(context.Background(), "mv-txn-1")
	if err != nil {
		t.Fatalf("Enquiry: %v", err)
	}
	receipt, ok := enquiry.Receipt()
	if !ok {
		t.Fatal("a settled transaction disclosed no receipt")
	}

	// Our own id is not a refund key, even though it is the enquiry key.
	if _, err := client.Refund(context.Background(), "mv-txn-1"); err == nil {
		t.Fatal("refunding by the partner id must fail")
	}

	resp, err := client.Refund(context.Background(), receipt)
	if err != nil {
		t.Fatalf("Refund: %v", err)
	}
	if resp.Data.Transaction.AirtelMoneyID != receipt {
		t.Fatalf("refund echoed %q, want %q", resp.Data.Transaction.AirtelMoneyID, receipt)
	}
}

func TestEnquiry_RequiresTransactionID(t *testing.T) {
	client, _ := newStubbed(t)
	if _, err := client.Enquiry(context.Background(), ""); err == nil {
		t.Fatal("an enquiry with no id must be refused before the wire")
	}
}

// A gateway timeout is an instruction to enquire, not to retry.
func TestPayment_GatewayTimeoutClassifies(t *testing.T) {
	client, stub := newStubbed(t)
	stub.TimeoutNext(airtelstub.RoutePayment)

	_, err := client.Payment(context.Background(), validPayment())
	if err == nil {
		t.Fatal("expected the timeout to surface")
	}

	var apiErr *AirtelError
	if !errors.As(err, &apiErr) {
		t.Fatalf("error is not an *AirtelError: %v", err)
	}
	if apiErr.StatusCode != http.StatusGatewayTimeout {
		t.Fatalf("status = %d, want %d", apiErr.StatusCode, http.StatusGatewayTimeout)
	}
	if !OutcomeFor(apiErr.ResponseCode()).ShouldEnquire() {
		t.Fatalf("a %s timeout does not ask for an enquiry", apiErr.ResponseCode())
	}
}

func TestEnquiryFloor(t *testing.T) {
	if EnquiryFloor != 3*time.Minute {
		t.Fatalf("EnquiryFloor = %s, want the documented three minutes", EnquiryFloor)
	}
}
