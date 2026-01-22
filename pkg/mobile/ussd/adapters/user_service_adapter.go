package adapters

import (
	"context"
	"fmt"
	"log"

	"github.com/Shamba-Records-Limited/microvault/internal/core/services/account"
	"github.com/Shamba-Records-Limited/microvault/internal/core/services/stellar"
	"github.com/Shamba-Records-Limited/microvault/internal/core/services/user"
	"github.com/Shamba-Records-Limited/microvault/pkg/mobile/ussd"
	"github.com/google/uuid"
	"github.com/stellar/go-stellar-sdk/keypair"
	"github.com/tyler-smith/go-bip32"
	"gorm.io/gorm"
)

const (
	// Stellar BIP44 coin type is 148
	// BIP44 path: m/44'/148'/account_index'/0'/0'
	purposeIndex = 44 + 0x80000000  // 44' (hardened)
	coinIndex    = 148 + 0x80000000 // 148' (hardened) - Stellar
)

// UserServiceAdapter adapts the user and account services to implement the USSD UserService interface
type UserServiceAdapter struct {
	userService        user.Service
	accountService     account.Service
	stellarService     stellar.Service
	db                 *gorm.DB
	masterKey          *bip32.Key
	treasuryPrivateKey string
	treasuryPublicKey  string
	usdcIssuer         string
	enableMultiSig     bool
	lowThreshold       uint32
	mediumThreshold    uint32
	highThreshold      uint32
}

// NewUserServiceAdapter creates a new user service adapter
func NewUserServiceAdapter(
	userService user.Service,
	accountService account.Service,
	stellarService stellar.Service,
	db *gorm.DB,
	treasuryPrivateKey string,
	usdcIssuer string,
	enableMultiSig bool,
	lowThreshold uint32,
	mediumThreshold uint32,
	highThreshold uint32,
) (*UserServiceAdapter, error) {
	// Parse the treasury private key
	treasuryKP := keypair.MustParseFull(treasuryPrivateKey)

	// Get the raw seed (32 bytes) from the Stellar keypair
	// We'll use this as the master seed for BIP32 derivation
	treasurySeed := treasuryKP.Seed()

	// Create BIP32 master key from the treasury seed
	// Note: BIP32 expects a seed, we'll use the Stellar seed bytes as entropy
	masterKey, err := bip32.NewMasterKey([]byte(treasurySeed))
	if err != nil {
		log.Println("Error creating master key:", err)
		return nil, fmt.Errorf("failed to create master key: %w", err)
	}

	return &UserServiceAdapter{
		userService:        userService,
		accountService:     accountService,
		stellarService:     stellarService,
		db:                 db,
		masterKey:          masterKey,
		treasuryPrivateKey: treasuryPrivateKey,
		treasuryPublicKey:  treasuryKP.Address(),
		usdcIssuer:         usdcIssuer,
		enableMultiSig:     enableMultiSig,
	}, nil
}

// GetUserWithAccounts retrieves a user and their accounts by ID or phone number
func (a *UserServiceAdapter) GetUserWithAccounts(ctx context.Context, userIDOrPhone string) (interface{}, []interface{}, error) {
	var userResp *user.UserResponse
	var err error

	// Check if input is a valid UUID - if so, try GetByID first
	if _, uuidErr := uuid.Parse(userIDOrPhone); uuidErr == nil {
		// Input is a valid UUID, try to get by ID
		userResp, err = a.userService.GetByID(ctx, userIDOrPhone)
		if err != nil {
			// If not found by ID, try by mobile number as fallback
			userResp, err = a.userService.GetByMobileNumber(ctx, userIDOrPhone)
			if err != nil {
				return nil, nil, err
			}
		}
	} else {
		// Input is not a UUID (likely a phone number), get by mobile number
		userResp, err = a.userService.GetByMobileNumber(ctx, userIDOrPhone)
		if err != nil {
			return nil, nil, err
		}
	}

	// Convert UserResponse to map for generic interface
	userMap := map[string]interface{}{
		"id":                  userResp.ID,
		"mobile_number":       userResp.MobileNumber,
		"mobile_country_code": userResp.MobileCountryCode,
		"kyc_status":          userResp.KYCStatus,
		"status":              userResp.Status,
		"role":                userResp.Role,
		"preferred_language":  userResp.PreferredLanguage,
		"created_at":          userResp.CreatedAt,
		"updated_at":          userResp.UpdatedAt,
	}

	// Add optional fields if present
	if userResp.FullName != nil {
		userMap["full_name"] = *userResp.FullName
	}
	if userResp.NationalID != nil {
		userMap["national_id"] = *userResp.NationalID
	}
	if userResp.KYCVerifiedAt != nil {
		userMap["kyc_verified_at"] = *userResp.KYCVerifiedAt
	}

	// Get user's account
	accountResp, err := a.accountService.GetByUserID(ctx, userResp.ID)
	if err != nil {
		return userMap, []interface{}{}, nil
	}

	// Convert AccountResponse to map
	accountMap := map[string]interface{}{
		"id":            accountResp.ID,
		"user_id":       accountResp.UserID,
		"public_key":    accountResp.PublicKey,
		"account_index": accountResp.AccountIndex,
		"status":        accountResp.Status,
		"created_at":    accountResp.CreatedAt,
		"updated_at":    accountResp.UpdatedAt,
	}

	accounts := []interface{}{accountMap}

	return userMap, accounts, nil
}

// RegisterUser registers a new user and auto-creates a Stellar account
func (a *UserServiceAdapter) RegisterUser(ctx context.Context, req *ussd.RegisterUserRequest) (interface{}, []interface{}, error) {
	// Convert USSD RegisterUserRequest to user service CreateUserRequest
	fullName := req.FullName
	nationalID := req.NationalID

	createReq := user.CreateUserRequest{
		MobileNumber:      req.MobileNumber,
		MobileCountryCode: req.MobileCountryCode,
		FullName:          &fullName,
		NationalID:        &nationalID,
		PreferredLanguage: req.PreferredLanguage,
		Role:              "user",
	}

	// Set default country code if not provided
	if createReq.MobileCountryCode == "" {
		createReq.MobileCountryCode = "254" // Default to Kenya
	}

	// Set default language if not provided
	if createReq.PreferredLanguage == "" {
		createReq.PreferredLanguage = "en"
	}

	// BEGIN DATABASE TRANSACTION - Ensure atomic user + account creation
	tx := a.db.Begin()
	if tx.Error != nil {
		return nil, nil, fmt.Errorf("failed to begin transaction: %w", tx.Error)
	}

	// Ensure rollback on panic
	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
			log.Printf("Panic during user registration, rolled back transaction: %v", r)
		}
	}()

	// Create user in transaction context
	userResp, err := a.userService.CreateWithTx(ctx, tx, createReq)
	if err != nil {
		tx.Rollback()
		log.Printf("Failed to create user in transaction: %v", err)
		return nil, nil, fmt.Errorf("failed to create user: %w", err)
	}

	// Get next GLOBAL account index within transaction
	// BIP44 derivation requires globally unique index to avoid keypair collisions
	accountIndex, err := a.accountService.GetNextAccountIndexWithTx(ctx, tx)
	if err != nil {
		tx.Rollback()
		log.Printf("Failed to get next account index: %v", err)
		return nil, nil, fmt.Errorf("failed to get next account index: %w", err)
	}
	log.Printf("Assigned global account index %d for user %s", accountIndex, userResp.ID)

	// Derive child keypair using BIP44
	childKP, err := a.deriveChildKeypair(accountIndex)
	if err != nil {
		tx.Rollback()
		log.Printf("Failed to derive child keypair: %v", err)
		return nil, nil, fmt.Errorf("failed to derive child keypair: %w", err)
	}

	// Create account on Stellar network BEFORE creating database record
	// This ensures the account exists on the network before we commit to the database
	stellarReq := stellar.CreateAccountRequest{
		ChildKeypair:    childKP,
		StartingBalance: "0", // Fully sponsored
		EnableMultiSig:  a.enableMultiSig,
	}

	// Configure multi-sig if enabled
	if a.enableMultiSig {
		stellarReq.MultiSigConfig = &stellar.MultiSigConfig{
			TreasurySigner: a.treasuryPublicKey,
			TreasuryWeight: 1,
			ChildWeight:    0,
			LowThreshold:   a.lowThreshold,
			MedThreshold:   a.mediumThreshold,
			HighThreshold:  a.highThreshold,
		}
	}

	// Submit transaction to Stellar network
	err = a.stellarService.CreateSponsoredAccount(ctx, stellarReq)
	if err != nil {
		tx.Rollback()
		log.Printf("Failed to create Stellar account for user %s: %v", userResp.ID, err)
		return nil, nil, fmt.Errorf("failed to create stellar account: %w", err)
	}

	// Create account record in database with transaction
	accountResp, err := a.accountService.CreateWithTx(ctx, tx, account.CreateAccountRequest{
		UserID:       userResp.ID,
		PublicKey:    childKP.Address(),
		AccountIndex: &accountIndex, // Use the same index we used for BIP44 derivation
	})
	if err != nil {
		tx.Rollback()
		log.Printf("Failed to create account record for user %s: %v", userResp.ID, err)
		return nil, nil, fmt.Errorf("failed to create account record: %w", err)
	}

	// COMMIT TRANSACTION - Both user and account created successfully
	if err := tx.Commit().Error; err != nil {
		log.Printf("Failed to commit transaction for user %s: %v", userResp.ID, err)
		return nil, nil, fmt.Errorf("failed to commit transaction: %w", err)
	}

	log.Printf("Successfully registered user %s with Stellar account %s (BIP44 index: %d)",
		userResp.ID, childKP.Address(), accountIndex)

	// Convert UserResponse to map
	userMap := map[string]interface{}{
		"id":                  userResp.ID,
		"mobile_number":       userResp.MobileNumber,
		"mobile_country_code": userResp.MobileCountryCode,
		"kyc_status":          userResp.KYCStatus,
		"status":              userResp.Status,
		"role":                userResp.Role,
		"preferred_language":  userResp.PreferredLanguage,
		"created_at":          userResp.CreatedAt,
		"updated_at":          userResp.UpdatedAt,
	}

	// Add optional fields if present
	if userResp.FullName != nil {
		userMap["full_name"] = *userResp.FullName
	}
	if userResp.NationalID != nil {
		userMap["national_id"] = *userResp.NationalID
	}

	// Convert account to map
	accountMap := map[string]interface{}{
		"id":            accountResp.ID,
		"user_id":       accountResp.UserID,
		"public_key":    accountResp.PublicKey,
		"account_index": accountResp.AccountIndex,
		"status":        accountResp.Status,
		"created_at":    accountResp.CreatedAt,
		"updated_at":    accountResp.UpdatedAt,
	}

	return userMap, []interface{}{accountMap}, nil
}

// deriveChildKeypair derives a child keypair using BIP44 path: m/44'/148'/accountIndex'
func (a *UserServiceAdapter) deriveChildKeypair(accountIndex int) (*keypair.Full, error) {
	// BIP44 path for Stellar: m/44'/148'/accountIndex'/0'/0'
	// All indices are hardened (') for security

	// Derive: m/44'
	purpose, err := a.masterKey.NewChildKey(purposeIndex)
	if err != nil {
		return nil, fmt.Errorf("failed to derive purpose: %w", err)
	}

	// Derive: m/44'/148'
	coinType, err := purpose.NewChildKey(coinIndex)
	if err != nil {
		return nil, fmt.Errorf("failed to derive coin type: %w", err)
	}

	// Derive: m/44'/148'/accountIndex'
	accountKey, err := coinType.NewChildKey(uint32(accountIndex) + 0x80000000) // hardened
	if err != nil {
		return nil, fmt.Errorf("failed to derive account: %w", err)
	}

	// Get the private key bytes (32 bytes)
	privateKeyBytes := accountKey.Key

	// Convert to [32]byte array for Stellar SDK
	var seed32 [32]byte
	copy(seed32[:], privateKeyBytes)

	// Create Stellar keypair from derived seed
	childKP, err := keypair.FromRawSeed(seed32)
	if err != nil {
		return nil, fmt.Errorf("failed to create Stellar keypair from derived seed: %w", err)
	}

	return childKP, nil
}
