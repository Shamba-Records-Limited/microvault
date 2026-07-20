package user

import (
	"context"
	"errors"
	"log"
	"slices"
	"time"

	"github.com/Shamba-Records-Limited/microvault/pkg/models"
	"github.com/Shamba-Records-Limited/microvault/pkg/repository"
	"github.com/Shamba-Records-Limited/microvault/pkg/services"
	"gorm.io/gorm"
)

// Valid KYC status transitions
var validKYCTransitions = map[string][]string{
	"pending":  {"verified", "rejected"},
	"verified": {"expired"}, // Verified KYC can expire
	"expired":  {"pending"}, // Expired KYC needs re-verification
	"rejected": {"pending"}, // Rejected KYC can be resubmitted
}

// Valid user status transitions
var validStatusTransitions = map[string][]string{
	"active":    {"suspended", "blocked", "frozen", "closed"},
	"suspended": {"active", "blocked", "frozen", "closed"},
	"frozen":    {"active", "blocked", "closed"},
	"blocked":   {"active", "closed"}, // Can unblock to active or permanently close
	"closed":    {},                   // Cannot transition from closed
}

// Valid roles
var validRoles = map[string]bool{
	"user":  true,
	"admin": true,
	"agent": true,
}

// Valid KYC statuses
var validKYCStatuses = map[string]bool{
	"pending":  true,
	"verified": true,
	"rejected": true,
	"expired":  true,
}

// Valid user statuses
var validUserStatuses = map[string]bool{
	"active":    true,
	"suspended": true,
	"blocked":   true,
	"frozen":    true,
	"closed":    true,
}

// Service defines the interface for user business logic operations
type Service interface {
	// User management
	Create(ctx context.Context, req CreateUserRequest) (*UserResponse, error)
	CreateWithTx(ctx context.Context, tx *gorm.DB, req CreateUserRequest) (*UserResponse, error)
	GetByID(ctx context.Context, id string) (*UserResponse, error)
	GetByMobileNumber(ctx context.Context, mobileNumber string) (*UserResponse, error)
	GetByNationalID(ctx context.Context, nationalID string) (*UserResponse, error)
	List(ctx context.Context, filters UserFilters, pagination services.Pagination) (*services.PaginatedResponse[UserResponse], error)
	Update(ctx context.Context, id string, req UpdateUserRequest) (*UserResponse, error)
	Delete(ctx context.Context, requesterID, targetID string) error
	Restore(ctx context.Context, requesterID, targetID string) error

	// KYC management
	UpdateKYCStatus(ctx context.Context, adminID, userID string, req UpdateKYCStatusRequest) (*UserResponse, error)

	// Status management
	UpdateUserStatus(ctx context.Context, adminID, userID string, req UpdateUserStatusRequest) (*UserResponse, error)

	// Role management
	UpdateUserRole(ctx context.Context, adminID, userID string, req UpdateUserRoleRequest) (*UserResponse, error)

	// Admin operations and analytics
	Count(ctx context.Context) (int64, error)
	CountByKYCStatus(ctx context.Context, kycStatus string) (int64, error)
	CountByRole(ctx context.Context, role string) (int64, error)
	CountAdmins(ctx context.Context) (int, error)
}

// service implements the Service interface
type service struct {
	repo repository.UserRepository
}

// NewService creates a new user service instance
func NewService(repo repository.UserRepository) Service {
	return &service{
		repo: repo,
	}
}

// Create creates a new user with business validation
func (s *service) Create(ctx context.Context, req CreateUserRequest) (*UserResponse, error) {
	// Validate input
	if err := s.validateCreateRequest(req); err != nil {
		return nil, err
	}

	// Check mobile number uniqueness
	_, err := s.repo.GetByMobileNumber(ctx, req.MobileNumber)
	if err == nil {
		return nil, ErrMobileNumberAlreadyExists
	}
	if !errors.Is(err, repository.ErrUserNotFound) {
		log.Printf("Create: failed to check mobile number uniqueness: %v", err)
		return nil, err
	}

	// Check national ID uniqueness if provided
	if req.NationalID != "" {
		_, err = s.repo.GetByNationalID(ctx, req.NationalID)
		if err == nil {
			return nil, ErrNationalIDAlreadyExists
		}
		if !errors.Is(err, repository.ErrUserNotFound) {
			log.Printf("Create: failed to check national ID uniqueness: %v", err)
			return nil, err
		}
	}

	// Set defaults
	if req.CountryCode == "" {
		req.CountryCode = "KE" // Default to Kenya
	}
	if req.PreferredLanguage == "" {
		req.PreferredLanguage = "en"
	}
	if req.Role == "" {
		req.Role = "user"
	}

	// Create user model
	user := &models.User{
		MobileNumber:      req.MobileNumber,
		CountryCode:       req.CountryCode,
		MobileNetworkCode: req.MobileNetworkCode,
		MomoNetworkCode:   req.MomoNetworkCode,
		MomoNetworkName:   req.MomoNetworkName,
		TelcoName:         req.TelcoName,
		PreferredLanguage: req.PreferredLanguage,
		Role:              req.Role,
		KYCStatus:         "pending",
		Status:            "active",
	}

	// Set optional pointer fields
	if req.FullName != "" {
		user.FullName = &req.FullName
	}
	if req.NationalID != "" {
		user.NationalID = &req.NationalID
	}

	// Create user in database
	if err := s.repo.Create(ctx, user); err != nil {
		log.Printf("Create: failed to create user: %v", err)
		return nil, err
	}

	return toUserResponse(user), nil
}

// CreateWithTx creates a new user with a transaction
func (s *service) CreateWithTx(ctx context.Context, tx *gorm.DB, req CreateUserRequest) (*UserResponse, error) {
	if err := s.validateCreateRequest(req); err != nil {
		return nil, err
	}

	user := &models.User{
		MobileNumber:      req.MobileNumber,
		CountryCode:       req.CountryCode,
		MobileNetworkCode: req.MobileNetworkCode,
		MomoNetworkCode:   req.MomoNetworkCode,
		MomoNetworkName:   req.MomoNetworkName,
		TelcoName:         req.TelcoName,
		PreferredLanguage: req.PreferredLanguage,
		Role:              req.Role,
		KYCStatus:         "pending",
		Status:            "active",
	}

	// Set optional pointer fields
	if req.FullName != "" {
		user.FullName = &req.FullName
	}
	if req.NationalID != "" {
		user.NationalID = &req.NationalID
	}
	if req.BirthDate != nil {
		user.BirthDate = req.BirthDate
	}
	if req.Address != "" {
		user.Address = &req.Address
	}
	if req.City != "" {
		user.City = &req.City
	}
	if req.PostalCode != "" {
		user.PostalCode = &req.PostalCode
	}
	if req.StateOrProvince != "" {
		user.StateOrProvince = &req.StateOrProvince
	}

	if err := s.repo.CreateWithTx(ctx, tx, user); err != nil {
		log.Printf("CreateWithTx: failed to create user: %v", err)
		return nil, err
	}

	return toUserResponse(user), nil
}

// GetByID retrieves a user by ID
func (s *service) GetByID(ctx context.Context, id string) (*UserResponse, error) {
	user, err := s.repo.GetByID(ctx, id)
	if err != nil {
		if errors.Is(err, repository.ErrUserNotFound) {
			return nil, ErrUserNotFound
		}
		log.Printf("GetByID: failed to get user: %v", err)
		return nil, err
	}

	return toUserResponse(user), nil
}

// GetByMobileNumber retrieves a user by mobile number
func (s *service) GetByMobileNumber(ctx context.Context, mobileNumber string) (*UserResponse, error) {
	user, err := s.repo.GetByMobileNumber(ctx, mobileNumber)
	if err != nil {
		if errors.Is(err, repository.ErrUserNotFound) {
			return nil, ErrUserNotFound
		}
		log.Printf("GetByMobileNumber: failed to get user: %v", err)
		return nil, err
	}

	return toUserResponse(user), nil
}

// GetByNationalID retrieves a user by national ID
func (s *service) GetByNationalID(ctx context.Context, nationalID string) (*UserResponse, error) {
	user, err := s.repo.GetByNationalID(ctx, nationalID)
	if err != nil {
		if errors.Is(err, repository.ErrUserNotFound) {
			return nil, ErrUserNotFound
		}
		log.Printf("GetByNationalID: failed to get user: %v", err)
		return nil, err
	}

	return toUserResponse(user), nil
}

// List retrieves users with filters and pagination
func (s *service) List(ctx context.Context, filters UserFilters, pagination services.Pagination) (*services.PaginatedResponse[UserResponse], error) {
	// Set default pagination
	if pagination.Page <= 0 {
		pagination.Page = 1
	}
	if pagination.PageSize <= 0 {
		pagination.PageSize = 10
	}
	if pagination.PageSize > 100 {
		pagination.PageSize = 100 // Max page size
	}

	offset := (pagination.Page - 1) * pagination.PageSize

	var users []*models.User
	var totalCount int64
	var err error

	// Apply filters and get both data and count
	if filters.KYCStatus != "" {
		// Get users by KYC status
		users, err = s.repo.GetByKYCStatus(ctx, filters.KYCStatus, pagination.PageSize, offset)
		if err != nil {
			log.Printf("List: failed to get users by KYC status: %v", err)
			return nil, err
		}
		// Get total count for this filter
		totalCount, err = s.repo.CountByKYCStatus(ctx, filters.KYCStatus)
		if err != nil {
			log.Printf("List: failed to count users by KYC status: %v", err)
			return nil, err
		}
	} else if filters.Role != "" {
		// Get users by role
		users, err = s.repo.GetByRole(ctx, filters.Role, pagination.PageSize, offset)
		if err != nil {
			log.Printf("List: failed to get users by role: %v", err)
			return nil, err
		}
		// Get total count for this filter
		totalCount, err = s.repo.CountByRole(ctx, filters.Role)
		if err != nil {
			log.Printf("List: failed to count users by role: %v", err)
			return nil, err
		}
	} else {
		// Get all users
		users, err = s.repo.List(ctx, pagination.PageSize, offset)
		if err != nil {
			log.Printf("List: failed to get users: %v", err)
			return nil, err
		}
		// Get total count of all users
		totalCount, err = s.repo.Count(ctx)
		if err != nil {
			log.Printf("List: failed to count users: %v", err)
			return nil, err
		}
	}

	// Convert to response DTOs
	responses := make([]UserResponse, len(users))
	for i, user := range users {
		responses[i] = *toUserResponse(user)
	}

	return &services.PaginatedResponse[UserResponse]{
		Data:       responses,
		Page:       pagination.Page,
		PageSize:   pagination.PageSize,
		TotalCount: int(totalCount),
	}, nil
}

// Update updates user information
func (s *service) Update(ctx context.Context, id string, req UpdateUserRequest) (*UserResponse, error) {
	// Get existing user
	user, err := s.repo.GetByID(ctx, id)
	if err != nil {
		if errors.Is(err, repository.ErrUserNotFound) {
			return nil, ErrUserNotFound
		}
		log.Printf("Update: failed to get user: %v", err)
		return nil, err
	}

	// Check if user is deleted
	if user.DeletedAt != nil {
		return nil, ErrCannotModifyDeletedUser
	}

	// Check national ID uniqueness if being updated
	if req.NationalID != nil && *req.NationalID != "" {
		if user.NationalID == nil || *user.NationalID != *req.NationalID {
			existingUser, err := s.repo.GetByNationalID(ctx, *req.NationalID)
			if err == nil && existingUser.ID != user.ID {
				return nil, ErrNationalIDAlreadyExists
			}
			if err != nil && !errors.Is(err, repository.ErrUserNotFound) {
				log.Printf("Update: failed to check national ID uniqueness: %v", err)
				return nil, err
			}
		}
	}

	// Update fields
	if req.FullName != nil {
		user.FullName = req.FullName
	}
	if req.NationalID != nil {
		user.NationalID = req.NationalID
	}
	if req.PreferredLanguage != nil {
		user.PreferredLanguage = *req.PreferredLanguage
	}
	if req.BirthDate != nil {
		user.BirthDate = req.BirthDate
	}
	if req.Address != nil {
		user.Address = req.Address
	}
	if req.City != nil {
		user.City = req.City
	}
	if req.PostalCode != nil {
		user.PostalCode = req.PostalCode
	}
	if req.StateOrProvince != nil {
		user.StateOrProvince = req.StateOrProvince
	}

	// Update in database
	if err := s.repo.Update(ctx, user); err != nil {
		log.Printf("Update: failed to update user: %v", err)
		return nil, err
	}

	return toUserResponse(user), nil
}

// Delete soft deletes a user with business rules validation
func (s *service) Delete(ctx context.Context, requesterID, targetID string) error {
	// Check if trying to delete self
	if requesterID == targetID {
		return ErrCannotDeleteSelf
	}

	// Get requester to check permissions
	requester, err := s.repo.GetByID(ctx, requesterID)
	if err != nil {
		if errors.Is(err, repository.ErrUserNotFound) {
			return services.ErrUnauthorized
		}
		log.Printf("Delete: failed to get requester: %v", err)
		return err
	}

	// Only admins can delete users
	if requester.Role != "admin" {
		return services.ErrForbidden
	}

	// Get target user
	targetUser, err := s.repo.GetByID(ctx, targetID)
	if err != nil {
		if errors.Is(err, repository.ErrUserNotFound) {
			return ErrUserNotFound
		}
		log.Printf("Delete: failed to get target user: %v", err)
		return err
	}

	// Check if user is already deleted
	if targetUser.DeletedAt != nil {
		return ErrUserAlreadyDeleted
	}

	// Cannot delete last admin
	if targetUser.Role == "admin" {
		count, err := s.repo.CountAdmins(ctx)
		if err != nil {
			log.Printf("Delete: failed to count admins: %v", err)
			return err
		}
		if count <= 1 {
			return ErrCannotDeleteLastAdmin
		}
	}

	// Delete user
	if err := s.repo.Delete(ctx, targetID); err != nil {
		log.Printf("Delete: failed to delete user: %v", err)
		return err
	}

	return nil
}

// Restore restores a soft-deleted user
func (s *service) Restore(ctx context.Context, requesterID, targetID string) error {
	// Get requester to check permissions
	requester, err := s.repo.GetByID(ctx, requesterID)
	if err != nil {
		if errors.Is(err, repository.ErrUserNotFound) {
			return services.ErrUnauthorized
		}
		log.Printf("Restore: failed to get requester: %v", err)
		return err
	}

	// Only admins can restore users
	if requester.Role != "admin" {
		return services.ErrForbidden
	}

	// Restore user
	if err := s.repo.Restore(ctx, targetID); err != nil {
		if errors.Is(err, repository.ErrUserNotFound) {
			return ErrUserNotFound
		}
		log.Printf("Restore: failed to restore user: %v", err)
		return err
	}

	return nil
}

// UpdateKYCStatus updates user KYC status with transition validation
func (s *service) UpdateKYCStatus(ctx context.Context, adminID, userID string, req UpdateKYCStatusRequest) (*UserResponse, error) {
	// Validate KYC status
	if !validKYCStatuses[req.KYCStatus] {
		return nil, ErrInvalidKYCStatus
	}

	// Get admin to check permissions
	admin, err := s.repo.GetByID(ctx, adminID)
	if err != nil {
		if errors.Is(err, repository.ErrUserNotFound) {
			return nil, services.ErrUnauthorized
		}
		log.Printf("UpdateKYCStatus: failed to get admin: %v", err)
		return nil, err
	}

	// Only admins can update KYC status
	if admin.Role != "admin" {
		return nil, services.ErrForbidden
	}

	// Get user
	user, err := s.repo.GetByID(ctx, userID)
	if err != nil {
		if errors.Is(err, repository.ErrUserNotFound) {
			return nil, ErrUserNotFound
		}
		log.Printf("UpdateKYCStatus: failed to get user: %v", err)
		return nil, err
	}

	// Validate state transition
	if !s.isValidKYCTransition(user.KYCStatus, req.KYCStatus) {
		return nil, ErrInvalidKYCTransition
	}

	// Update KYC status
	user.KYCStatus = req.KYCStatus
	if req.KYCStatus == "verified" {
		now := time.Now()
		user.KYCVerifiedAt = &now
	}

	// Update in database
	if err := s.repo.Update(ctx, user); err != nil {
		log.Printf("UpdateKYCStatus: failed to update user: %v", err)
		return nil, err
	}

	return toUserResponse(user), nil
}

// UpdateUserStatus updates user status with transition validation
func (s *service) UpdateUserStatus(ctx context.Context, adminID, userID string, req UpdateUserStatusRequest) (*UserResponse, error) {
	// Validate status
	if !validUserStatuses[req.Status] {
		return nil, ErrInvalidUserStatus
	}

	// Get admin to check permissions
	admin, err := s.repo.GetByID(ctx, adminID)
	if err != nil {
		if errors.Is(err, repository.ErrUserNotFound) {
			return nil, services.ErrUnauthorized
		}
		log.Printf("UpdateUserStatus: failed to get admin: %v", err)
		return nil, err
	}

	// Only admins can update user status
	if admin.Role != "admin" {
		return nil, services.ErrForbidden
	}

	// Get user
	user, err := s.repo.GetByID(ctx, userID)
	if err != nil {
		if errors.Is(err, repository.ErrUserNotFound) {
			return nil, ErrUserNotFound
		}
		log.Printf("UpdateUserStatus: failed to get user: %v", err)
		return nil, err
	}

	// Validate state transition
	if !s.isValidStatusTransition(user.Status, req.Status) {
		return nil, ErrInvalidStatusTransition
	}

	// Update status
	user.Status = req.Status

	// Update in database
	if err := s.repo.Update(ctx, user); err != nil {
		log.Printf("UpdateUserStatus: failed to update user: %v", err)
		return nil, err
	}

	return toUserResponse(user), nil
}

// UpdateUserRole updates user role with validation
func (s *service) UpdateUserRole(ctx context.Context, adminID, userID string, req UpdateUserRoleRequest) (*UserResponse, error) {
	// Validate role
	if !validRoles[req.Role] {
		return nil, ErrInvalidRole
	}

	// Cannot change own role
	if adminID == userID {
		return nil, ErrCannotChangeOwnRole
	}

	// Get admin to check permissions
	admin, err := s.repo.GetByID(ctx, adminID)
	if err != nil {
		if errors.Is(err, repository.ErrUserNotFound) {
			return nil, services.ErrUnauthorized
		}
		log.Printf("UpdateUserRole: failed to get admin: %v", err)
		return nil, err
	}

	// Only admins can update user roles
	if admin.Role != "admin" {
		return nil, services.ErrForbidden
	}

	// Get user
	user, err := s.repo.GetByID(ctx, userID)
	if err != nil {
		if errors.Is(err, repository.ErrUserNotFound) {
			return nil, ErrUserNotFound
		}
		log.Printf("UpdateUserRole: failed to get user: %v", err)
		return nil, err
	}

	// If demoting from admin, check if last admin
	if user.Role == "admin" && req.Role != "admin" {
		count, err := s.repo.CountAdmins(ctx)
		if err != nil {
			log.Printf("UpdateUserRole: failed to count admins: %v", err)
			return nil, err
		}
		if count <= 1 {
			return nil, ErrCannotDeleteLastAdmin
		}
	}

	// Update role
	user.Role = req.Role

	// Update in database
	if err := s.repo.Update(ctx, user); err != nil {
		log.Printf("UpdateUserRole: failed to update user: %v", err)
		return nil, err
	}

	return toUserResponse(user), nil
}

// CountAdmins returns the number of admins
func (s *service) CountAdmins(ctx context.Context) (int, error) {
	count, err := s.repo.CountAdmins(ctx)
	if err != nil {
		log.Printf("CountAdmins: failed to count admins: %v", err)
		return 0, err
	}
	return count, nil
}

// Count returns the total number of users
func (s *service) Count(ctx context.Context) (int64, error) {
	count, err := s.repo.Count(ctx)
	if err != nil {
		log.Printf("Count: failed to count users: %v", err)
		return 0, err
	}
	return count, nil
}

// CountByKYCStatus returns the number of users with a specific KYC status
func (s *service) CountByKYCStatus(ctx context.Context, kycStatus string) (int64, error) {
	// Validate KYC status
	if !validKYCStatuses[kycStatus] {
		return 0, ErrInvalidKYCStatus
	}

	count, err := s.repo.CountByKYCStatus(ctx, kycStatus)
	if err != nil {
		log.Printf("CountByKYCStatus: failed to count users by KYC status: %v", err)
		return 0, err
	}
	return count, nil
}

// CountByRole returns the number of users with a specific role
func (s *service) CountByRole(ctx context.Context, role string) (int64, error) {
	// Validate role
	if !validRoles[role] {
		return 0, ErrInvalidRole
	}

	count, err := s.repo.CountByRole(ctx, role)
	if err != nil {
		log.Printf("CountByRole: failed to count users by role: %v", err)
		return 0, err
	}
	return count, nil
}

// --- Helper functions ---

// validateCreateRequest validates the create user request
func (s *service) validateCreateRequest(req CreateUserRequest) error {
	if req.MobileNumber == "" {
		return ErrInvalidMobileNumber
	}

	if req.Role != "" && !validRoles[req.Role] {
		return ErrInvalidRole
	}

	return nil
}

// isValidKYCTransition checks if a KYC status transition is valid
func (s *service) isValidKYCTransition(from, to string) bool {
	if from == to {
		return true // No change is valid
	}

	validTransitions, exists := validKYCTransitions[from]
	if !exists {
		return false
	}

	return slices.Contains(validTransitions, to)
}

// isValidStatusTransition checks if a status transition is valid
func (s *service) isValidStatusTransition(from, to string) bool {
	if from == to {
		return true // No change is valid
	}

	validTransitions, exists := validStatusTransitions[from]
	if !exists {
		return false
	}

	return slices.Contains(validTransitions, to)
}

// toUserResponse converts a user model to response DTO
func toUserResponse(user *models.User) *UserResponse {
	resp := &UserResponse{
		ID:                user.ID,
		MobileNumber:      user.MobileNumber,
		CountryCode:       user.CountryCode,
		MobileNetworkCode: user.MobileNetworkCode,
		MomoNetworkCode:   user.MomoNetworkCode,
		MomoNetworkName:   user.MomoNetworkName,
		TelcoName:         user.TelcoName,
		KYCStatus:         user.KYCStatus,
		KYCVerifiedAt:     user.KYCVerifiedAt,
		PreferredLanguage: user.PreferredLanguage,
		Status:            user.Status,
		Role:              user.Role,
		CreatedAt:         user.CreatedAt,
		UpdatedAt:         user.UpdatedAt,
	}

	// Handle optional pointer fields
	if user.FullName != nil {
		resp.FullName = *user.FullName
	}
	if user.NationalID != nil {
		resp.NationalID = *user.NationalID
	}
	if user.BirthDate != nil {
		resp.BirthDate = user.BirthDate
	}
	if user.Address != nil {
		resp.Address = *user.Address
	}
	if user.City != nil {
		resp.City = *user.City
	}
	if user.PostalCode != nil {
		resp.PostalCode = *user.PostalCode
	}
	if user.StateOrProvince != nil {
		resp.StateOrProvince = *user.StateOrProvince
	}

	return resp
}
