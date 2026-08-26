package transaction

import (
	"context"
	"errors"
	"testing"

	"github.com/Shamba-Records-Limited/microvault/pkg/models"
	"github.com/Shamba-Records-Limited/microvault/pkg/repository"
	"github.com/Shamba-Records-Limited/microvault/pkg/services"
)

// fakeRepo implements repository.TransactionRepository with configurable returns.
type fakeRepo struct {
	tx            *models.Transaction
	getByIDErr    error
	byHash        *models.Transaction
	byHashErr     error
	created       *models.Transaction
	createErr     error
	updatedFields map[string]any
	updateErr     error
	list          []*models.Transaction
	listErr       error
}

func (f *fakeRepo) Create(_ context.Context, tx *models.Transaction) error {
	f.created = tx
	return f.createErr
}

func (f *fakeRepo) BatchCreate(_ context.Context, _ []*models.Transaction) error { return f.createErr }

func (f *fakeRepo) GetByID(context.Context, string) (*models.Transaction, error) {
	return f.tx, f.getByIDErr
}

func (f *fakeRepo) GetByStellarHash(context.Context, string) (*models.Transaction, error) {
	return f.byHash, f.byHashErr
}

func (f *fakeRepo) ListByExternalID(context.Context, string) ([]*models.Transaction, error) {
	return f.list, f.listErr
}

func (f *fakeRepo) GetByLoanIDAndType(context.Context, string, string) (*models.Transaction, error) {
	return f.tx, f.getByIDErr
}

func (f *fakeRepo) GetByLoanID(context.Context, string, int, int) ([]*models.Transaction, error) {
	return f.list, f.listErr
}

func (f *fakeRepo) GetByUserID(context.Context, string, int, int) ([]*models.Transaction, error) {
	return f.list, f.listErr
}

func (f *fakeRepo) GetByStatus(context.Context, string, int, int) ([]*models.Transaction, error) {
	return f.list, f.listErr
}

func (f *fakeRepo) List(context.Context, string, string, int, int) ([]*models.Transaction, error) {
	return f.list, f.listErr
}
func (f *fakeRepo) Count(context.Context, string, string) (int64, error) { return 0, nil }
func (f *fakeRepo) Update(context.Context, *models.Transaction) error    { return nil }
func (f *fakeRepo) UpdateFields(_ context.Context, _ string, fields map[string]any) error {
	f.updatedFields = fields
	return f.updateErr
}

func newSvc(r *fakeRepo) Service { return NewService(r) }

func TestCreate_Validation(t *testing.T) {
	svc := newSvc(&fakeRepo{})
	cases := []struct {
		name string
		req  CreateTransactionRequest
		want error
	}{
		{"missing type", CreateTransactionRequest{Amount: 1, Asset: "USDC"}, ErrInvalidTxType},
		{"zero amount", CreateTransactionRequest{TxType: "borrow", Amount: 0, Asset: "USDC"}, ErrInvalidAmount},
		{"missing asset", CreateTransactionRequest{TxType: "borrow", Amount: 1}, ErrInvalidInput},
	}
	for _, c := range cases {
		if _, err := svc.Create(context.Background(), c.req); !errors.Is(err, c.want) {
			t.Errorf("%s: err = %v, want %v", c.name, err, c.want)
		}
	}
}

func TestCreate_Success(t *testing.T) {
	repo := &fakeRepo{byHashErr: repository.ErrTransactionNotFound}
	svc := newSvc(repo)
	resp, err := svc.Create(context.Background(), CreateTransactionRequest{
		TxType: "off_ramp", Amount: 100, Asset: "USDC",
	})
	if err != nil {
		t.Fatal(err)
	}
	if repo.created == nil || repo.created.Status != models.TxStatusPending {
		t.Errorf("created tx not pending: %+v", repo.created)
	}
	if resp.TxCategory == "" {
		t.Error("response should carry a derived TxCategory")
	}
}

func TestCreate_DuplicateStellarHash(t *testing.T) {
	hash := "abc123"
	repo := &fakeRepo{byHash: &models.Transaction{ID: "x", StellarTxHash: &hash}}
	svc := newSvc(repo)
	_, err := svc.Create(context.Background(), CreateTransactionRequest{
		TxType: "borrow", Amount: 1, Asset: "USDC", StellarTxHash: &hash,
	})
	if !errors.Is(err, ErrStellarHashAlreadyExists) {
		t.Errorf("err = %v, want ErrStellarHashAlreadyExists", err)
	}
}

func TestGetByID_NotFoundMapping(t *testing.T) {
	svc := newSvc(&fakeRepo{getByIDErr: repository.ErrTransactionNotFound})
	if _, err := svc.GetByID(context.Background(), "nope"); !errors.Is(err, ErrTransactionNotFound) {
		t.Errorf("err = %v, want ErrTransactionNotFound", err)
	}
}

func TestGetByStatus_InvalidStatus(t *testing.T) {
	svc := newSvc(&fakeRepo{})
	if _, err := svc.GetByStatus(context.Background(), "bogus", services.Pagination{}); !errors.Is(err, ErrInvalidStatus) {
		t.Errorf("err = %v, want ErrInvalidStatus", err)
	}
}

func TestUpdate_StatusTransitions(t *testing.T) {
	sub := models.TxStatusSubmitted
	success := models.TxStatusSuccess

	// pending -> success is invalid (must go via submitted).
	svc := newSvc(&fakeRepo{tx: &models.Transaction{ID: "1", Status: models.TxStatusPending}})
	if _, err := svc.Update(context.Background(), "1", UpdateTransactionRequest{Status: &success}); !errors.Is(err, ErrInvalidStatusTransition) {
		t.Errorf("pending->success err = %v, want ErrInvalidStatusTransition", err)
	}

	// pending -> submitted is valid.
	repo := &fakeRepo{tx: &models.Transaction{ID: "1", Status: models.TxStatusPending}}
	if _, err := newSvc(repo).Update(context.Background(), "1", UpdateTransactionRequest{Status: &sub}); err != nil {
		t.Errorf("pending->submitted err = %v, want nil", err)
	}
	if got, ok := repo.updatedFields["status"].(*string); !ok || got == nil || *got != models.TxStatusSubmitted {
		t.Errorf("status field not written as submitted: %v", repo.updatedFields)
	}

	// A completed transaction cannot be modified.
	done := newSvc(&fakeRepo{tx: &models.Transaction{ID: "1", Status: models.TxStatusSuccess}})
	if _, err := done.Update(context.Background(), "1", UpdateTransactionRequest{Status: &sub}); !errors.Is(err, ErrCannotModifyTransaction) {
		t.Errorf("modify success err = %v, want ErrCannotModifyTransaction", err)
	}
}

func TestReadMethods(t *testing.T) {
	ctx := context.Background()
	hash := "h1"
	sample := &models.Transaction{ID: "1", TxType: "off_ramp", Status: models.TxStatusPending}

	// BatchCreate: empty -> empty, non-empty -> mapped responses.
	svc := newSvc(&fakeRepo{})
	if out, err := svc.BatchCreate(ctx, nil); err != nil || len(out) != 0 {
		t.Errorf("BatchCreate(empty) = %v (err %v)", out, err)
	}
	out, err := svc.BatchCreate(ctx, []CreateTransactionRequest{{TxType: "borrow", Amount: 1, Asset: "USDC"}})
	if err != nil || len(out) != 1 {
		t.Errorf("BatchCreate = %v (err %v)", out, err)
	}

	// ListByExternalID maps every row.
	svc = newSvc(&fakeRepo{list: []*models.Transaction{sample, sample}})
	if rows, err := svc.ListByExternalID(ctx, "ext-1"); err != nil || len(rows) != 2 {
		t.Errorf("ListByExternalID = %v (err %v)", rows, err)
	}

	// GetByStellarHash not-found maps to the service sentinel.
	svc = newSvc(&fakeRepo{byHashErr: repository.ErrTransactionNotFound})
	if _, err := svc.GetByStellarHash(ctx, hash); !errors.Is(err, ErrTransactionNotFound) {
		t.Errorf("GetByStellarHash err = %v, want ErrTransactionNotFound", err)
	}

	// GetByLoanIDAndType: nil row -> not found; present -> response.
	svc = newSvc(&fakeRepo{tx: nil})
	if _, err := svc.GetByLoanIDAndType(ctx, "loan-1", "off_ramp"); !errors.Is(err, ErrTransactionNotFound) {
		t.Errorf("GetByLoanIDAndType(nil) err = %v, want ErrTransactionNotFound", err)
	}
	svc = newSvc(&fakeRepo{tx: sample})
	if resp, err := svc.GetByLoanIDAndType(ctx, "loan-1", "off_ramp"); err != nil || resp.ID != "1" {
		t.Errorf("GetByLoanIDAndType = %v (err %v)", resp, err)
	}
}

func TestPaginationClamping(t *testing.T) {
	repo := &fakeRepo{}
	svc := newSvc(repo)
	// Page<=0 becomes 1, PageSize>100 becomes 100.
	resp, err := svc.GetByLoanID(context.Background(), "loan-1", services.Pagination{Page: 0, PageSize: 500})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Page != 1 || resp.PageSize != 100 {
		t.Errorf("clamping wrong: page=%d size=%d", resp.Page, resp.PageSize)
	}
}
