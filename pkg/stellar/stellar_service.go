package stellar

import (
	"context"

	"github.com/stellar/go-stellar-sdk/clients/rpcclient"

	"github.com/Shamba-Records-Limited/microvault/pkg/stellar/classic"
	"github.com/Shamba-Records-Limited/microvault/pkg/stellar/soroban"
	"github.com/Shamba-Records-Limited/microvault/pkg/stellar/types"
)

// Re-export types from the types package for external consumers
type (
	CreateAccountRequest                = types.CreateAccountRequest
	MultiSigConfig                      = types.MultiSigConfig
	SponsoredPaymentTransactionRequest  = types.SponsoredPaymentTransactionRequest
	SponsoredPaymentTransactionResponse = types.SponsoredPaymentTransactionResponse
	SendUSDCRequest                     = types.SendUSDCRequest
	SendUSDCResponse                    = types.SendUSDCResponse
	BorrowRequest                       = types.BorrowRequest
	BorrowResponse                      = types.BorrowResponse
	RepayRequest                        = types.RepayRequest
	RepayResponse                       = types.RepayResponse
	VaultStatus                         = types.VaultStatus
	ContractError                       = types.ContractError
)

// Re-export error variables
var (
	ErrFailedToBuildTransaction      = types.ErrFailedToBuildTransaction
	ErrFailedToSignTransaction       = types.ErrFailedToSignTransaction
	ErrFailedToSubmitTransaction     = types.ErrFailedToSubmitTransaction
	ErrFailedToSignWithSponsorKey    = types.ErrFailedToSignWithSponsorKey
	ErrFailedToConvertTransaction    = types.ErrFailedToConvertTransaction
	ErrFailedToLoadAccount           = types.ErrFailedToLoadAccount
	ErrFailedToConvertCreditAsset    = types.ErrFailedToConvertCreditAsset
	ErrInvalidStellarAddress         = types.ErrInvalidStellarAddress
	ErrInvalidTransactionAmount      = types.ErrInvalidTransactionAmount
	ErrFailedToValidateTrustline     = types.ErrFailedToValidateTrustline
	ErrMissingTrustline              = types.ErrMissingTrustline
	ErrTransactionRejected           = types.ErrTransactionRejected
	ErrStellarCoreOverloaded         = types.ErrStellarCoreOverloaded
	ErrTransactionFailed             = types.ErrTransactionFailed
	ErrTransactionNotSuccessful      = types.ErrTransactionNotSuccessful
	ErrContextCancelled              = types.ErrContextCancelled
	ErrTransactionFailedOnLedger     = types.ErrTransactionFailedOnLedger
	ErrUnknownTransactionStatus      = types.ErrUnknownTransactionStatus
	ErrTransactionTimeout            = types.ErrTransactionTimeout
	ErrFailedToGetTransactionDetails = types.ErrFailedToGetTransactionDetails
	ErrInvalidContractID             = types.ErrInvalidContractID
	ErrNoSimulationResult            = types.ErrNoSimulationResult
	ErrSimulationFailed              = types.ErrSimulationFailed
	ErrVaultPaused                   = types.ErrVaultPaused
	ErrSharesLocked                  = types.ErrSharesLocked
	ErrInsufficientBalance           = types.ErrInsufficientBalance
)

// Re-export function
var MapContractError = types.MapContractError

// Service composes both classic and soroban services
type Service interface {
	classic.Service
	soroban.Service
}

type service struct {
	classicService classic.Service
	sorobanService soroban.Service
}

// Classic service method delegation
func (s *service) CreateSponsoredAccount(ctx context.Context, req CreateAccountRequest) error {
	return s.classicService.CreateSponsoredAccount(ctx, req)
}

func (s *service) SponsoredPaymentTransaction(ctx context.Context, req SponsoredPaymentTransactionRequest) (*SponsoredPaymentTransactionResponse, error) {
	return s.classicService.SponsoredPaymentTransaction(ctx, req)
}

func (s *service) SendUSDC(ctx context.Context, req SendUSDCRequest) (*SendUSDCResponse, error) {
	return s.classicService.SendUSDC(ctx, req)
}

func (s *service) CheckUSDCTrustline(ctx context.Context, address string) (bool, error) {
	return s.classicService.CheckUSDCTrustline(ctx, address)
}

// Soroban view method delegation
func (s *service) GetTreasuryAddress(ctx context.Context) (string, error) {
	return s.sorobanService.GetTreasuryAddress(ctx)
}

func (s *service) GetTotalBorrowed(ctx context.Context) (int64, error) {
	return s.sorobanService.GetTotalBorrowed(ctx)
}

func (s *service) GetAvailableLiquidity(ctx context.Context) (int64, error) {
	return s.sorobanService.GetAvailableLiquidity(ctx)
}

func (s *service) GetTotalManagedAssets(ctx context.Context) (int64, error) {
	return s.sorobanService.GetTotalManagedAssets(ctx)
}

func (s *service) GetUtilizationRate(ctx context.Context) (int64, error) {
	return s.sorobanService.GetUtilizationRate(ctx)
}

func (s *service) GetBorrowAPR(ctx context.Context) (int64, error) {
	return s.sorobanService.GetBorrowAPR(ctx)
}

func (s *service) GetBorrowIndex(ctx context.Context) (int64, error) {
	return s.sorobanService.GetBorrowIndex(ctx)
}

func (s *service) IsUserLocked(ctx context.Context, userAddress string) (bool, error) {
	return s.sorobanService.IsUserLocked(ctx, userAddress)
}

func (s *service) GetLockPeriod(ctx context.Context) (uint64, error) {
	return s.sorobanService.GetLockPeriod(ctx)
}

func (s *service) GetRemainingLockTime(ctx context.Context, userAddress string) (uint64, error) {
	return s.sorobanService.GetRemainingLockTime(ctx, userAddress)
}

func (s *service) IsPaused(ctx context.Context) (bool, error) {
	return s.sorobanService.IsPaused(ctx)
}

// Soroban treasury method delegation
func (s *service) BorrowFromVault(ctx context.Context, req BorrowRequest) (*BorrowResponse, error) {
	return s.sorobanService.BorrowFromVault(ctx, req)
}

func (s *service) RepayToVault(ctx context.Context, req RepayRequest) (*RepayResponse, error) {
	return s.sorobanService.RepayToVault(ctx, req)
}

func (s *service) AccrueInterest(ctx context.Context) error {
	return s.sorobanService.AccrueInterest(ctx)
}

// Soroban admin method delegation
func (s *service) PauseVault(ctx context.Context) error {
	return s.sorobanService.PauseVault(ctx)
}

func (s *service) UnpauseVault(ctx context.Context) error {
	return s.sorobanService.UnpauseVault(ctx)
}

func (s *service) SetMaxDeposit(ctx context.Context, limit int64) error {
	return s.sorobanService.SetMaxDeposit(ctx, limit)
}

func (s *service) SetMaxWithdraw(ctx context.Context, limit int64) error {
	return s.sorobanService.SetMaxWithdraw(ctx, limit)
}

func (s *service) SetLockPeriod(ctx context.Context, periodSeconds uint64) error {
	return s.sorobanService.SetLockPeriod(ctx, periodSeconds)
}

// NewService creates a new Stellar service with both classic and Soroban support
func NewService(
	rpcClient *rpcclient.Client,
	networkPassphrase string,
	treasuryPrivateKey string,
	adminPrivateKey string,
	contractID string,
	usdcIssuer string,
) Service {
	return &service{
		classicService: classic.NewService(
			rpcClient,
			networkPassphrase,
			treasuryPrivateKey,
			usdcIssuer,
		),
		sorobanService: soroban.NewService(
			rpcClient,
			networkPassphrase,
			treasuryPrivateKey,
			adminPrivateKey,
			contractID,
		),
	}
}
