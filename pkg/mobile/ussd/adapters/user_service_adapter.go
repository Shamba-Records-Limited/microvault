package adapters

import (
	"context"
	"errors"
	"fmt"
	"log"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/stellar/go-stellar-sdk/keypair"
	"github.com/tyler-smith/go-bip32"
	"gorm.io/gorm"

	"github.com/Shamba-Records-Limited/microvault/pkg/account"
	"github.com/Shamba-Records-Limited/microvault/pkg/mobile/ussd"
	"github.com/Shamba-Records-Limited/microvault/pkg/models"
	"github.com/Shamba-Records-Limited/microvault/pkg/stellar"
	"github.com/Shamba-Records-Limited/microvault/pkg/user"
)

const (
	// Stellar BIP44 coin type is 148
	// BIP44 path: m/44'/148'/account_index'/0'/0'
	purposeIndex = 44 + 0x80000000  // 44' (hardened)
	coinIndex    = 148 + 0x80000000 // 148' (hardened) - Stellar
)

// WalletConfig contains configuration for HD wallet derivation and Stellar account creation
type WalletConfig struct {
	MasterKey          *bip32.Key
	TreasuryPrivateKey string
	TreasuryPublicKey  string
	USDCIssuer         string
	EnableMultiSig     bool
	LowThreshold       uint32
	MediumThreshold    uint32
	HighThreshold      uint32
}

// AlertService escalates conditions that need a human. Optional — a nil
// service degrades to logging.
type AlertService interface {
	AlertOps(subject, message string) error
}

// UserServiceAdapter adapts the user and account services to implement the USSD UserService interface
type UserServiceAdapter struct {
	userService    user.Service
	accountService account.Service
	stellarService stellar.Service
	networkMapper  *ussd.NetworkMapper
	logger         *slog.Logger
	alerts         AlertService
	db             *gorm.DB
	walletConfig   WalletConfig
}

// NewUserServiceAdapter creates a new user service adapter. logger and alerts
// may be nil; the logger then falls back to the default and alerts degrade to
// log lines.
func NewUserServiceAdapter(
	userService user.Service,
	accountService account.Service,
	stellarService stellar.Service,
	db *gorm.DB,
	walletCfg WalletConfig,
	logger *slog.Logger,
	alerts AlertService,
) (*UserServiceAdapter, error) {
	// Parse the treasury private key
	treasuryKP := keypair.MustParseFull(walletCfg.TreasuryPrivateKey)

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

	// Set derived values
	walletCfg.MasterKey = masterKey
	walletCfg.TreasuryPublicKey = treasuryKP.Address()

	if logger == nil {
		logger = slog.Default()
	}

	return &UserServiceAdapter{
		userService:    userService,
		accountService: accountService,
		stellarService: stellarService,
		db:             db,
		walletConfig:   walletCfg,
		logger:         logger.With("component", "user_service_adapter"),
		alerts:         alerts,
	}, nil
}

// alertOps escalates to the ops team, falling back to a log line when no alert
// service is wired.
func (a *UserServiceAdapter) alertOps(subject, message string) {
	if a.alerts == nil {
		a.logger.Error("ops alert", "subject", subject, "message", message)
		return
	}
	if err := a.alerts.AlertOps(subject, message); err != nil {
		a.logger.Error("failed to send ops alert", "subject", subject, "error", err)
	}
}

// GetUserWithAccounts retrieves a user and their accounts by ID or phone number
func (a *UserServiceAdapter) GetUserWithAccounts(ctx context.Context, userIDOrPhone string) (any, []any, error) {
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
	userMap := map[string]any{
		"id":                  userResp.ID,
		"mobile_number":       userResp.MobileNumber,
		"country_code":        userResp.CountryCode,
		"mobile_network_code": userResp.MobileNetworkCode,
		"momo_network_code":   userResp.MomoNetworkCode,
		"momo_network_name":   userResp.MomoNetworkName,
		"telco_name":          userResp.TelcoName,
		"full_name":           userResp.FullName,
		"national_id":         userResp.NationalID,
		"kyc_status":          userResp.KYCStatus,
		"status":              userResp.Status,
		"role":                userResp.Role,
		"preferred_language":  userResp.PreferredLanguage,
		"created_at":          userResp.CreatedAt,
		"updated_at":          userResp.UpdatedAt,
	}

	if userResp.BirthDate != nil {
		userMap["birth_date"] = userResp.BirthDate.Format("2006-01-02")
	}

	if userResp.Address != "" {
		userMap["address"] = userResp.Address
	}
	if userResp.City != "" {
		userMap["city"] = userResp.City
	}
	if userResp.PostalCode != "" {
		userMap["postal_code"] = userResp.PostalCode
	}

	if userResp.KYCVerifiedAt != nil {
		userMap["kyc_verified_at"] = *userResp.KYCVerifiedAt
	}

	// Get user's account
	accountResp, err := a.accountService.GetByUserID(ctx, userResp.ID)
	if err != nil {
		return userMap, []any{}, nil
	}

	// Convert AccountResponse to map
	accountMap := map[string]any{
		"id":            accountResp.ID,
		"user_id":       accountResp.UserID,
		"public_key":    accountResp.PublicKey,
		"account_index": accountResp.AccountIndex,
		"status":        accountResp.Status,
		"created_at":    accountResp.CreatedAt,
		"updated_at":    accountResp.UpdatedAt,
	}

	accounts := []any{accountMap}

	return userMap, accounts, nil
}

// buildSponsoredAccountReq assembles the sponsored-creation request for a child
// keypair, including multi-sig config when enabled. Shared by the registration
// path and EnsureOnChainAccount so both derive identical account parameters.
func (a *UserServiceAdapter) buildSponsoredAccountReq(childKP *keypair.Full) stellar.CreateAccountRequest {
	req := stellar.CreateAccountRequest{
		ChildKeypair:    childKP,
		StartingBalance: "0", // Fully sponsored
		EnableMultiSig:  a.walletConfig.EnableMultiSig,
	}
	if a.walletConfig.EnableMultiSig {
		req.MultiSigConfig = &stellar.MultiSigConfig{
			TreasurySigner: a.walletConfig.TreasuryPublicKey,
			TreasuryWeight: 1,
			ChildWeight:    0,
			LowThreshold:   a.walletConfig.LowThreshold,
			MedThreshold:   a.walletConfig.MediumThreshold,
			HighThreshold:  a.walletConfig.HighThreshold,
		}
	}
	return req
}

// EnsureOnChainAccount guarantees the child account at accountIndex exists on
// the Stellar network, creating it (sponsored) if missing. The account is a
// fund-less on-chain identity used for tracking/auditing, so lending must not
// proceed without it. Idempotent and safe to call before each disbursement;
// returns an error only when the account is absent and cannot be created.
func (a *UserServiceAdapter) EnsureOnChainAccount(ctx context.Context, accountIndex int, address string) error {
	exists, err := a.stellarService.AccountExists(ctx, address)
	if err != nil {
		return fmt.Errorf("check on-chain account %s: %w", address, err)
	}
	if exists {
		// Settles rows left pending by a dropped goroutine, marked failed by an
		// earlier attempt, or unknown because they predate the column.
		a.confirmChainStatus(ctx, address)
		return nil
	}

	childKP, err := a.deriveChildKeypair(accountIndex)
	if err != nil {
		return fmt.Errorf("derive child keypair for index %d: %w", accountIndex, err)
	}
	if childKP.Address() != address {
		return fmt.Errorf("derived address %s does not match stored %s (index %d)", childKP.Address(), address, accountIndex)
	}

	if err := a.stellarService.CreateSponsoredAccount(ctx, a.buildSponsoredAccountReq(childKP)); err != nil {
		// The account may have appeared between the check and the create (async
		// registration finishing, a concurrent ensure) — re-check before failing.
		if ok, e2 := a.stellarService.AccountExists(ctx, address); e2 == nil && ok {
			a.confirmChainStatus(ctx, address)
			return nil
		}
		return fmt.Errorf("create on-chain account %s: %w", address, err)
	}
	a.confirmChainStatus(ctx, address)
	a.logger.Info("created missing on-chain account",
		"address", address, "account_index", accountIndex)
	return nil
}

// confirmChainStatus marks the account behind address as present on-chain. Best
// effort: the account genuinely exists at this point, so a bookkeeping failure
// must not fail the caller — worst case a later ensure records it.
func (a *UserServiceAdapter) confirmChainStatus(ctx context.Context, address string) {
	if a.accountService == nil {
		return
	}
	acct, err := a.accountService.GetByPublicKey(ctx, address)
	if err != nil {
		a.logger.Warn("could not resolve account to confirm chain status",
			"address", address, "error", err)
		return
	}
	if acct.ChainStatus == models.ChainStatusConfirmed {
		return
	}
	a.setChainStatus(ctx, acct.ID, models.ChainStatusConfirmed)
}

// createSponsoredAccountAsync submits the child account's sponsored-creation
// transaction off the USSD request path. It uses a background context so it
// outlives the USSD turn, retries transient failures, and records the outcome
// on accounts.chain_status so a failure is queryable rather than log-only.
//
// The row is already committed by the time this runs — a failure here is not
// rolled back. accounts.chain_status is what marks it for reconciliation, and
// EnsureOnChainAccount is what heals it before lending.
func (a *UserServiceAdapter) createSponsoredAccountAsync(userID, accountID string, req stellar.CreateAccountRequest) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	address := req.ChildKeypair.Address()

	const attempts = 3
	var err error
	for attempt := 1; attempt <= attempts; attempt++ {
		if err = a.stellarService.CreateSponsoredAccount(ctx, req); err == nil {
			a.setChainStatus(ctx, accountID, models.ChainStatusConfirmed)
			a.logger.Info("sponsored Stellar account created",
				"user_id", userID, "account_id", accountID, "address", address)
			return
		}
		a.logger.Warn("sponsored account creation attempt failed",
			"user_id", userID, "account_id", accountID, "address", address,
			"attempt", attempt, "attempts", attempts, "error", err)
		time.Sleep(time.Duration(attempt) * 2 * time.Second)
	}

	// Marked before alerting: the durable record is what a reconciler reads,
	// and it must survive whether or not the alert is delivered.
	a.setChainStatus(ctx, accountID, models.ChainStatusFailed)
	a.logger.Error("sponsored account creation permanently failed — needs reconciliation",
		"user_id", userID, "account_id", accountID, "address", address, "error", err)
	a.alertOps("Stellar account creation failed",
		fmt.Sprintf("User %s account %s (%s) has no on-chain account after %d attempts: %v. "+
			"chain_status=failed; lending is blocked until it is healed.",
			userID, accountID, address, attempts, err))
}

// setChainStatus persists an observed on-chain state. Uses its own context so
// the write still lands when the caller's has expired mid-retry.
func (a *UserServiceAdapter) setChainStatus(ctx context.Context, accountID, status string) {
	if accountID == "" {
		return
	}
	if ctx.Err() != nil {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
	}
	if err := a.accountService.UpdateChainStatus(ctx, accountID, status); err != nil {
		a.logger.Error("failed to record account chain status",
			"account_id", accountID, "chain_status", status, "error", err)
	}
}

// UpdateBio sets the optional SEP-9 bio fields on an existing user. Only
// non-empty fields are applied; a malformed birth date is skipped.
func (a *UserServiceAdapter) UpdateBio(ctx context.Context, userID string, bio ussd.BioUpdate) error {
	req := user.UpdateUserRequest{}
	if bio.Address != "" {
		req.Address = &bio.Address
	}
	if bio.City != "" {
		req.City = &bio.City
	}
	if bio.PostalCode != "" {
		req.PostalCode = &bio.PostalCode
	}
	if bio.BirthDate != "" {
		if t, err := time.Parse("2006-01-02", bio.BirthDate); err == nil {
			req.BirthDate = &models.Date{Time: t}
		}
	}
	_, err := a.userService.Update(ctx, userID, req)
	return err
}

// GetUserIDByNationalID resolves the account behind a national ID, or "" if none.
func (a *UserServiceAdapter) GetUserIDByNationalID(ctx context.Context, nationalID string) (string, error) {
	userResp, err := a.userService.GetByNationalID(ctx, nationalID)
	if err != nil {
		if errors.Is(err, user.ErrUserNotFound) {
			return "", nil
		}
		return "", err
	}
	return userResp.ID, nil
}

// RebindMobileNumber moves an account to a new MSISDN (new-SIM recovery).
func (a *UserServiceAdapter) RebindMobileNumber(ctx context.Context, userID, mobileNumber string) error {
	return a.userService.RebindMobileNumber(ctx, userID, mobileNumber)
}

// NationalIDExists reports whether a user is already registered with nationalID.
func (a *UserServiceAdapter) NationalIDExists(ctx context.Context, nationalID string) (bool, error) {
	if _, err := a.userService.GetByNationalID(ctx, nationalID); err != nil {
		if errors.Is(err, user.ErrUserNotFound) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// RegisterUser registers a new user and auto-creates a Stellar account
func (a *UserServiceAdapter) RegisterUser(ctx context.Context, req *ussd.RegisterUserRequest) (any, []any, error) {
	// Map Africa's Talking network code to MoMo network info
	var momoNetworkCode, momoNetworkName, telcoName string
	var countryCode string

	if req.NetworkCode != "" && a.networkMapper != nil {
		networkInfo, err := a.networkMapper.MapMobileNetworkCode(req.NetworkCode)
		if err != nil {
			if a.logger != nil {
				a.logger.Warn("failed to map mobile network code",
					slog.String("mobile_network_code", req.NetworkCode),
					slog.String("error", err.Error()),
				)
			} else {
				log.Printf("failed to map mobile network code %s: %v", req.NetworkCode, err)
			}
		} else {
			momoNetworkCode = networkInfo.MomoNetworkCode
			momoNetworkName = networkInfo.MomoNetworkName
			telcoName = networkInfo.TelcoName
			countryCode = networkInfo.Country

			if a.logger != nil {
				a.logger.Info("mapped mobile network code",
					slog.String("at_network_code", req.NetworkCode),
					slog.String("momo_network_code", networkInfo.MomoNetworkCode),
					slog.String("momo_network_name", networkInfo.MomoNetworkName),
					slog.String("country", networkInfo.Country),
				)
			}
		}
	}

	// 2. Convert USSD RegisterUserRequest to user service CreateUserRequest
	createReq := user.CreateUserRequest{
		MobileNumber:      req.MobileNumber,
		CountryCode:       countryCode,
		MobileNetworkCode: req.NetworkCode,
		MomoNetworkCode:   momoNetworkCode,
		MomoNetworkName:   momoNetworkName,
		TelcoName:         telcoName,
		FullName:          req.FullName,
		NationalID:        req.NationalID,
		PreferredLanguage: req.PreferredLanguage,
		Role:              "user",
		Address:           req.Address,
		City:              req.City,
		PostalCode:        req.PostalCode,
	}
	if req.PinHash != "" {
		createReq.PinHash = &req.PinHash
		createReq.PinSetAt = req.PinSetAt
	}
	if req.BirthDate != "" {
		if t, err := time.Parse("2006-01-02", req.BirthDate); err == nil {
			createReq.BirthDate = &models.Date{Time: t}
		}
	}

	// Set default language if not provided
	if createReq.PreferredLanguage == "" {
		createReq.PreferredLanguage = "en"
	}

	// 3. BEGIN DATABASE TRANSACTION - Ensure atomic user + account creation
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

	// Build the sponsored-account creation request. The on-chain submit is
	// deferred to a background goroutine after commit: it takes several seconds
	// to confirm and the USSD gateway ends sessions on multi-second turns.
	stellarReq := a.buildSponsoredAccountReq(childKP)

	// Create account record in database with transaction
	accountResp, err := a.accountService.CreateWithTx(ctx, tx, account.CreateAccountRequest{
		UserID:    userResp.ID,
		PublicKey: childKP.Address(),
		// Stored verbatim: account_index is the BIP-44 derivation index, so
		// public_key must always re-derive from it exactly.
		AccountIndex: &accountIndex,
	})
	if err != nil {
		tx.Rollback()
		log.Printf("Failed to create account record for user %s: %v", userResp.ID, err)
		return nil, nil, fmt.Errorf("failed to create account record: %w", err)
	}

	// COMMIT TRANSACTION - user + account row created atomically
	if err := tx.Commit().Error; err != nil {
		log.Printf("Failed to commit transaction for user %s: %v", userResp.ID, err)
		return nil, nil, fmt.Errorf("failed to commit transaction: %w", err)
	}

	// Submit the on-chain sponsored-account creation off the USSD request path
	// so the turn returns immediately and the gateway session survives.
	go a.createSponsoredAccountAsync(userResp.ID, accountResp.ID, stellarReq)

	log.Printf("Registered user %s with account %s (BIP44 index: %d); on-chain creation dispatched",
		userResp.ID, childKP.Address(), accountIndex)

	// Convert UserResponse to map
	userMap := map[string]any{
		"id":                  userResp.ID,
		"mobile_number":       userResp.MobileNumber,
		"country_code":        userResp.CountryCode,
		"mobile_network_code": userResp.MobileNetworkCode,
		"momo_network_code":   userResp.MomoNetworkCode,
		"momo_network_name":   userResp.MomoNetworkName,
		"telco_name":          userResp.TelcoName,
		"full_name":           userResp.FullName,
		"national_id":         userResp.NationalID,
		"kyc_status":          userResp.KYCStatus,
		"status":              userResp.Status,
		"role":                userResp.Role,
		"preferred_language":  userResp.PreferredLanguage,
		"created_at":          userResp.CreatedAt,
		"updated_at":          userResp.UpdatedAt,
	}

	// Convert account to map
	accountMap := map[string]any{
		"id":            accountResp.ID,
		"user_id":       accountResp.UserID,
		"public_key":    accountResp.PublicKey,
		"account_index": accountResp.AccountIndex,
		"status":        accountResp.Status,
		"created_at":    accountResp.CreatedAt,
		"updated_at":    accountResp.UpdatedAt,
	}

	return userMap, []any{accountMap}, nil
}

// deriveChildKeypair derives a child keypair using BIP44 path: m/44'/148'/accountIndex'
//
// accountIndex is `accounts.account_index` unmodified. Callers must not offset
// it: the index is what identifies the key, so anything that shifts it derives
// a valid keypair for the wrong account. Indices are also never reclaimed —
// deleting a row does not delete the Stellar account it created — which is why
// account_index_seq is monotonic.
func (a *UserServiceAdapter) deriveChildKeypair(accountIndex int) (*keypair.Full, error) {
	// BIP44 path for Stellar: m/44'/148'/accountIndex'/0'/0'
	// All indices are hardened (') for security

	// Derive: m/44'
	purpose, err := a.walletConfig.MasterKey.NewChildKey(purposeIndex)
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
