package user

import "time"

// CreateUserRequest represents the request to create a new user
type CreateUserRequest struct {
	MobileNumber      string  `json:"mobile_number" validate:"required"`
	MobileCountryCode string  `json:"mobile_country_code" validate:"required"`
	FullName          *string `json:"full_name,omitempty"`
	NationalID        *string `json:"national_id,omitempty"`
	PreferredLanguage string  `json:"preferred_language,omitempty"`
	Role              string  `json:"role,omitempty"` // For admin creating users with specific roles
}

// UpdateUserRequest represents the request to update user information
type UpdateUserRequest struct {
	FullName          *string `json:"full_name,omitempty"`
	NationalID        *string `json:"national_id,omitempty"`
	PreferredLanguage *string `json:"preferred_language,omitempty"`
}

// UpdateKYCStatusRequest represents the request to update user KYC status
type UpdateKYCStatusRequest struct {
	KYCStatus string `json:"kyc_status" validate:"required,oneof=pending in_progress verified rejected"`
}

// UpdateUserStatusRequest represents the request to update user status
type UpdateUserStatusRequest struct {
	Status string `json:"status" validate:"required,oneof=active suspended inactive"`
}

// UpdateUserRoleRequest represents the request to update user role
type UpdateUserRoleRequest struct {
	Role string `json:"role" validate:"required,oneof=user admin agent"`
}

// UserResponse represents the response containing user information
type UserResponse struct {
	ID                string     `json:"id"`
	MobileNumber      string     `json:"mobile_number"`
	MobileCountryCode string     `json:"mobile_country_code"`
	FullName          *string    `json:"full_name,omitempty"`
	NationalID        *string    `json:"national_id,omitempty"`
	KYCStatus         string     `json:"kyc_status"`
	KYCVerifiedAt     *time.Time `json:"kyc_verified_at,omitempty"`
	PreferredLanguage string     `json:"preferred_language"`
	Status            string     `json:"status"`
	Role              string     `json:"role"`
	CreatedAt         time.Time  `json:"created_at"`
	UpdatedAt         time.Time  `json:"updated_at"`
}

// UserFilters represents the filters for listing users
type UserFilters struct {
	KYCStatus string `json:"kyc_status,omitempty"`
	Role      string `json:"role,omitempty"`
	Status    string `json:"status,omitempty"`
}
