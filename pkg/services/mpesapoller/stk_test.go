package mpesapoller

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/Shamba-Records-Limited/microvault/pkg/models"
	"github.com/Shamba-Records-Limited/microvault/pkg/payment/mpesa"
	"github.com/Shamba-Records-Limited/microvault/pkg/repository"
)

type fakeQuerier struct {
	resp *mpesa.ExpressQueryResponse
	err  error

	calls int
}

func (f *fakeQuerier) ExpressQuery(ctx context.Context, checkoutRequestID string, shortcode uint) (*mpesa.ExpressQueryResponse, error) {
	f.calls++
	return f.resp, f.err
}

type fakeRepo struct {
	confirmed  []string
	parked     map[string]time.Time
	stopped    []string
	confirmErr error
}

func (f *fakeRepo) Confirm(ctx context.Context, transID string, via models.MpesaTransactionConfirmVia, loanID string) error {
	if f.confirmErr != nil {
		return f.confirmErr
	}
	f.confirmed = append(f.confirmed, transID)
	return nil
}

func (f *fakeRepo) UpdatePoll(ctx context.Context, transID string, at time.Time) error {
	if f.parked == nil {
		f.parked = map[string]time.Time{}
	}
	f.parked[transID] = at
	return nil
}

func (f *fakeRepo) StopPoll(ctx context.Context, transID string) error {
	f.stopped = append(f.stopped, transID)
	return nil
}

func (f *fakeRepo) Record(ctx context.Context, tx *models.MpesaTransaction) error { return nil }

func (f *fakeRepo) GetByTransID(ctx context.Context, transID string) (*models.MpesaTransaction, error) {
	return nil, repository.ErrMpesaNotFound
}

func (f *fakeRepo) DuePoll(ctx context.Context, limit int) ([]*models.MpesaTransaction, error) {
	return nil, nil
}

func (f *fakeRepo) GetLoanIDByReference(ctx context.Context, reference string) (string, error) {
	return "", nil
}

func (f *fakeRepo) SetReversalState(ctx context.Context, transID string, state models.MpesaTransactionReversal) error {
	return nil
}

func (f *fakeRepo) UpdateFields(ctx context.Context, tx *models.MpesaTransaction) error { return nil }

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func testDriver(repo *fakeRepo, querier *fakeQuerier) *stkDriver {
	return &stkDriver{
		repo:    repo,
		client:  querier,
		now:     func() time.Time { return time.Unix(1700000000, 0) },
		backoff: 5 * time.Second,
		logger:  discardLogger(),
	}
}

func stkTx(checkout string) models.MpesaTransaction {
	return models.MpesaTransaction{
		TransID:           "NLJ7RT61SV",
		Source:            models.MpesaSourceSTKCallback,
		CheckoutRequestID: &checkout,
	}
}

func queryResp(code string) *mpesa.ExpressQueryResponse {
	return &mpesa.ExpressQueryResponse{ResultCode: json.RawMessage(code)}
}

func TestDrivePendingParks(t *testing.T) {
	repo := &fakeRepo{}
	querier := &fakeQuerier{err: errors.New("the transaction is being processed")}
	d := testDriver(repo, querier)

	d.Drive(context.Background(), stkTx("ws_CO_1"))

	if querier.calls != 1 {
		t.Fatalf("query calls = %d, want 1", querier.calls)
	}
	if _, ok := repo.parked["NLJ7RT61SV"]; !ok {
		t.Fatal("pending checkout was not parked")
	}
	if len(repo.confirmed) != 0 || len(repo.stopped) != 0 {
		t.Fatal("pending checkout was confirmed or stopped")
	}
}

func TestDriveSuccessConfirms(t *testing.T) {
	repo := &fakeRepo{}
	querier := &fakeQuerier{resp: queryResp("0")}
	d := testDriver(repo, querier)

	d.Drive(context.Background(), stkTx("ws_CO_1"))

	if len(repo.confirmed) != 1 || repo.confirmed[0] != "NLJ7RT61SV" {
		t.Fatalf("confirmed = %v, want [NLJ7RT61SV]", repo.confirmed)
	}
	if len(repo.parked) != 0 || len(repo.stopped) != 0 {
		t.Fatal("success was parked or stopped")
	}
}

func TestDriveRetryableParks(t *testing.T) {
	repo := &fakeRepo{}
	querier := &fakeQuerier{resp: queryResp("1")} // insufficient funds
	d := testDriver(repo, querier)

	d.Drive(context.Background(), stkTx("ws_CO_1"))

	if _, ok := repo.parked["NLJ7RT61SV"]; !ok {
		t.Fatal("retryable failure was not parked")
	}
	if len(repo.stopped) != 0 {
		t.Fatal("retryable failure was stopped")
	}
}

func TestDriveTerminalStopsPoll(t *testing.T) {
	repo := &fakeRepo{}
	querier := &fakeQuerier{resp: queryResp("2")} // below minimum
	d := testDriver(repo, querier)

	d.Drive(context.Background(), stkTx("ws_CO_1"))

	if len(repo.stopped) != 1 || repo.stopped[0] != "NLJ7RT61SV" {
		t.Fatalf("stopped = %v, want [NLJ7RT61SV]", repo.stopped)
	}
	if _, ok := repo.parked["NLJ7RT61SV"]; ok {
		t.Fatal("terminal failure was parked")
	}
}

func TestDriveConfirmErrorParks(t *testing.T) {
	repo := &fakeRepo{confirmErr: errors.New("db down")}
	querier := &fakeQuerier{resp: queryResp("0")}
	d := testDriver(repo, querier)

	d.Drive(context.Background(), stkTx("ws_CO_1"))

	if _, ok := repo.parked["NLJ7RT61SV"]; !ok {
		t.Fatal("failed confirm was not re-parked")
	}
	if len(repo.confirmed) != 0 {
		t.Fatal("failed confirm was recorded as confirmed")
	}
}

func TestDriveNonSTKParksWithoutQuery(t *testing.T) {
	repo := &fakeRepo{}
	querier := &fakeQuerier{}
	d := testDriver(repo, querier)

	tx := models.MpesaTransaction{TransID: "RKTQDM7W6J", Source: models.MpesaSourceC2BConfirmation}
	d.Drive(context.Background(), tx)

	if querier.calls != 0 {
		t.Fatalf("query calls = %d, want 0", querier.calls)
	}
	if _, ok := repo.parked["RKTQDM7W6J"]; !ok {
		t.Fatal("non-STK observation was not parked")
	}
}

func TestDriveMissingCheckoutParksWithoutQuery(t *testing.T) {
	repo := &fakeRepo{}
	querier := &fakeQuerier{}
	d := testDriver(repo, querier)

	d.Drive(context.Background(), models.MpesaTransaction{TransID: "NLJ7RT61SV", Source: models.MpesaSourceSTKCallback})

	if querier.calls != 0 {
		t.Fatalf("query calls = %d, want 0", querier.calls)
	}
	if _, ok := repo.parked["NLJ7RT61SV"]; !ok {
		t.Fatal("observation without a checkout id was not parked")
	}
}
