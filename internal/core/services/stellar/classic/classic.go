package classic

import (
	"context"
	"fmt"
	"log"
	"log/slog"
	"strconv"

	"github.com/stellar/go-stellar-sdk/clients/rpcclient"
	"github.com/stellar/go-stellar-sdk/keypair"
	protocol "github.com/stellar/go-stellar-sdk/protocols/rpc"
	"github.com/stellar/go-stellar-sdk/protocols/stellarcore"
	"github.com/stellar/go-stellar-sdk/txnbuild"
	"github.com/stellar/go-stellar-sdk/xdr"

	"github.com/Shamba-Records-Limited/microvault/internal/core/services/stellar/rpc"
	"github.com/Shamba-Records-Limited/microvault/internal/core/services/stellar/types"
)

// RPCClient defines the interface for Stellar RPC operations
type RPCClient interface {
	LoadAccount(ctx context.Context, address string) (txnbuild.Account, error)
	SendTransaction(ctx context.Context, req protocol.SendTransactionRequest) (protocol.SendTransactionResponse, error)
	GetTransaction(ctx context.Context, req protocol.GetTransactionRequest) (protocol.GetTransactionResponse, error)
	GetLedgerEntries(ctx context.Context, req protocol.GetLedgerEntriesRequest) (protocol.GetLedgerEntriesResponse, error)
}

// Ensure rpcclient.Client implements RPCClient
var _ RPCClient = (*rpcclient.Client)(nil)

// Service defines the interface for Stellar classic operations.
type Service interface {
	// CreateSponsoredAccount creates a new child account with sponsorship and USDC trustline.
	CreateSponsoredAccount(ctx context.Context, req types.CreateAccountRequest) error

	// SponsoredPaymentTransaction executes a sponsored payment from a child account.
	SponsoredPaymentTransaction(ctx context.Context, req types.SponsoredPaymentTransactionRequest) (*types.SponsoredPaymentTransactionResponse, error)

	// SendUSDC sends USDC directly from the treasury wallet to an external Stellar address with a memo.
	SendUSDC(ctx context.Context, req types.SendUSDCRequest) (*types.SendUSDCResponse, error)
}

type service struct {
	rpcClient         RPCClient
	networkPassphrase string
	treasurySecretKey string
	usdcIssuer        string
	logger            *slog.Logger
}

// NewService creates a new classic Stellar service
func NewService(
	rpcClient *rpcclient.Client,
	networkPassphrase string,
	treasurySecretKey string,
	usdcIssuer string,
) Service {
	return &service{
		rpcClient:         rpcClient,
		networkPassphrase: networkPassphrase,
		treasurySecretKey: treasurySecretKey,
		usdcIssuer:        usdcIssuer,
		logger:            slog.Default().With(slog.String("service", "classic")),
	}
}

// NewServiceWithClient creates a new classic Stellar service with a custom RPC client
func NewServiceWithClient(
	rpcClient RPCClient,
	networkPassphrase string,
	treasurySecretKey string,
	usdcIssuer string,
) Service {
	return &service{
		rpcClient:         rpcClient,
		networkPassphrase: networkPassphrase,
		treasurySecretKey: treasurySecretKey,
		usdcIssuer:        usdcIssuer,
		logger:            slog.Default().With(slog.String("service", "classic")),
	}
}

// CreateSponsoredAccount implements the classic create account operation and establishes a trustline for provided asset through sponsorship
func (s *service) CreateSponsoredAccount(ctx context.Context, req types.CreateAccountRequest) error {
	// 1. Parse sponsor keypair
	sponsorKP := keypair.MustParseFull(s.treasurySecretKey)

	// 2. Get sponsor account from network
	sponsorAccount, err := s.rpcClient.LoadAccount(ctx, sponsorKP.Address())
	if err != nil {
		return types.ErrFailedToLoadAccount
	}

	// 3. Define asset configuration
	usdcAsset := txnbuild.CreditAsset{
		Code:   "USDC",
		Issuer: s.usdcIssuer,
	}

	// Convert to ChangeTrustAsset
	changeTrustAsset, err := usdcAsset.ToChangeTrustAsset()
	if err != nil {
		return types.ErrFailedToConvertCreditAsset
	}

	// 4. Bundle operations
	childPublicKey := req.ChildKeypair.Address()
	ops := []txnbuild.Operation{
		// Begin sponsorship of future reserves
		&txnbuild.BeginSponsoringFutureReserves{
			SponsoredID: childPublicKey,
		},

		// Create account
		&txnbuild.CreateAccount{
			Destination: childPublicKey,
			Amount:      req.StartingBalance,
		},

		// Add USDC trustline
		&txnbuild.ChangeTrust{
			Line:          changeTrustAsset,
			Limit:         txnbuild.MaxTrustlineLimit,
			SourceAccount: childPublicKey,
		},
	}

	// Add multi-sig operations if enabled
	if req.EnableMultiSig && req.MultiSigConfig != nil {
		ops = append(ops,
			&txnbuild.SetOptions{
				SourceAccount: childPublicKey,
				Signer: &txnbuild.Signer{
					Address: req.MultiSigConfig.TreasurySigner,
					Weight:  txnbuild.Threshold(req.MultiSigConfig.TreasuryWeight),
				},
			},
			&txnbuild.SetOptions{
				SourceAccount:   childPublicKey,
				MasterWeight:    txnbuild.NewThreshold(txnbuild.Threshold(req.MultiSigConfig.ChildWeight)),
				LowThreshold:    txnbuild.NewThreshold(txnbuild.Threshold(req.MultiSigConfig.LowThreshold)),
				MediumThreshold: txnbuild.NewThreshold(txnbuild.Threshold(req.MultiSigConfig.MedThreshold)),
				HighThreshold:   txnbuild.NewThreshold(txnbuild.Threshold(req.MultiSigConfig.HighThreshold)),
			},
		)
	}

	// End sponsorship of future reserves
	ops = append(ops, &txnbuild.EndSponsoringFutureReserves{
		SourceAccount: childPublicKey,
	})

	// 5. Build transaction
	tx, err := txnbuild.NewTransaction(
		txnbuild.TransactionParams{
			SourceAccount:        sponsorAccount,
			IncrementSequenceNum: true,
			BaseFee:              txnbuild.MinBaseFee,
			Preconditions: txnbuild.Preconditions{
				TimeBounds: txnbuild.NewTimeout(300),
			},
			Operations: ops,
		},
	)
	if err != nil {
		log.Println("CreateSponsoredAccount: failed to build transaction: %w", err)
		return types.ErrFailedToBuildTransaction
	}

	// 6. Sign transaction with network passphrase and sponsor keypair
	tx, err = tx.Sign(s.networkPassphrase, sponsorKP)
	if err != nil {
		log.Printf("CreateSponsoredAccount: failed to sign transaction with sponsor key: %v", err)
		return types.ErrFailedToSignWithSponsorKey
	}

	// 7. Sign with child keypair
	tx, err = tx.Sign(s.networkPassphrase, req.ChildKeypair)
	if err != nil {
		log.Printf("CreateSponsoredAccount: failed to sign transaction with child key: %v", err)
		return types.ErrFailedToSignTransaction
	}

	// 8. Convert transaction to XDR format
	xdrTx, err := tx.Base64()
	if err != nil {
		log.Println("CreateSponsoredAccount: failed to convert transaction to XDR: %w", err)
		return types.ErrFailedToConvertTransaction
	}

	// 9. Submit to Stellar network using RPC client
	txResponse, err := s.rpcClient.SendTransaction(ctx, protocol.SendTransactionRequest{
		Transaction: xdrTx,
	})
	if err != nil {
		log.Printf("CreateSponsoredAccount: failed to submit transaction: %v", err)
		return types.ErrFailedToSubmitTransaction
	}

	// 10. Check submission status
	log.Printf("CreateSponsoredAccount: transaction submission status: %s, hash: %s", txResponse.Status, txResponse.Hash)

	// Handle different submission statuses
	switch txResponse.Status {
	case stellarcore.TXStatusError:
		if txResponse.ErrorResultXDR != "" {
			log.Printf("CreateSponsoredAccount: transaction rejected: %s", txResponse.ErrorResultXDR)
		} else {
			log.Printf("CreateSponsoredAccount: transaction rejected by stellar-core")
		}
		return types.ErrTransactionRejected
	case stellarcore.TXStatusTryAgainLater:
		log.Printf("CreateSponsoredAccount: stellar-core is overloaded, try again later")
		return types.ErrStellarCoreOverloaded
	case stellarcore.TXStatusDuplicate:
		log.Printf("CreateSponsoredAccount: duplicate transaction detected")
	case stellarcore.TXStatusPending:
		log.Printf("CreateSponsoredAccount: transaction pending")
	default:
		log.Printf("CreateSponsoredAccount: unknown submission status: %s", txResponse.Status)
	}

	txHash := txResponse.Hash
	s.logger.Info("transaction submitted, waiting for confirmation",
		slog.String("operation", "CreateSponsoredAccount"),
		slog.String("tx_hash", txHash),
	)

	// 11. Wait for transaction to be applied to the ledger
	pollCfg := rpc.DefaultPollConfig()
	pollCfg.Logger = s.logger
	txResult, err := rpc.PollTransaction(ctx, s.rpcClient, txHash, pollCfg)
	if err != nil {
		s.logger.Error("transaction failed",
			slog.String("operation", "CreateSponsoredAccount"),
			slog.String("tx_hash", txHash),
			slog.String("error", err.Error()),
		)
		return types.ErrTransactionFailed
	}

	if txResult.Status != protocol.TransactionStatusSuccess {
		s.logger.Error("transaction not successful",
			slog.String("operation", "CreateSponsoredAccount"),
			slog.String("tx_hash", txHash),
			slog.String("status", txResult.Status),
		)
		return types.ErrTransactionNotSuccessful
	}

	s.logger.Info("account created successfully on Stellar",
		slog.String("operation", "CreateSponsoredAccount"),
		slog.String("account", childPublicKey),
		slog.String("tx_hash", txHash),
	)
	return nil
}

// SponsoredPaymentTransaction implements the classic payment operation using sponsorship
func (s *service) SponsoredPaymentTransaction(ctx context.Context, req types.SponsoredPaymentTransactionRequest) (*types.SponsoredPaymentTransactionResponse, error) {
	// 1. Validate request
	destination, err := keypair.ParseAddress(req.Destination)
	if err != nil {
		log.Printf("SponsoredPaymentTransaction: invalid stellar address: %s", req.Destination)
		return nil, types.ErrInvalidStellarAddress
	}

	if req.Amount <= 0 {
		log.Printf("SponsoredPaymentTransaction: invalid transaction amount: %d", req.Amount)
		return nil, types.ErrInvalidTransactionAmount
	}

	// 2. Validate destination has asset trustline
	destinationAccount, err := s.rpcClient.LoadAccount(ctx, destination.Address())
	if err != nil {
		log.Printf("SponsoredPaymentTransaction: failed to load destination account: %s", req.Destination)
		return nil, types.ErrFailedToLoadAccount
	}
	result, err := hasAssetTrustline(ctx, s.rpcClient, destinationAccount.GetAccountID(), req.AssetCode, req.AssetIssuer)
	if err != nil {
		log.Printf("SponsoredPaymentTransaction: failed to validate %s trustline for destination: %s", req.AssetCode, req.Destination)
		return nil, types.ErrFailedToValidateTrustline
	}

	if !result {
		log.Printf("SponsoredPaymentTransaction: destination account %s does not have a %s trustline", req.Destination, req.AssetCode)
		return nil, types.ErrMissingTrustline
	}

	// 3. Build sponsored payment transaction operations
	sponsorKP := keypair.MustParseFull(s.treasurySecretKey)

	sponsorAccount, err := s.rpcClient.LoadAccount(ctx, sponsorKP.Address())
	if err != nil {
		return nil, types.ErrFailedToLoadAccount
	}

	paymentAsset := txnbuild.CreditAsset{
		Code:   req.AssetCode,
		Issuer: req.AssetIssuer,
	}

	ops := []txnbuild.Operation{
		&txnbuild.BeginSponsoringFutureReserves{
			SponsoredID: req.Source,
		},
		&txnbuild.Payment{
			Destination:   destination.Address(),
			Amount:        strconv.FormatInt(req.Amount, 10),
			Asset:         paymentAsset,
			SourceAccount: req.Source,
		},
		&txnbuild.EndSponsoringFutureReserves{
			SourceAccount: req.Source,
		},
	}

	// 4. Build transaction
	tx, err := txnbuild.NewTransaction(
		txnbuild.TransactionParams{
			SourceAccount:        sponsorAccount,
			IncrementSequenceNum: true,
			BaseFee:              txnbuild.MinBaseFee,
			Preconditions: txnbuild.Preconditions{
				TimeBounds: txnbuild.NewTimeout(300),
			},
			Operations: ops,
		},
	)
	if err != nil {
		log.Println("SponsoredPaymentTransaction: failed to build transaction: %w", err)
		return nil, types.ErrFailedToBuildTransaction
	}

	// 5. Sign transaction
	tx, err = tx.Sign(s.networkPassphrase, sponsorKP)
	if err != nil {
		log.Printf("SponsoredPaymentTransaction: failed to sign transaction with sponsor key: %v", err)
		return nil, types.ErrFailedToSignWithSponsorKey
	}

	// 6. Convert transaction to XDR format
	xdrTx, err := tx.Base64()
	if err != nil {
		log.Println("SponsoredPaymentTransaction: failed to convert transaction to XDR: %w", err)
		return nil, types.ErrFailedToConvertTransaction
	}

	// 7. Submit to Stellar network
	txResponse, err := s.rpcClient.SendTransaction(ctx, protocol.SendTransactionRequest{
		Transaction: xdrTx,
	})
	if err != nil {
		log.Printf("SponsoredPaymentTransaction: failed to submit transaction: %v", err)
		return nil, types.ErrFailedToSubmitTransaction
	}

	log.Printf("SponsoredPaymentTransaction: transaction submission status: %s, hash: %s", txResponse.Status, txResponse.Hash)

	switch txResponse.Status {
	case stellarcore.TXStatusError:
		if txResponse.ErrorResultXDR != "" {
			log.Printf("SponsoredPaymentTransaction: transaction rejected: %s", txResponse.ErrorResultXDR)
		} else {
			log.Printf("SponsoredPaymentTransaction: transaction rejected by stellar-core")
		}
		return nil, types.ErrTransactionRejected
	case stellarcore.TXStatusTryAgainLater:
		log.Printf("SponsoredPaymentTransaction: stellar-core is overloaded, try again later")
		return nil, types.ErrStellarCoreOverloaded
	case stellarcore.TXStatusDuplicate:
		log.Printf("SponsoredPaymentTransaction: duplicate transaction detected")
	case stellarcore.TXStatusPending:
		log.Printf("SponsoredPaymentTransaction: transaction pending")
	default:
		log.Printf("SponsoredPaymentTransaction: unknown submission status: %s", txResponse.Status)
	}

	txHash := txResponse.Hash
	s.logger.Info("transaction submitted, waiting for confirmation",
		slog.String("operation", "SponsoredPaymentTransaction"),
		slog.String("tx_hash", txHash),
	)

	pollCfg := rpc.DefaultPollConfig()
	pollCfg.Logger = s.logger
	txResult, err := rpc.PollTransaction(ctx, s.rpcClient, txHash, pollCfg)
	if err != nil {
		s.logger.Error("transaction failed",
			slog.String("operation", "SponsoredPaymentTransaction"),
			slog.String("tx_hash", txHash),
			slog.String("error", err.Error()),
		)
		return nil, types.ErrTransactionFailed
	}

	if txResult.Status != protocol.TransactionStatusSuccess {
		s.logger.Error("transaction not successful",
			slog.String("operation", "SponsoredPaymentTransaction"),
			slog.String("tx_hash", txHash),
			slog.String("status", txResult.Status),
		)
		return nil, types.ErrTransactionNotSuccessful
	}

	s.logger.Info("payment successful",
		slog.String("operation", "SponsoredPaymentTransaction"),
		slog.String("source", req.Source),
		slog.String("destination", req.Destination),
		slog.String("tx_hash", txHash),
		slog.Uint64("ledger", uint64(txResult.Ledger)),
	)

	return &types.SponsoredPaymentTransactionResponse{
		TxHash: txHash,
		Ledger: int64(txResult.Ledger),
		Status: txResult.Status,
	}, nil
}

// SendUSDC sends USDC directly from the treasury wallet to a destination Stellar address
// with a text memo.
func (s *service) SendUSDC(ctx context.Context, req types.SendUSDCRequest) (*types.SendUSDCResponse, error) {
	// 1. Validate request
	destination, err := keypair.ParseAddress(req.Destination)
	if err != nil {
		s.logger.Error("SendUSDC: invalid destination address", slog.String("destination", req.Destination))
		return nil, types.ErrInvalidStellarAddress
	}

	if req.Amount <= 0 {
		return nil, types.ErrInvalidTransactionAmount
	}

	// 2. Validate destination has USDC trustline
	destinationAccount, err := s.rpcClient.LoadAccount(ctx, destination.Address())
	if err != nil {
		s.logger.Error("SendUSDC: failed to load destination account", slog.String("destination", req.Destination))
		return nil, types.ErrFailedToLoadAccount
	}

	hasTrustline, err := hasAssetTrustline(ctx, s.rpcClient, destinationAccount.GetAccountID(), "USDC", s.usdcIssuer)
	if err != nil {
		return nil, types.ErrFailedToValidateTrustline
	}
	if !hasTrustline {
		return nil, types.ErrMissingTrustline
	}

	// 3. Build treasury payment transaction
	treasuryKP := keypair.MustParseFull(s.treasurySecretKey)

	treasuryAccount, err := s.rpcClient.LoadAccount(ctx, treasuryKP.Address())
	if err != nil {
		return nil, types.ErrFailedToLoadAccount
	}

	usdcAsset := txnbuild.CreditAsset{
		Code:   "USDC",
		Issuer: s.usdcIssuer,
	}

	// Convert stroops to string amount (divide by 10^7)
	amountStr := strconv.FormatFloat(float64(req.Amount)/10_000_000, 'f', 7, 64)

	ops := []txnbuild.Operation{
		&txnbuild.Payment{
			Destination: destination.Address(),
			Amount:      amountStr,
			Asset:       usdcAsset,
		},
	}

	txParams := txnbuild.TransactionParams{
		SourceAccount:        treasuryAccount,
		IncrementSequenceNum: true,
		BaseFee:              txnbuild.MinBaseFee,
		Preconditions: txnbuild.Preconditions{
			TimeBounds: txnbuild.NewTimeout(300),
		},
		Operations: ops,
	}

	// Add text memo if provided
	if req.Memo != "" {
		txParams.Memo = txnbuild.MemoText(req.Memo)
	}

	tx, err := txnbuild.NewTransaction(txParams)
	if err != nil {
		s.logger.Error("SendUSDC: failed to build transaction", slog.String("error", err.Error()))
		return nil, types.ErrFailedToBuildTransaction
	}

	// 4. Sign with treasury key
	tx, err = tx.Sign(s.networkPassphrase, treasuryKP)
	if err != nil {
		s.logger.Error("SendUSDC: failed to sign transaction", slog.String("error", err.Error()))
		return nil, types.ErrFailedToSignWithSponsorKey
	}

	// 5. Convert to XDR and submit
	xdrTx, err := tx.Base64()
	if err != nil {
		return nil, types.ErrFailedToConvertTransaction
	}

	txResponse, err := s.rpcClient.SendTransaction(ctx, protocol.SendTransactionRequest{
		Transaction: xdrTx,
	})
	if err != nil {
		s.logger.Error("SendUSDC: failed to submit transaction", slog.String("error", err.Error()))
		return nil, types.ErrFailedToSubmitTransaction
	}

	// 6. Handle submission status
	switch txResponse.Status {
	case stellarcore.TXStatusError:
		s.logger.Error("SendUSDC: transaction rejected",
			slog.String("error_xdr", txResponse.ErrorResultXDR),
		)
		return nil, types.ErrTransactionRejected
	case stellarcore.TXStatusTryAgainLater:
		return nil, types.ErrStellarCoreOverloaded
	}

	txHash := txResponse.Hash
	s.logger.Info("SendUSDC: transaction submitted, waiting for confirmation",
		slog.String("tx_hash", txHash),
		slog.String("destination", req.Destination),
		slog.String("memo", req.Memo),
		slog.Int64("amount_stroops", req.Amount),
	)

	// 7. Poll for confirmation
	pollCfg := rpc.DefaultPollConfig()
	pollCfg.Logger = s.logger
	txResult, err := rpc.PollTransaction(ctx, s.rpcClient, txHash, pollCfg)
	if err != nil {
		s.logger.Error("SendUSDC: transaction failed",
			slog.String("tx_hash", txHash),
			slog.String("error", err.Error()),
		)
		return nil, types.ErrTransactionFailed
	}

	if txResult.Status != protocol.TransactionStatusSuccess {
		s.logger.Error("SendUSDC: transaction not successful",
			slog.String("tx_hash", txHash),
			slog.String("status", txResult.Status),
		)
		return nil, types.ErrTransactionNotSuccessful
	}

	s.logger.Info("SendUSDC: payment successful",
		slog.String("tx_hash", txHash),
		slog.String("destination", req.Destination),
		slog.String("memo", req.Memo),
		slog.Int64("amount_stroops", req.Amount),
		slog.Uint64("ledger", uint64(txResult.Ledger)),
	)

	return &types.SendUSDCResponse{
		TxHash: txHash,
		Ledger: int64(txResult.Ledger),
		Status: txResult.Status,
	}, nil
}

// hasAssetTrustline checks if an account has an established trustline for the provided asset
func hasAssetTrustline(ctx context.Context, client RPCClient, accountID string, assetCode string, assetIssuer string) (bool, error) {
	asset := txnbuild.CreditAsset{
		Code:   assetCode,
		Issuer: assetIssuer,
	}

	trustlineAsset, err := asset.MustToTrustLineAsset().ToXDR()
	if err != nil {
		return false, fmt.Errorf("failed to convert asset to XDR: %w", err)
	}

	ledgerKey := xdr.LedgerKey{
		Type: xdr.LedgerEntryTypeTrustline,
		TrustLine: &xdr.LedgerKeyTrustLine{
			AccountId: xdr.MustAddress(accountID),
			Asset:     trustlineAsset,
		},
	}

	keyB64, err := xdr.MarshalBase64(ledgerKey)
	if err != nil {
		return false, fmt.Errorf("failed to marshal ledger key: %w", err)
	}

	resp, err := client.GetLedgerEntries(ctx, protocol.GetLedgerEntriesRequest{
		Keys: []string{keyB64},
	})
	if err != nil {
		return false, fmt.Errorf("failed to get ledger entries: %w", err)
	}

	if len(resp.Entries) == 0 {
		return false, nil
	}

	return true, nil
}
