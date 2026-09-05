package mpesa

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/Shamba-Records-Limited/microvault/pkg/payment/mpesa/darajastub"
)

// Daraja signs nothing, so a payload an attacker wrote parses exactly as well
// as one Safaricom sent. This test exists to make that explicit rather than
// implicit: nothing in this package can tell the two apart, and a caller that
// credits on a parse alone credits forgeries.
func TestCallbackIsNotEvidence(t *testing.T) {
	genuine := []byte(`{"TransactionType":"Pay Bill","TransID":"RKL51ZDR4F","TransTime":"20231121121325",
		"TransAmount":"5000.00","BusinessShortCode":"174379","BillRefNumber":"MV7K3QA9F",
		"MSISDN":"2547 ***** 126","FirstName":"NICHOLAS"}`)
	forged := []byte(`{"TransactionType":"Pay Bill","TransID":"FORGED0001","TransTime":"20231121121325",
		"TransAmount":"999999.00","BusinessShortCode":"174379","BillRefNumber":"MV7K3QA9F",
		"MSISDN":"2547 ***** 126","FirstName":"NICHOLAS"}`)

	authentic, err := ParseC2BNotification(genuine)
	if err != nil {
		t.Fatalf("genuine: %v", err)
	}
	fake, err := ParseC2BNotification(forged)
	if err != nil {
		t.Fatalf("forged: %v", err)
	}

	if authentic.TransAmountMinor == 0 || fake.TransAmountMinor == 0 {
		t.Fatal("one of the payloads failed to decode")
	}
	// If this ever starts differing, someone has added an authenticity signal
	// and the confirm-before-credit rule can be revisited.
	if (authentic.TransID == "") != (fake.TransID == "") {
		t.Error("the parser distinguished a forgery, which it has no way to do")
	}
}

// Daraja repeats callbacks. Parsing is pure, so a replay produces identical
// values and cannot be the thing that stops a double credit — that has to be
// the receipt's uniqueness.
func TestDuplicateCallbacksAreIndistinguishable(t *testing.T) {
	c, stub := c2bClient(t)
	received := make(chan []byte, 8)
	stub.DuplicateCallbacks()

	v := newValidator(t, AcceptPayment("ref-1"))
	register(t, c, ResponseTypeCancelled, v.url, callbackTarget(t, received))

	if _, err := c.Simulate(context.Background(), SimulateRequest{
		AmountKES: 500, Payer: "254712345678", BillRefNumber: "MV7K3QA9F",
	}); err != nil {
		t.Fatalf("Simulate: %v", err)
	}
	stub.Deliver()

	first, err := ParseC2BNotification(<-received)
	if err != nil {
		t.Fatalf("first: %v", err)
	}
	second, err := ParseC2BNotification(<-received)
	if err != nil {
		t.Fatalf("second: %v", err)
	}
	if first.TransID != second.TransID || first.TransAmountMinor != second.TransAmountMinor {
		t.Fatal("a duplicate delivery differed from the original")
	}
	if first.TransID == "" {
		t.Fatal("no receipt to deduplicate on")
	}
	// The receipt is the only field that can carry idempotency downstream.
	if got := stub.Balance(testShortcode, darajastub.AccountUtility); got != 50_000 {
		t.Errorf("the duplicate moved the ledger twice: %d", got)
	}
}

// A Daraja-side failure must surface as an error and leave nothing half-done.
func TestFaultInjection_SynchronousFailure(t *testing.T) {
	c, stub := expressClient(t)
	stub.FailNext(darajastub.RouteExpress, http.StatusInternalServerError, "500.003.02",
		"Error Occurred: Spike Arrest Violation")

	if _, err := c.Express(context.Background(), validExpress()); err == nil {
		t.Fatal("expected the injected failure to surface")
	}
	if got := len(stub.Checkouts()); got != 0 {
		t.Errorf("a failed request left %d checkouts behind", got)
	}
}

// A dropped response is ambiguous: the request may or may not have been
// accepted. It must surface as an error, and callers must not read it as a
// failure, because retrying an accepted request is how money moves twice.
func TestFaultInjection_DroppedResponseIsAmbiguous(t *testing.T) {
	c, stub := expressClient(t)
	stub.DropNext(darajastub.RouteExpress)

	if _, err := c.Express(context.Background(), validExpress()); err == nil {
		t.Fatal("a dropped response did not surface as an error")
	}
}

// The offset walk must terminate against a Daraja that ignores it, rather than
// looping until something else times out.
func TestPullAll_TerminatesOnStuckOffset(t *testing.T) {
	stub := darajastub.New(t, darajastub.WithPullPageSize(2), darajastub.WithPullStuckOffset())
	c, err := New(Config{
		Environment: EnvironmentSandbox, ConsumerKey: "k", ConsumerSecret: "s",
		CollectionShortcode: testShortcode, BaseURL: stub.URL(), Certificate: stub.Certificate(),
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	received := make(chan []byte, 16)
	if _, err := c.RegisterURL(context.Background(), RegisterURLRequest{
		ResponseType: ResponseTypeCompleted, ConfirmationURL: callbackTarget(t, received),
	}); err != nil {
		t.Fatalf("RegisterURL: %v", err)
	}
	for range 3 {
		stub.SimulateC2B(testShortcode, "254712345678", "MV7K3QA9F", 1_000)
	}
	stub.Deliver()

	done := make(chan error, 1)
	go func() {
		_, err := c.PullAll(context.Background(), time.Now().Add(-time.Hour), time.Now(), 0)
		done <- err
	}()

	select {
	case err := <-done:
		if err == nil {
			t.Error("a non-advancing offset was reported as a successful walk")
		}
	case <-time.After(30 * time.Second):
		t.Fatal("PullAll did not terminate against a stuck offset")
	}
}

// The poller races the callback. DeliverNext lets a test hold the notification
// back and query first, which is the ordering the poller exists to survive.
func TestPollerRace_QueryBeforeCallback(t *testing.T) {
	c, stub := expressClient(t)
	received := make(chan []byte, 4)

	req := validExpress()
	req.CallbackURL = callbackTarget(t, received)
	resp, err := c.Express(context.Background(), req)
	if err != nil {
		t.Fatalf("Express: %v", err)
	}

	// Resolved at Daraja, notification still in flight.
	stub.CompleteSTK(resp.CheckoutRequestID)
	if stub.Pending() != 1 {
		t.Fatalf("Pending() = %d, want the callback held back", stub.Pending())
	}

	query, err := c.ExpressQuery(context.Background(), resp.CheckoutRequestID, 0)
	if err != nil {
		t.Fatalf("ExpressQuery before the callback: %v", err)
	}
	if query.ResultCode.Int64() != 0 {
		t.Errorf("query result code = %d", query.ResultCode.Int64())
	}

	// The callback then arrives and says the same thing, so a caller that acted
	// on the query must be able to treat it as a no-op.
	stub.DeliverNext()
	callback, err := ParseExpressCallback(<-received)
	if err != nil {
		t.Fatalf("ParseExpressCallback: %v", err)
	}
	if !callback.Succeeded() {
		t.Error("the callback disagreed with the query")
	}
}

// A locked initiator password stops every Initiator-bearing endpoint at once,
// which is why Account Balance is the cheapest way to find out.
func TestLockedCredentialStopsEveryInitiatorEndpoint(t *testing.T) {
	c, stub := asyncClient(t)
	stub.Credit(testShortcode, darajastub.AccountUtility, 100_000)
	stub.LockCredential()

	result := make(chan []byte, 4)
	timeout := make(chan []byte, 4)
	urls := asyncURLs(t, result, timeout)

	if _, err := c.AccountBalance(context.Background(), AccountBalanceRequest{URLs: urls}); err == nil {
		t.Error("AccountBalance accepted a locked credential")
	}
	if _, err := c.TransactionStatus(context.Background(), TransactionStatusRequest{
		TransactionID: "X", URLs: urls,
	}); err == nil {
		t.Error("TransactionStatus accepted a locked credential")
	}
	if _, err := c.Reverse(context.Background(), ReversalRequest{
		TransactionID: "X", AmountKES: 1, URLs: urls,
	}); err == nil {
		t.Error("Reverse accepted a locked credential")
	}
	if got := stub.Balance(testShortcode, darajastub.AccountUtility); got != 100_000 {
		t.Errorf("a refused reversal still moved the ledger to %d", got)
	}
}
