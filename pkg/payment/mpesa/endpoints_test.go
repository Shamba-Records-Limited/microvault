package mpesa

import (
	"context"
	"testing"
	"time"

	"github.com/Shamba-Records-Limited/microvault/pkg/payment/mpesa/darajastub"
)

func asyncClient(t *testing.T) (*Client, *darajastub.Stub) {
	t.Helper()
	stub := darajastub.New(t,
		darajastub.WithConsumerCredentials("k", "s"),
		darajastub.WithInitiatorPassword("hunter2"),
	)
	c, err := New(Config{
		Environment: EnvironmentSandbox, ConsumerKey: "k", ConsumerSecret: "s",
		CollectionShortcode: testShortcode, Passkey: "stub-passkey",
		InitiatorName: "apiop37", InitiatorPassword: "hunter2",
		BaseURL: stub.URL(), Certificate: stub.Certificate(),
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return c, stub
}

func asyncURLs(t *testing.T, result, timeout chan<- []byte) AsyncURLs {
	t.Helper()
	return AsyncURLs{
		ResultURL:       callbackTarget(t, result),
		QueueTimeOutURL: callbackTarget(t, timeout),
	}
}

func TestAsyncURLs_Validation(t *testing.T) {
	c, _ := asyncClient(t)
	shared := "https://x.test/callbacks/daraja/abc/both"

	cases := map[string]AsyncURLs{
		"no result":  {QueueTimeOutURL: shared},
		"no timeout": {ResultURL: shared},
		// Sharing one URL makes a timeout and a result indistinguishable, and
		// a timeout read as a failure is how a retry moves money twice.
		"same URL": {ResultURL: shared, QueueTimeOutURL: shared},
		"blocked":  {ResultURL: "https://x.test/mpesa/result", QueueTimeOutURL: shared},
	}
	for name, urls := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := c.AccountBalance(context.Background(), AccountBalanceRequest{URLs: urls}); err == nil {
				t.Error("expected a validation error")
			}
		})
	}
}

func TestAccountBalance_ReadsTheLedger(t *testing.T) {
	c, stub := asyncClient(t)
	stub.Credit(testShortcode, darajastub.AccountUtility, 22_803_700)
	stub.Credit(testShortcode, darajastub.AccountWorking, 70_000_000)

	result := make(chan []byte, 2)
	timeout := make(chan []byte, 2)
	ack, err := c.AccountBalance(context.Background(), AccountBalanceRequest{URLs: asyncURLs(t, result, timeout)})
	if err != nil {
		t.Fatalf("AccountBalance: %v", err)
	}
	if !ack.Accepted() {
		t.Fatalf("ack = %+v", ack)
	}
	stub.Deliver()

	callback, err := ParseCallback(CallbackResult, <-result)
	if err != nil {
		t.Fatalf("ParseCallback: %v", err)
	}
	if callback.Outcome != OutcomeSucceeded {
		t.Fatalf("outcome = %q", callback.Outcome)
	}

	balances, ok := callback.Result.Parameters.Balances("AccountBalance")
	if !ok || len(balances) != 3 {
		t.Fatalf("balances = %+v", balances)
	}
	byName := map[string]int64{}
	for _, balance := range balances {
		byName[balance.Name] = balance.Available
	}
	if byName["Utility Account"] != 22_803_700 || byName["Working Account"] != 70_000_000 {
		t.Errorf("balances = %+v", byName)
	}
}

// A queue timeout is delivered to a different URL and must never be read as a
// failure, whatever its body says.
func TestAsync_QueueTimeoutIsUnknownNotFailed(t *testing.T) {
	c, stub := asyncClient(t)
	result := make(chan []byte, 2)
	timeout := make(chan []byte, 2)

	stub.TimeoutNext(darajastub.RouteAccountBalance)
	if _, err := c.AccountBalance(context.Background(), AccountBalanceRequest{URLs: asyncURLs(t, result, timeout)}); err != nil {
		t.Fatalf("AccountBalance: %v", err)
	}
	stub.Deliver()

	select {
	case <-result:
		t.Fatal("a timeout was delivered to the result URL")
	case body := <-timeout:
		callback, err := ParseCallback(CallbackTimeout, body)
		if err != nil {
			t.Fatalf("ParseCallback: %v", err)
		}
		if callback.Outcome != OutcomeUnknown {
			t.Errorf("outcome = %q, want unknown", callback.Outcome)
		}
		if callback.Result.ResultCode == 0 {
			t.Error("the timeout body carried a success code, which is why the body cannot be trusted to classify it")
		}
	}
}

// Every Initiator-bearing endpoint stands or falls on the RSA credential, and
// the stub holds the private key, so a wrong password is caught here rather
// than as an opaque 2001 at go-live.
func TestAsync_WrongInitiatorPasswordIsRejected(t *testing.T) {
	stub := darajastub.New(t, darajastub.WithInitiatorPassword("hunter2"))
	c, _ := New(Config{
		Environment: EnvironmentSandbox, ConsumerKey: "k", ConsumerSecret: "s",
		CollectionShortcode: testShortcode, InitiatorName: "apiop37",
		InitiatorPassword: "wrong-password",
		BaseURL:           stub.URL(), Certificate: stub.Certificate(),
	})

	result := make(chan []byte, 2)
	timeout := make(chan []byte, 2)
	if _, err := c.AccountBalance(context.Background(), AccountBalanceRequest{URLs: asyncURLs(t, result, timeout)}); err == nil {
		t.Error("a credential encrypting the wrong password was accepted")
	}
}

func TestAsync_LockedCredential(t *testing.T) {
	c, stub := asyncClient(t)
	stub.LockCredential()

	result := make(chan []byte, 2)
	timeout := make(chan []byte, 2)
	if _, err := c.AccountBalance(context.Background(), AccountBalanceRequest{URLs: asyncURLs(t, result, timeout)}); err == nil {
		t.Error("a locked credential was accepted")
	}
}

func TestTransactionStatus(t *testing.T) {
	c, stub := asyncClient(t)
	result := make(chan []byte, 2)
	timeout := make(chan []byte, 2)

	if _, err := c.TransactionStatus(context.Background(), TransactionStatusRequest{
		URLs: asyncURLs(t, result, timeout),
	}); err == nil {
		t.Error("a status query with neither identifier was accepted")
	}

	ack, err := c.TransactionStatus(context.Background(), TransactionStatusRequest{
		TransactionID: "RKL0000001",
		URLs:          asyncURLs(t, result, timeout),
	})
	if err != nil {
		t.Fatalf("TransactionStatus: %v", err)
	}
	if !ack.Accepted() {
		t.Errorf("ack = %+v", ack)
	}
	stub.Deliver()

	callback, err := ParseCallback(CallbackResult, <-result)
	if err != nil {
		t.Fatalf("ParseCallback: %v", err)
	}
	// An unknown receipt resolves to "does not exist", which is a fact, not an
	// error: that is what makes status the resolution path for an unknown.
	if callback.Outcome != OutcomeFailed {
		t.Errorf("outcome = %q", callback.Outcome)
	}
}

func TestReversal(t *testing.T) {
	c, stub := asyncClient(t)
	stub.Credit(testShortcode, darajastub.AccountUtility, 100_000)
	result := make(chan []byte, 2)
	timeout := make(chan []byte, 2)
	urls := asyncURLs(t, result, timeout)

	ack, err := c.Reverse(context.Background(), ReversalRequest{
		TransactionID: "PDU91HIVIT", AmountKES: 200, URLs: urls,
	})
	if err != nil {
		t.Fatalf("Reverse: %v", err)
	}
	if !ack.Accepted() {
		t.Fatalf("ack = %+v", ack)
	}
	stub.Deliver()

	callback, err := ParseCallback(CallbackResult, <-result)
	if err != nil {
		t.Fatalf("ParseCallback: %v", err)
	}
	if callback.Outcome != OutcomeSucceeded {
		t.Fatalf("outcome = %q: %s", callback.Outcome, callback.Result.ResultDesc)
	}
	if got := stub.Balance(testShortcode, darajastub.AccountUtility); got != 80_000 {
		t.Errorf("utility balance = %d, want 80000", got)
	}

	// A reversal result discloses the payer unmasked, with a full name. It is
	// audit-record material, never log material.
	if name, ok := callback.Result.Parameters.Get("CreditPartyPublicName"); !ok || name == "" {
		t.Error("CreditPartyPublicName was absent")
	}
}

func TestReversal_LocalValidation(t *testing.T) {
	c, _ := asyncClient(t)
	result := make(chan []byte, 2)
	timeout := make(chan []byte, 2)
	urls := asyncURLs(t, result, timeout)

	cases := map[string]ReversalRequest{
		"no transaction":  {AmountKES: 200, URLs: urls},
		"zero amount":     {TransactionID: "X", URLs: urls},
		"negative amount": {TransactionID: "X", AmountKES: -1, URLs: urls},
		"short remarks":   {TransactionID: "X", AmountKES: 1, Remarks: "a", URLs: urls},
	}
	for name, req := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := c.Reverse(context.Background(), req); err == nil {
				t.Error("expected a validation error")
			}
		})
	}
}

func TestReversal_InsufficientBalance(t *testing.T) {
	c, stub := asyncClient(t)
	result := make(chan []byte, 2)
	timeout := make(chan []byte, 2)

	if _, err := c.Reverse(context.Background(), ReversalRequest{
		TransactionID: "PDU91HIVIT", AmountKES: 200, URLs: asyncURLs(t, result, timeout),
	}); err != nil {
		t.Fatalf("Reverse: %v", err)
	}
	stub.Deliver()

	callback, _ := ParseCallback(CallbackResult, <-result)
	if callback.Outcome != OutcomeFailed || callback.Result.ResultCode != 1 {
		t.Errorf("outcome = %q, code = %d", callback.Outcome, callback.Result.ResultCode)
	}
}

func TestPull(t *testing.T) {
	c, _ := asyncClient(t)

	register, err := c.PullRegister(context.Background(), PullRegisterRequest{
		NominatedNumber: "254712345678",
		CallbackURL:     "https://x.test/callbacks/daraja/abc/pull",
	})
	if err != nil {
		t.Fatalf("PullRegister: %v", err)
	}
	if register.AlreadyRegistered() {
		t.Error("a first registration reported itself already registered")
	}

	// A second registration is 1001, which is a success for anyone whose goal
	// is "be registered".
	again, err := c.PullRegister(context.Background(), PullRegisterRequest{
		CallbackURL: "https://x.test/callbacks/daraja/abc/pull",
	})
	if err != nil {
		t.Fatalf("second PullRegister: %v", err)
	}
	if !again.AlreadyRegistered() {
		t.Errorf("second registration = %+v", again)
	}
}

// An empty window is reported as 1001 with the body "[[]]" rather than an empty
// array. Treating that as an error makes every quiet hour look like an outage.
func TestPull_EmptyWindowIsSuccess(t *testing.T) {
	c, _ := asyncClient(t)

	transactions, err := c.PullTransactions(context.Background(),
		time.Now().Add(-time.Hour), time.Now(), 0, 0)
	if err != nil {
		t.Fatalf("PullTransactions on an empty window: %v", err)
	}
	if len(transactions) != 0 {
		t.Errorf("got %d transactions", len(transactions))
	}
}

// Pull is the only route to the payer's unmasked number, which is what makes it
// the compliance path rather than a reconciliation convenience.
func TestPull_ReturnsUnmaskedMSISDN(t *testing.T) {
	c, stub := asyncClient(t)
	received := make(chan []byte, 8)
	confirmation := callbackTarget(t, received)

	if _, err := c.RegisterURL(context.Background(), RegisterURLRequest{
		ResponseType: ResponseTypeCompleted, ConfirmationURL: confirmation,
	}); err != nil {
		t.Fatalf("RegisterURL: %v", err)
	}
	stub.SimulateC2B(testShortcode, "254712345678", "MV7K3QA9F", 50_000)
	stub.Deliver()

	confirmed, err := ParseC2BNotification(<-received)
	if err != nil {
		t.Fatalf("ParseC2BNotification: %v", err)
	}
	if !confirmed.MSISDN.Masked() {
		t.Fatal("the C2B confirmation was not masked, so this test proves nothing")
	}

	pulled, err := c.PullTransactions(context.Background(), time.Now().Add(-time.Hour), time.Now(), 0, 0)
	if err != nil {
		t.Fatalf("PullTransactions: %v", err)
	}
	if len(pulled) != 1 {
		t.Fatalf("pulled %d transactions", len(pulled))
	}
	if pulled[0].MSISDN != "254712345678" {
		t.Errorf("pulled MSISDN = %q, want it unmasked", pulled[0].MSISDN)
	}
	if pulled[0].AmountMinor != 50_000 || pulled[0].BillReference != "MV7K3QA9F" {
		t.Errorf("pulled = %+v", pulled[0])
	}
}

func TestPullAll_Paginates(t *testing.T) {
	stub := darajastub.New(t, darajastub.WithPullPageSize(2))
	c, _ := New(Config{
		Environment: EnvironmentSandbox, ConsumerKey: "k", ConsumerSecret: "s",
		CollectionShortcode: testShortcode, BaseURL: stub.URL(), Certificate: stub.Certificate(),
	})

	received := make(chan []byte, 16)
	if _, err := c.RegisterURL(context.Background(), RegisterURLRequest{
		ResponseType: ResponseTypeCompleted, ConfirmationURL: callbackTarget(t, received),
	}); err != nil {
		t.Fatalf("RegisterURL: %v", err)
	}
	for range 5 {
		stub.SimulateC2B(testShortcode, "254712345678", "MV7K3QA9F", 1_000)
	}
	stub.Deliver()

	all, err := c.PullAll(context.Background(), time.Now().Add(-time.Hour), time.Now(), 0)
	if err != nil {
		t.Fatalf("PullAll: %v", err)
	}
	if len(all) != 5 {
		t.Errorf("PullAll returned %d, want 5", len(all))
	}
}

func TestPull_WindowValidation(t *testing.T) {
	c, _ := asyncClient(t)
	now := time.Now()
	if _, err := c.PullTransactions(context.Background(), now, now, 0, 0); err == nil {
		t.Error("a zero-length window was accepted")
	}
	if _, err := c.PullRegister(context.Background(), PullRegisterRequest{}); err == nil {
		t.Error("a registration without a callback URL was accepted")
	}
}

func TestValidateMobileNumber(t *testing.T) {
	c, stub := asyncClient(t)
	stub.BindIdentity("254712345678", "12345678")

	matched, err := c.ValidateMobileNumber(context.Background(), "0712345678", IDTypeNational, "12345678", 0)
	if err != nil {
		t.Fatalf("ValidateMobileNumber: %v", err)
	}
	if !matched.Matched || matched.ResponseCode != "4000" {
		t.Errorf("matched = %+v", matched)
	}

	mismatched, err := c.ValidateMobileNumber(context.Background(), "0712345678", IDTypeNational, "99999999", 0)
	if err != nil {
		t.Fatalf("ValidateMobileNumber: %v", err)
	}
	if mismatched.Matched || mismatched.ResponseCode != "4001" {
		t.Errorf("mismatched = %+v", mismatched)
	}

	if _, err := c.ValidateMobileNumber(context.Background(), "0712345678", IDTypeNational, "", 0); err == nil {
		t.Error("an empty ID number was accepted")
	}
}

func TestDynamicQR(t *testing.T) {
	c, _ := asyncClient(t)

	resp, err := c.DynamicQR(context.Background(), QRRequest{
		MerchantName: "MICROVAULT", ReferenceNo: "MV7K3QA9F",
		AmountKES: 500, TrxCode: TrxPayBill, CreditPartyIdentifier: "174379",
	})
	if err != nil {
		t.Fatalf("DynamicQR: %v", err)
	}
	if resp.QRCode == "" {
		t.Fatal("no QR code returned")
	}

	// The base64 string is returned as it arrived; decoding is opt-in and
	// touches no filesystem.
	png, err := resp.DecodePNG()
	if err != nil {
		t.Fatalf("DecodePNG: %v", err)
	}
	if len(png) < 8 || string(png[1:4]) != "PNG" {
		t.Errorf("decoded %d bytes that are not a PNG", len(png))
	}

	if _, err := c.DynamicQR(context.Background(), QRRequest{AmountKES: 1}); err == nil {
		t.Error("a request without a merchant name was accepted")
	}
	if _, err := c.DynamicQR(context.Background(), QRRequest{
		MerchantName: "M", CreditPartyIdentifier: "1", AmountKES: 0,
	}); err == nil {
		t.Error("a zero amount was accepted")
	}
	if _, err := (QRResponse{QRCode: "!!!not base64"}).DecodePNG(); err == nil {
		t.Error("invalid base64 decoded successfully")
	}
}

func TestHakikisha(t *testing.T) {
	req, err := ParseHakikishaRequest([]byte(`{"accountNumber":"MV7K3QA9F","shortCode":"174379","timestamp":"1717000000","transactionId":"RKL1"}`))
	if err != nil {
		t.Fatalf("ParseHakikishaRequest: %v", err)
	}
	if req.AccountNumber != "MV7K3QA9F" || req.Timestamp.Int64() != 1717000000 {
		t.Errorf("request = %+v", req)
	}

	// timestamp arrives as a string or a number.
	numeric, err := ParseHakikishaRequest([]byte(`{"accountNumber":"X","timestamp":1717000000}`))
	if err != nil || numeric.Timestamp.Int64() != 1717000000 {
		t.Errorf("numeric timestamp = %+v, err %v", numeric, err)
	}

	// A negative answer must disclose nothing at all: this endpoint answers to
	// any M-Pesa customer who can guess a reference.
	missing := AccountNotFound("MV0000000")
	if missing.AccountName != "" || missing.ResponseCode != HakikishaNotFound {
		t.Errorf("not-found response = %+v", missing)
	}
	found := AccountFound("MV7K3QA9F", "Microvault Loan MV7K3QA9")
	if found.ResponseCode != HakikishaFound || found.AccountName == "" {
		t.Errorf("found response = %+v", found)
	}

	if _, err := ParseHakikishaRequest([]byte(`{bad`)); err == nil {
		t.Error("expected a decode error")
	}
}
