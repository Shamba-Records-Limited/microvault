package repository

import (
	"context"
	"errors"
	"log"
	"time"

	pkgErrors "github.com/Shamba-Records-Limited/microvault/pkg/errors"
	"github.com/Shamba-Records-Limited/microvault/pkg/models"
	"gorm.io/gorm"
)

// Common errors for UserRepository
var (
	ErrUserNotFound           = errors.New("user not found")
	ErrFailedToCreateUser     = errors.New("failed to create user")
	ErrFailedToGetUser        = errors.New("failed to get user")
	ErrFailedToGetUsers       = errors.New("failed to get users")
	ErrFailedToGetUsersByKYC  = errors.New("failed to get users by KYC status")
	ErrFailedToGetUsersByRole = errors.New("failed to get users by role")
	ErrFailedToCountUsers     = errors.New("failed to count users")
	ErrFailedToCountAdmins    = errors.New("failed to count admins")
	ErrFailedToUpdateUser     = errors.New("failed to update user")
	ErrFailedToDeleteUser     = errors.New("failed to delete user")
	ErrFailedToRestoreUser    = errors.New("failed to restore user")
)

// UserRepository defines the interface for user data access
type UserRepository interface {
	// Create operations
	Create(ctx context.Context, user *models.User) error
	CreateWithTx(ctx context.Context, tx *gorm.DB, user *models.User) error

	// Read operations
	GetByID(ctx context.Context, id string) (*models.User, error)
	GetByMobileNumber(ctx context.Context, mobileNumber string) (*models.User, error)
	GetByNationalID(ctx context.Context, nationalID string) (*models.User, error)
	GetByKYCStatus(ctx context.Context, kycStatus string, limit, offset int) ([]*models.User, error)
	GetByRole(ctx context.Context, role string, limit, offset int) ([]*models.User, error)
	List(ctx context.Context, limit, offset int) ([]*models.User, error)

	// Count operations
	Count(ctx context.Context) (int64, error)
	CountByKYCStatus(ctx context.Context, kycStatus string) (int64, error)
	CountByRole(ctx context.Context, role string) (int64, error)
	CountAdmins(ctx context.Context) (int, error)

	// Update operations
	Update(ctx context.Context, user *models.User) error

	// UpdateMobileNumber rebinds a user to a new MSISDN. Deliberately separate
	// from Update (which does not touch mobile_number) because this changes the
	// account's identity anchor — it is only used by new-SIM account recovery,
	// after the caller has verified ownership. The unique index on
	// mobile_number is the final guard against binding a number twice.
	UpdateMobileNumber(ctx context.Context, userID, mobileNumber string) error

	Restore(ctx context.Context, id string) error

	// Delete operations
	Delete(ctx context.Context, id string) error
}

// userRepository represents a repository for managing user data
type userRepository struct {
	db *gorm.DB
}

// NewUserRepository creates a new instance of UserRepository
func NewUserRepository(db *gorm.DB) (UserRepository, error) {
	if db == nil {
		return nil, pkgErrors.ErrNilDB
	}
	return &userRepository{db: db}, nil
}

// --- Create Operations ---

// Create creates a new user in the database
func (r *userRepository) Create(ctx context.Context, user *models.User) error {
	if user.Role == "" {
		user.Role = "user"
	}
	result := r.db.WithContext(ctx).Create(user)
	if result.Error != nil {
		log.Printf("Create: database error: %v", result.Error)
		return ErrFailedToCreateUser
	}
	return nil
}

// CreateWithTx creates a new user in the database with a transaction
func (r *userRepository) CreateWithTx(ctx context.Context, tx *gorm.DB, user *models.User) error {
	if user.Role == "" {
		user.Role = "user"
	}
	result := tx.Create(user)
	if result.Error != nil {
		log.Printf("CreateWithTx: database error: %v", result.Error)
		return ErrFailedToCreateUser
	}
	return nil
}

// --- Read Operations ---

// GetByID retrieves a user by their ID
func (r *userRepository) GetByID(ctx context.Context, id string) (*models.User, error) {
	var user models.User
	result := r.db.WithContext(ctx).
		Where("id = ? AND deleted_at IS NULL", id).
		First(&user)
	if errors.Is(result.Error, gorm.ErrRecordNotFound) {
		return nil, ErrUserNotFound
	}
	if result.Error != nil {
		log.Printf("GetByID: database error: %v", result.Error)
		return nil, ErrFailedToGetUser
	}
	return &user, nil
}

// GetByMobileNumber retrieves a user by their mobile number
func (r *userRepository) GetByMobileNumber(ctx context.Context, mobileNumber string) (*models.User, error) {
	var user models.User
	result := r.db.WithContext(ctx).
		Where("mobile_number = ? AND deleted_at IS NULL", mobileNumber).
		First(&user)
	if errors.Is(result.Error, gorm.ErrRecordNotFound) {
		return nil, ErrUserNotFound
	}
	if result.Error != nil {
		log.Printf("GetByMobileNumber: database error: %v", result.Error)
		return nil, ErrFailedToGetUser
	}
	return &user, nil
}

// GetByNationalID retrieves a user by their national ID
func (r *userRepository) GetByNationalID(ctx context.Context, nationalID string) (*models.User, error) {
	var user models.User
	result := r.db.WithContext(ctx).
		Where("national_id = ? AND deleted_at IS NULL", nationalID).
		First(&user)
	if errors.Is(result.Error, gorm.ErrRecordNotFound) {
		return nil, ErrUserNotFound
	}
	if result.Error != nil {
		log.Printf("GetByNationalID: database error: %v", result.Error)
		return nil, ErrFailedToGetUser
	}
	return &user, nil
}

// GetByKYCStatus retrieves users by KYC status
func (r *userRepository) GetByKYCStatus(ctx context.Context, kycStatus string, limit, offset int) ([]*models.User, error) {
	var users []*models.User
	result := r.db.WithContext(ctx).
		Where("kyc_status = ? AND deleted_at IS NULL", kycStatus).
		Order("created_at DESC").
		Limit(limit).
		Offset(offset).
		Find(&users)
	if result.Error != nil {
		log.Printf("GetByKYCStatus: database error: %v", result.Error)
		return nil, ErrFailedToGetUsersByKYC
	}
	return users, nil
}

// GetByRole retrieves users by role
func (r *userRepository) GetByRole(ctx context.Context, role string, limit, offset int) ([]*models.User, error) {
	var users []*models.User
	result := r.db.WithContext(ctx).
		Where("role = ? AND deleted_at IS NULL", role).
		Order("created_at DESC").
		Limit(limit).
		Offset(offset).
		Find(&users)
	if result.Error != nil {
		log.Printf("GetByRole: database error: %v", result.Error)
		return nil, ErrFailedToGetUsersByRole
	}
	return users, nil
}

// List retrieves a list of users from the database
func (r *userRepository) List(ctx context.Context, limit, offset int) ([]*models.User, error) {
	var users []*models.User
	result := r.db.WithContext(ctx).
		Where("deleted_at IS NULL").
		Order("created_at DESC").
		Limit(limit).
		Offset(offset).
		Find(&users)
	if result.Error != nil {
		log.Printf("List: database error: %v", result.Error)
		return nil, ErrFailedToGetUsers
	}
	return users, nil
}

// Count counts the total number of users in the database
func (r *userRepository) Count(ctx context.Context) (int64, error) {
	var count int64
	result := r.db.WithContext(ctx).
		Model(&models.User{}).
		Where("deleted_at IS NULL").
		Count(&count)
	if result.Error != nil {
		log.Printf("Count: database error: %v", result.Error)
		return 0, ErrFailedToCountUsers
	}
	return count, nil
}

// CountByKYCStatus counts users by KYC status
func (r *userRepository) CountByKYCStatus(ctx context.Context, kycStatus string) (int64, error) {
	var count int64
	result := r.db.WithContext(ctx).
		Model(&models.User{}).
		Where("kyc_status = ? AND deleted_at IS NULL", kycStatus).
		Count(&count)
	if result.Error != nil {
		log.Printf("CountByKYCStatus: database error: %v", result.Error)
		return 0, ErrFailedToCountUsers
	}
	return count, nil
}

// CountByRole counts users by role
func (r *userRepository) CountByRole(ctx context.Context, role string) (int64, error) {
	var count int64
	result := r.db.WithContext(ctx).
		Model(&models.User{}).
		Where("role = ? AND deleted_at IS NULL", role).
		Count(&count)
	if result.Error != nil {
		log.Printf("CountByRole: database error: %v", result.Error)
		return 0, ErrFailedToCountUsers
	}
	return count, nil
}

// CountAdmins counts the number of admins in the database
func (r *userRepository) CountAdmins(ctx context.Context) (int, error) {
	var count int64
	result := r.db.WithContext(ctx).
		Model(&models.User{}).
		Where("role = ? AND deleted_at IS NULL", "admin").
		Count(&count)
	if result.Error != nil {
		log.Printf("CountAdmins: database error: %v", result.Error)
		return 0, ErrFailedToCountAdmins
	}
	return int(count), nil
}

// --- Update Operations ---

// Update updates an existing user in the database
func (r *userRepository) Update(ctx context.Context, user *models.User) error {
	result := r.db.WithContext(ctx).
		Model(user).
		Where("id = ? AND deleted_at IS NULL", user.ID).
		Updates(map[string]interface{}{
			"full_name":          user.FullName,
			"birth_date":         user.BirthDate,
			"address":            user.Address,
			"city":               user.City,
			"postal_code":        user.PostalCode,
			"national_id":        user.NationalID,
			"kyc_status":         user.KYCStatus,
			"kyc_verified_at":    user.KYCVerifiedAt,
			"preferred_language": user.PreferredLanguage,
			"status":             user.Status,
			"role":               user.Role,
			"pin_hash":           user.PinHash,
			"pin_attempts":       user.PinAttempts,
			"pin_locked_until":   user.PinLockedUntil,
			"pin_set_at":         user.PinSetAt,
			"updated_at":         time.Now(),
		})
	if result.RowsAffected == 0 {
		return ErrUserNotFound
	}
	if result.Error != nil {
		log.Printf("Update: database error: %v", result.Error)
		return ErrFailedToUpdateUser
	}
	return nil
}

// Restore restores a soft-deleted user
// UpdateMobileNumber rebinds the user to a new MSISDN. See the interface docs.
func (r *userRepository) UpdateMobileNumber(ctx context.Context, userID, mobileNumber string) error {
	result := r.db.WithContext(ctx).
		Model(&models.User{}).
		Where("id = ? AND deleted_at IS NULL", userID).
		Updates(map[string]interface{}{
			"mobile_number": mobileNumber,
			"updated_at":    time.Now(),
		})
	if result.Error != nil {
		log.Printf("UpdateMobileNumber: database error: %v", result.Error)
		return ErrFailedToUpdateUser
	}
	if result.RowsAffected == 0 {
		return ErrUserNotFound
	}
	return nil
}

func (r *userRepository) Restore(ctx context.Context, id string) error {
	result := r.db.WithContext(ctx).
		Unscoped().
		Model(&models.User{}).
		Where("id = ?", id).
		Update("deleted_at", nil)
	if result.RowsAffected == 0 {
		return ErrUserNotFound
	}
	if result.Error != nil {
		log.Printf("Restore: database error: %v", result.Error)
		return ErrFailedToRestoreUser
	}
	return nil
}

// --- Delete Operations ---

// Delete soft deletes an existing user from the database
func (r *userRepository) Delete(ctx context.Context, id string) error {
	result := r.db.WithContext(ctx).
		Model(&models.User{}).
		Where("id = ? AND deleted_at IS NULL", id).
		Update("deleted_at", time.Now())
	if result.RowsAffected == 0 {
		return ErrUserNotFound
	}
	if result.Error != nil {
		log.Printf("Delete: database error: %v", result.Error)
		return ErrFailedToDeleteUser
	}
	return nil
}
