package controllers

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"

	"github.com/Shamba-Records-Limited/microvault/pkg/config"
	"github.com/Shamba-Records-Limited/microvault/pkg/loanref"
	"github.com/Shamba-Records-Limited/microvault/pkg/models"
	"github.com/Shamba-Records-Limited/microvault/pkg/repository"
)

type fakeMpesaRepo struct {
	recorded   []*models.MpesaTransaction
	recordErr  error
	resolved   string
	resolveErr error
}

func (f *fakeMpesaRepo) Record(ctx context.Context, tx *models.MpesaTransaction) error {
	if f.recordErr != nil {
		return f.recordErr
	}
	f.recorded = append(f.recorded, tx)
	return nil
}

func (f *fakeMpesaRepo) GetByTransID(ctx context.Context, transID string) (*models.MpesaTransaction, error) {
	return nil, repository.ErrMpesaNotFound
}

func (f *fakeMpesaRepo) GetByCheckoutID(ctx context.Context, checkoutID string) (*models.MpesaTransaction, error) {
	return nil, repository.ErrMpesaNotFound
}

func (f *fakeMpesaRepo) DuePoll(ctx context.Context, limit int) ([]*models.MpesaTransaction, error) {
	return nil, nil
}

func (f *fakeMpesaRepo) Confirm(ctx context.Context, transID string, via models.MpesaTransactionConfirmVia, loanID string) error {
	return nil
}

func (f *fakeMpesaRepo) UpdatePoll(ctx context.Context, transID string, at time.Time) error {
	return nil
}

func (f *fakeMpesaRepo) StopPoll(ctx context.Context, transID string) error { return nil }

func (f *fakeMpesaRepo) GetLoanIDByReference(ctx context.Context, reference string) (string, error) {
	return f.resolved, f.resolveErr
}

func (f *fakeMpesaRepo) SetReversalState(ctx context.Context, transID string, state models.MpesaTransactionReversal) error {
	return nil
}

func (f *fakeMpesaRepo) UpdateFields(ctx context.Context, tx *models.MpesaTransaction) error {
	return nil
}

func callbackController(repo *fakeMpesaRepo, serverEnv string, cidrs []string) *DarajaCallbackController {
	cfg := config.MpesaConfig{
		CallbackSlug:         "testslug",
		ReferencePrefix:      "MV",
		CallbackAllowedCIDRs: cidrs,
		STKPollInterval:      5 * time.Second,
	}
	return &DarajaCallbackController{
		repo:      repo,
		config:    cfg,
		serverEnv: serverEnv,
		resolveLoan: func(ctx context.Context, reference string) (string, error) {
			return repo.resolved, repo.resolveErr
		},
		now: func() time.Time { return time.Unix(1700000000, 0) },
	}
}

func serve(t *testing.T, ctrl *DarajaCallbackController) *fiber.App {
	t.Helper()
	app := fiber.New()
	ctrl.Register(app)
	return app
}

func postJSON(t *testing.T, app *fiber.App, path, body string) *http.Response {
	t.Helper()
	req := httptest.NewRequestWithContext(context.Background(), fiber.MethodPost, path, strings.NewReader(body))
	req.Header.Set(fiber.HeaderContentType, fiber.MIMEApplicationJSON)
	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	return resp
}

func bodyString(t *testing.T, resp *http.Response) string {
	t.Helper()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	return string(raw)
}

const stkSuccessBody = `{
	"Body": {
		"stkCallback": {
			"MerchantRequestID": "29115-34620561-1",
			"CheckoutRequestID": "ws_CO_19122019102018173",
			"ResultCode": 0,
			"ResultDesc": "The service request is processed successfully.",
			"CallbackMetadata": {
				"Item": [
					{"Name": "Amount", "Value": 1.00},
					{"Name": "MpesaReceiptNumber", "Value": "NLJ7RT61SV"},
					{"Name": "PhoneNumber", "Value": 254708374149},
					{"Name": "TransactionDate", "Value": 20191219102115}
				]
			}
		}
	}
}`

const stkFailedBody = `{
	"Body": {
		"stkCallback": {
			"MerchantRequestID": "29115-34620561-1",
			"CheckoutRequestID": "ws_CO_19122019102018173",
			"ResultCode": 1032,
			"ResultDesc": "Request cancelled by user"
		}
	}
}`

func c2bBody(billRef string) string {
	return `{
		"TransactionType": "Pay Bill",
		"TransID": "RKTQDM7W6J",
		"TransTime": "20190928203240",
		"TransAmount": "10",
		"BusinessShortCode": "600638",
		"BillRefNumber": "` + billRef + `",
		"OrgAccountBalance": "49197.00",
		"MSISDN": "254708374149",
		"FirstName": "John",
		"LastName": "Doe"
	}`
}

const statusResultBody = `{
	"Result": {
		"ResultType": 0,
		"ResultCode": 0,
		"ResultDesc": "The service request is processed successfully.",
		"OriginatorConversationID": "10571-7910404-1",
		"ConversationID": "AG_20191219_00004e48f20467fc3f33",
		"TransactionID": "NLJ41RT61SV",
		"ResultParameters": {"ResultParameter": [
			{"Name": "TransactionStatus", "Value": "Completed"}
		]}
	}
}`

func TestSTKSuccessRecordedForPolling(t *testing.T) {
	repo := &fakeMpesaRepo{}
	ctrl := callbackController(repo, "development", nil)
	app := serve(t, ctrl)

	resp := postJSON(t, app, "/callbacks/daraja/testslug/stk/result", stkSuccessBody)
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if len(repo.recorded) != 1 {
		t.Fatalf("recorded = %d observations, want 1", len(repo.recorded))
	}
	tx := repo.recorded[0]
	if tx.TransID != "NLJ7RT61SV" {
		t.Fatalf("TransID = %q, want NLJ7RT61SV", tx.TransID)
	}
	if tx.NextPollAt == nil {
		t.Fatal("success was not scheduled for polling")
	}
	if !tx.NextPollAt.After(time.Unix(1700000000, 0)) {
		t.Fatal("NextPollAt is not in the future")
	}
}

func TestSTKFailureNotRecorded(t *testing.T) {
	repo := &fakeMpesaRepo{}
	ctrl := callbackController(repo, "development", nil)
	app := serve(t, ctrl)

	resp := postJSON(t, app, "/callbacks/daraja/testslug/stk/result", stkFailedBody)
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if len(repo.recorded) != 0 {
		t.Fatal("a failed prompt was recorded as a payment")
	}
}

func TestSTKDuplicateAcknowledged(t *testing.T) {
	repo := &fakeMpesaRepo{recordErr: repository.ErrMpesaConflict}
	ctrl := callbackController(repo, "development", nil)
	app := serve(t, ctrl)

	resp := postJSON(t, app, "/callbacks/daraja/testslug/stk/result", stkSuccessBody)
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
}

func TestSTKBadBodyRejected(t *testing.T) {
	repo := &fakeMpesaRepo{}
	ctrl := callbackController(repo, "development", nil)
	app := serve(t, ctrl)

	resp := postJSON(t, app, "/callbacks/daraja/testslug/stk/result", `{}`)
	if resp.StatusCode != fiber.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
	if len(repo.recorded) != 0 {
		t.Fatal("unparseable body was recorded")
	}
}

func TestC2BValidationAccepts(t *testing.T) {
	repo := &fakeMpesaRepo{resolved: "loan-123"}
	ctrl := callbackController(repo, "development", nil)
	app := serve(t, ctrl)

	ref, err := loanref.GenerateDeterministic("MV", [16]byte{1})
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	resp := postJSON(t, app, "/callbacks/daraja/testslug/c2b/validation", c2bBody(ref))
	raw := bodyString(t, resp)
	for _, want := range []string{`"ResultCode":"0"`, `"ThirdPartyTransID":"loan-123"`} {
		if !strings.Contains(raw, want) {
			t.Fatalf("body = %s, want %s", raw, want)
		}
	}
}

func TestC2BValidationRejectsBadShape(t *testing.T) {
	repo := &fakeMpesaRepo{resolved: "loan-123"}
	ctrl := callbackController(repo, "development", nil)
	app := serve(t, ctrl)

	resp := postJSON(t, app, "/callbacks/daraja/testslug/c2b/validation", c2bBody("NOT-A-REF"))
	raw := bodyString(t, resp)
	if want := `"ResultCode":"C2B00012"`; !strings.Contains(raw, want) {
		t.Fatalf("body = %s, want %s", raw, want)
	}
}

func TestC2BValidationRejectsUnknownReference(t *testing.T) {
	repo := &fakeMpesaRepo{resolved: ""}
	ctrl := callbackController(repo, "development", nil)
	app := serve(t, ctrl)

	ref, err := loanref.GenerateDeterministic("MV", [16]byte{2})
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	resp := postJSON(t, app, "/callbacks/daraja/testslug/c2b/validation", c2bBody(ref))
	raw := bodyString(t, resp)
	if want := `"ResultCode":"C2B00012"`; !strings.Contains(raw, want) {
		t.Fatalf("body = %s, want %s", raw, want)
	}
}

func TestC2BValidationRejectsInternalError(t *testing.T) {
	repo := &fakeMpesaRepo{resolveErr: context.DeadlineExceeded}
	ctrl := callbackController(repo, "development", nil)
	app := serve(t, ctrl)

	ref, err := loanref.GenerateDeterministic("MV", [16]byte{3})
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	resp := postJSON(t, app, "/callbacks/daraja/testslug/c2b/validation", c2bBody(ref))
	raw := bodyString(t, resp)
	if want := `"ResultCode":"C2B00016"`; !strings.Contains(raw, want) {
		t.Fatalf("body = %s, want %s", raw, want)
	}
}

func TestC2BConfirmationRecorded(t *testing.T) {
	repo := &fakeMpesaRepo{}
	ctrl := callbackController(repo, "development", nil)
	app := serve(t, ctrl)

	resp := postJSON(t, app, "/callbacks/daraja/testslug/c2b/confirmation", c2bBody("MV2XY7ZQ"))
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if len(repo.recorded) != 1 {
		t.Fatalf("recorded = %d, want 1", len(repo.recorded))
	}
	tx := repo.recorded[0]
	if tx.TransID != "RKTQDM7W6J" || tx.Source != models.MpesaSourceC2BConfirmation {
		t.Fatalf("tx = %+v", tx)
	}
	if tx.AmountKes != 10 {
		t.Fatalf("AmountKes = %d, want 10", tx.AmountKes)
	}
	if tx.PayerName == nil || *tx.PayerName != "John" {
		t.Fatalf("PayerName = %v, want John", tx.PayerName)
	}
	if tx.NextPollAt != nil {
		t.Fatal("a C2B confirmation must not enter the STK poll set")
	}
}

func TestAsyncResultNoPoll(t *testing.T) {
	repo := &fakeMpesaRepo{}
	ctrl := callbackController(repo, "development", nil)
	app := serve(t, ctrl)

	resp := postJSON(t, app, "/callbacks/daraja/testslug/status/result", statusResultBody)
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if len(repo.recorded) != 1 {
		t.Fatalf("recorded = %d, want 1", len(repo.recorded))
	}
	if repo.recorded[0].NextPollAt != nil {
		t.Fatal("a result delivery was scheduled for polling")
	}
}

func TestAsyncTimeoutParks(t *testing.T) {
	repo := &fakeMpesaRepo{}
	ctrl := callbackController(repo, "development", nil)
	app := serve(t, ctrl)

	resp := postJSON(t, app, "/callbacks/daraja/testslug/status/timeout", statusResultBody)
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if len(repo.recorded) != 1 {
		t.Fatalf("recorded = %d, want 1", len(repo.recorded))
	}
	if repo.recorded[0].NextPollAt == nil {
		t.Fatal("a queue timeout was not parked for TransactionStatus resolution")
	}
}

func TestCIDRFailClosedInProduction(t *testing.T) {
	repo := &fakeMpesaRepo{}
	ctrl := callbackController(repo, "production", nil)
	app := serve(t, ctrl)

	resp := postJSON(t, app, "/callbacks/daraja/testslug/stk/result", stkSuccessBody)
	if resp.StatusCode != fiber.StatusForbidden {
		t.Fatalf("status = %d, want 403", resp.StatusCode)
	}
	if len(repo.recorded) != 0 {
		t.Fatal("production request without an allowlist was recorded")
	}
}

func TestCIDRMismatchForbidden(t *testing.T) {
	repo := &fakeMpesaRepo{}
	ctrl := callbackController(repo, "development", []string{"10.0.0.0/8"})
	app := serve(t, ctrl)

	resp := postJSON(t, app, "/callbacks/daraja/testslug/stk/result", stkSuccessBody)
	if resp.StatusCode != fiber.StatusForbidden {
		t.Fatalf("status = %d, want 403", resp.StatusCode)
	}
	if len(repo.recorded) != 0 {
		t.Fatal("request outside the allowlist was recorded")
	}
}

func TestCIDRMatchAllowed(t *testing.T) {
	repo := &fakeMpesaRepo{}
	ctrl := callbackController(repo, "production", []string{"0.0.0.0/0"})
	app := serve(t, ctrl)

	resp := postJSON(t, app, "/callbacks/daraja/testslug/stk/result", stkSuccessBody)
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if len(repo.recorded) != 1 {
		t.Fatalf("recorded = %d, want 1", len(repo.recorded))
	}
}
