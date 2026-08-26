package account

import (
	"context"
	"errors"
	"strings"
	"testing"

	"gorm.io/gorm"

	"github.com/Shamba-Records-Limited/microvault/pkg/models"
	"github.com/Shamba-Records-Limited/microvault/pkg/repository"
	"github.com/Shamba-Records-Limited/microvault/pkg/services"
)

// --- fakes ---

type acctRepo struct {
	byPubKey    *models.Account
	byPubKeyErr error
	created     *models.Account
}

func (f *acctRepo) Create(_ context.Context, a *models.Account) error { f.created = a; return nil }
func (f *acctRepo) CreateWithTx(_ context.Context, _ *gorm.DB, a *models.Account) error {
	f.created = a
	return nil
}
func (f *acctRepo) GetByID(context.Context, string) (*models.Account, error)     { return nil, nil }
func (f *acctRepo) GetByUserID(context.Context, string) (*models.Account, error) { return nil, nil }
func (f *acctRepo) GetByPublicKey(context.Context, string) (*models.Account, error) {
	return f.byPubKey, f.byPubKeyErr
}

func (f *acctRepo) GetNextAccountIndex(context.Context, string) (int, error) { return 0, nil }

func (f *acctRepo) GetNextAccountIndexWithTx(context.Context, *gorm.DB) (int, error) { return 0, nil }
func (f *acctRepo) EnsureAccountIndexFloor(context.Context, int64) error             { return nil }
func (f *acctRepo) Update(context.Context, *models.Account) error                    { return nil }
func (f *acctRepo) UpdateChainStatus(context.Context, string, string) error          { return nil }
func (f *acctRepo) Restore(context.Context, string) error                            { return nil }
func (f *acctRepo) Delete(context.Context, string) error                             { return nil }

type usrRepo struct {
	byID    *models.User
	byIDErr error
}

func (f *usrRepo) Create(context.Context, *models.User) error                 { return nil }
func (f *usrRepo) CreateWithTx(context.Context, *gorm.DB, *models.User) error { return nil }
func (f *usrRepo) GetByID(context.Context, string) (*models.User, error)      { return f.byID, f.byIDErr }

func (f *usrRepo) GetByMobileNumber(context.Context, string) (*models.User, error) { return nil, nil }

func (f *usrRepo) GetByNationalID(context.Context, string) (*models.User, error) { return nil, nil }

func (f *usrRepo) GetByKYCStatus(context.Context, string, int, int) ([]*models.User, error) {
	return nil, nil
}

func (f *usrRepo) GetByRole(context.Context, string, int, int) ([]*models.User, error) {
	return nil, nil
}
func (f *usrRepo) List(context.Context, int, int) ([]*models.User, error) { return nil, nil }
func (f *usrRepo) ListFiltered(context.Context, string, string, int, int) ([]*models.User, error) {
	return nil, nil
}
func (f *usrRepo) Count(context.Context) (int64, error) { return 0, nil }
func (f *usrRepo) CountFiltered(context.Context, string, string) (int64, error) {
	return 0, nil
}
func (f *usrRepo) CountByKYCStatus(context.Context, string) (int64, error)  { return 0, nil }
func (f *usrRepo) CountByRole(context.Context, string) (int64, error)       { return 0, nil }
func (f *usrRepo) CountAdmins(context.Context) (int, error)                 { return 1, nil }
func (f *usrRepo) Update(context.Context, *models.User) error               { return nil }
func (f *usrRepo) UpdateMobileNumber(context.Context, string, string) error { return nil }
func (f *usrRepo) Restore(context.Context, string) error                    { return nil }
func (f *usrRepo) Delete(context.Context, string) error                     { return nil }

const validPK = "G" + "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA" // 56 chars

func TestAccountCreate_Validation(t *testing.T) {
	svc := NewService(&acctRepo{}, &usrRepo{})
	if _, err := svc.Create(context.Background(), CreateAccountRequest{}); !errors.Is(err, ErrInvalidInput) {
		t.Errorf("missing user: err = %v, want ErrInvalidInput", err)
	}
	if _, err := svc.Create(context.Background(), CreateAccountRequest{UserID: "u1", PublicKey: "tooshort"}); !errors.Is(err, ErrInvalidPublicKey) {
		t.Errorf("bad key: err = %v, want ErrInvalidPublicKey", err)
	}
}

func TestAccountCreate_UserNotFound(t *testing.T) {
	svc := NewService(&acctRepo{}, &usrRepo{byIDErr: repository.ErrUserNotFound})
	_, err := svc.Create(context.Background(), CreateAccountRequest{UserID: "ghost", PublicKey: validPK})
	if !errors.Is(err, services.ErrNotFound) {
		t.Errorf("err = %v, want services.ErrNotFound", err)
	}
}

func TestAccountCreate_DuplicateKey(t *testing.T) {
	acc := &acctRepo{byPubKey: &models.Account{ID: "a1"}} // GetByPublicKey succeeds -> exists
	svc := NewService(acc, &usrRepo{byID: &models.User{ID: "u1"}})
	_, err := svc.Create(context.Background(), CreateAccountRequest{UserID: "u1", PublicKey: validPK})
	if !errors.Is(err, ErrPublicKeyAlreadyExists) {
		t.Errorf("err = %v, want ErrPublicKeyAlreadyExists", err)
	}
}

func TestAccountCreate_Success(t *testing.T) {
	acc := &acctRepo{byPubKeyErr: repository.ErrAccountNotFound}
	svc := NewService(acc, &usrRepo{byID: &models.User{ID: "u1"}})
	resp, err := svc.Create(context.Background(), CreateAccountRequest{UserID: "u1", PublicKey: validPK})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if resp == nil || acc.created == nil || !strings.HasPrefix(acc.created.PublicKey, "G") {
		t.Errorf("account not created correctly: %+v", acc.created)
	}
}
