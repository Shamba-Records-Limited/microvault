package user

import (
	"context"
	"errors"
	"testing"

	"github.com/Shamba-Records-Limited/microvault/pkg/models"
	"github.com/Shamba-Records-Limited/microvault/pkg/repository"
	"gorm.io/gorm"
)

// fakeUserRepo implements repository.UserRepository. Only the lookups exercised
// by the service are configurable; the rest are inert stubs.
type fakeUserRepo struct {
	byMobile    *models.User
	byMobileErr error
	byNat       *models.User
	byNatErr    error
	byID        *models.User
	byIDErr     error
	created     *models.User
	createErr   error
}

func (f *fakeUserRepo) Create(_ context.Context, u *models.User) error {
	f.created = u
	return f.createErr
}
func (f *fakeUserRepo) CreateWithTx(_ context.Context, _ *gorm.DB, u *models.User) error {
	f.created = u
	return f.createErr
}
func (f *fakeUserRepo) GetByID(context.Context, string) (*models.User, error) {
	return f.byID, f.byIDErr
}
func (f *fakeUserRepo) GetByMobileNumber(context.Context, string) (*models.User, error) {
	return f.byMobile, f.byMobileErr
}
func (f *fakeUserRepo) GetByNationalID(context.Context, string) (*models.User, error) {
	return f.byNat, f.byNatErr
}
func (f *fakeUserRepo) GetByKYCStatus(context.Context, string, int, int) ([]*models.User, error) {
	return nil, nil
}
func (f *fakeUserRepo) GetByRole(context.Context, string, int, int) ([]*models.User, error) {
	return nil, nil
}
func (f *fakeUserRepo) List(context.Context, int, int) ([]*models.User, error) { return nil, nil }
func (f *fakeUserRepo) ListFiltered(context.Context, string, string, int, int) ([]*models.User, error) {
	return nil, nil
}
func (f *fakeUserRepo) Count(context.Context) (int64, error) { return 0, nil }
func (f *fakeUserRepo) CountFiltered(context.Context, string, string) (int64, error) {
	return 0, nil
}
func (f *fakeUserRepo) CountByKYCStatus(context.Context, string) (int64, error)  { return 0, nil }
func (f *fakeUserRepo) CountByRole(context.Context, string) (int64, error)       { return 0, nil }
func (f *fakeUserRepo) CountAdmins(context.Context) (int, error)                 { return 1, nil }
func (f *fakeUserRepo) Update(context.Context, *models.User) error               { return nil }
func (f *fakeUserRepo) UpdateMobileNumber(context.Context, string, string) error { return nil }
func (f *fakeUserRepo) Restore(context.Context, string) error                    { return nil }
func (f *fakeUserRepo) Delete(context.Context, string) error                     { return nil }

// notFoundRepo returns "not found" for the uniqueness lookups so Create proceeds.
func notFoundRepo() *fakeUserRepo {
	return &fakeUserRepo{
		byMobileErr: repository.ErrUserNotFound,
		byNatErr:    repository.ErrUserNotFound,
	}
}

func TestUserCreate_Validation(t *testing.T) {
	svc := NewService(notFoundRepo())
	if _, err := svc.Create(context.Background(), CreateUserRequest{}); !errors.Is(err, ErrInvalidMobileNumber) {
		t.Errorf("missing mobile: err = %v, want ErrInvalidMobileNumber", err)
	}
	if _, err := svc.Create(context.Background(), CreateUserRequest{MobileNumber: "254711000111", Role: "wizard"}); !errors.Is(err, ErrInvalidRole) {
		t.Errorf("bad role: err = %v, want ErrInvalidRole", err)
	}
}

func TestUserCreate_Duplicates(t *testing.T) {
	// Existing mobile.
	repo := notFoundRepo()
	repo.byMobile, repo.byMobileErr = &models.User{ID: "u1"}, nil
	if _, err := NewService(repo).Create(context.Background(), CreateUserRequest{MobileNumber: "254711000111"}); !errors.Is(err, ErrMobileNumberAlreadyExists) {
		t.Errorf("dup mobile: err = %v, want ErrMobileNumberAlreadyExists", err)
	}

	// Existing national ID.
	repo = notFoundRepo()
	repo.byNat, repo.byNatErr = &models.User{ID: "u1"}, nil
	if _, err := NewService(repo).Create(context.Background(), CreateUserRequest{MobileNumber: "254711000111", NationalID: "12345678"}); !errors.Is(err, ErrNationalIDAlreadyExists) {
		t.Errorf("dup national ID: err = %v, want ErrNationalIDAlreadyExists", err)
	}
}

func TestUserCreate_SuccessDefaults(t *testing.T) {
	repo := notFoundRepo()
	svc := NewService(repo)
	resp, err := svc.Create(context.Background(), CreateUserRequest{MobileNumber: "254711000111"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if resp == nil || repo.created == nil {
		t.Fatal("user not created")
	}
	// Defaults applied.
	if repo.created.CountryCode != "KE" {
		t.Errorf("CountryCode = %q, want KE", repo.created.CountryCode)
	}
	if repo.created.Role != "user" {
		t.Errorf("Role = %q, want user", repo.created.Role)
	}
}

func TestUserGetByID_NotFound(t *testing.T) {
	svc := NewService(&fakeUserRepo{byIDErr: repository.ErrUserNotFound})
	if _, err := svc.GetByID(context.Background(), "nope"); !errors.Is(err, ErrUserNotFound) {
		t.Errorf("err = %v, want ErrUserNotFound", err)
	}
}
