// Package adapters provides core implementation for USSD integration with yellowcard off-ramp provider.
package adapters

import (
	"context"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/Shamba-Records-Limited/microvault/pkg/payment/offramp"
	"github.com/Shamba-Records-Limited/microvault/pkg/payment/yellowcard"
)

// ycNameRegexp strips everything except letters and spaces.
var ycNameRegexp = regexp.MustCompile(`[^a-zA-Z ]+`)

// YellowCardOffRampAdapter implements OffRampService using YellowCard with
// dual-mode settlement: direct (crypto-funded) and fiat (YC balance-funded).
type YellowCardOffRampAdapter struct {
	ycAdapter            *yellowcard.YellowcardAdapter
	treasury             offramp.TreasuryTransfer
	businessID           string
	businessName         string
	testDestinationPhone   string // gated test-only override; see config docs.
	testDestinationAddress string // gated test-only override; see config docs.
	logger                 *slog.Logger

	// Thread-safe caching for channels and networks
	cacheMu          sync.RWMutex
	cachedChannels   map[string][]yellowcard.Channel // keyed by country
	cachedNetworks   map[string][]yellowcard.Network // keyed by country
	channelsExpireAt map[string]time.Time            // keyed by country
	networksExpireAt map[string]time.Time            // keyed by country
	cacheTTL         time.Duration
}

// YellowCardOffRampConfig contains configuration for the YellowCard off-ramp adapter.
type YellowCardOffRampConfig struct {
	Adapter      *yellowcard.YellowcardAdapter
	Treasury     offramp.TreasuryTransfer // Required for direct settlement mode
	BusinessID   string
	BusinessName string
	Logger       *slog.Logger

	// TestDestinationPhoneOverride replaces every payment request's
	// destination phone with a fixed value. Used for testing against
	// YellowCard sandbox simulation numbers (e.g. +2341111111111 for a
	// guaranteed-success momo transaction).
	//
	// MUST be empty in production. The caller is responsible for the
	// environment gate — see microvault-credit/cmd/credit/main.go where
	// the env var is only read when SERVER_ENVIRONMENT != "production".
	// When non-empty, every disbursement logs a WARN before submission so
	// the override is impossible to overlook in logs.
	TestDestinationPhoneOverride string

	// TestDestinationAddressOverride, when set, makes direct mode run its
	// trustline check against this fixed address before any YC submission. Set
	// it to an address with no USDC trustline to force the direct-to-fiat
	// failover without creating an orphaned direct YC payment. Same production
	// gate as the phone override.
	TestDestinationAddressOverride string
}

// NewYellowCardOffRampAdapter creates a new YellowCard off-ramp adapter.
func NewYellowCardOffRampAdapter(cfg YellowCardOffRampConfig) *YellowCardOffRampAdapter {
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}
	scoped := logger.With("component", "yellowcard_offramp")
	if cfg.TestDestinationPhoneOverride != "" {
		scoped.Warn("YC test phone override active — every disbursement will go to a fixed number; DO NOT use in production",
			"override_phone", cfg.TestDestinationPhoneOverride)
	}
	if cfg.TestDestinationAddressOverride != "" {
		scoped.Warn("YC test destination address override active — direct settlement will send to a fixed Stellar address; DO NOT use in production",
			"override_address", cfg.TestDestinationAddressOverride)
	}
	return &YellowCardOffRampAdapter{
		ycAdapter:              cfg.Adapter,
		treasury:               cfg.Treasury,
		businessID:             cfg.BusinessID,
		businessName:           cfg.BusinessName,
		testDestinationPhone:   cfg.TestDestinationPhoneOverride,
		testDestinationAddress: cfg.TestDestinationAddressOverride,
		logger:                 scoped,
		cachedChannels:       make(map[string][]yellowcard.Channel),
		cachedNetworks:       make(map[string][]yellowcard.Network),
		channelsExpireAt:     make(map[string]time.Time),
		networksExpireAt:     make(map[string]time.Time),
		cacheTTL:             15 * time.Minute,
	}
}

var (
	_ offramp.Provider             = (*YellowCardOffRampAdapter)(nil)
	_ offramp.StatusReader         = (*YellowCardOffRampAdapter)(nil)
	_ offramp.Quoter               = (*YellowCardOffRampAdapter)(nil)
	_ offramp.Directory            = (*YellowCardOffRampAdapter)(nil)
	_ offramp.MobileMoneyDirectory = (*YellowCardOffRampAdapter)(nil)
	_ offramp.BalanceReporter      = (*YellowCardOffRampAdapter)(nil)
)

// ID identifies this provider in the registry.
func (a *YellowCardOffRampAdapter) ID() offramp.ProviderID { return offramp.ProviderYellowCard }

// Initiate is the dual-mode orchestrator for loan disbursement.
//
// Settlement flow:
//   - "direct" (default): Submit with directSettlement=true to send USDC to YC wallet to YC disburses fiat
//     Failover F1: If YC API call fails to fallback to fiat
//     Failover F2: If Stellar USDC transfer fails to fallback to fiat (USDC still in treasury)
//   - "fiat": Check YC balance to submit with forceAccept only to YC disburses from pre-funded balance
func (a *YellowCardOffRampAdapter) Initiate(ctx context.Context, req offramp.Request) (*offramp.Result, error) {
	opts, err := readYCOptions(req.Options)
	if err != nil {
		return nil, err
	}

	a.logger.Info("off-ramp initiated",
		"loan_id", req.LoanID,
		"user_id", req.UserID,
		"amount_usd", req.AmountUSD,
		"amount_stroops", req.AmountStroops,
		"destination_phone", req.DestinationPhone,
		"country", req.CountryCode,
		"network_code", req.NetworkCode,
		"settlement_method", opts.SettlementMethod,
	)

	if req.NetworkCode == "" {
		return nil, fmt.Errorf("network code is required for disbursement")
	}

	// Resolve channel and network upfront (shared by both modes).
	a.logger.Info("fetching channels", "loan_id", req.LoanID, "country", req.CountryCode)
	channels, err := a.getChannelsWithCache(ctx, req.CountryCode)
	if err != nil {
		a.logger.Error("failed to fetch channels", "loan_id", req.LoanID, "error", err)
		return nil, fmt.Errorf("failed to get channels: %w", err)
	}

	activeChannels := yellowcard.FilterActiveChannels(channels, yellowcard.ChannelTypeMomo)
	a.logger.Info("active momo channels found",
		"loan_id", req.LoanID,
		"total_channels", len(channels),
		"active_momo_channels", len(activeChannels),
	)

	// Filter for withdraw (disbursement) channels only.
	var withdrawChannels []yellowcard.Channel
	for _, ch := range activeChannels {
		if ch.RampType == yellowcard.RampTypeWithdraw {
			withdrawChannels = append(withdrawChannels, ch)
		}
	}
	if len(withdrawChannels) == 0 {
		a.logger.Error("no active withdraw momo channel",
			"loan_id", req.LoanID,
			"country", req.CountryCode,
			"active_momo_channels", len(activeChannels),
		)
		return nil, fmt.Errorf("no active mobile money withdraw channel found for %s", req.CountryCode)
	}

	momoChannel := withdrawChannels[0]
	a.logger.Info("channel resolved",
		"loan_id", req.LoanID,
		"channel_id", momoChannel.ID,
		"country", momoChannel.Country,
		"currency", momoChannel.Currency,
		"country_currency", momoChannel.CountryCurrency,
		"channel_type", momoChannel.ChannelType,
		"ramp_type", momoChannel.RampType,
		"settlement_type", momoChannel.SettlementType,
		"min_local", momoChannel.Min,
		"max_local", momoChannel.Max,
		"fee_usd", momoChannel.FeeUSD,
		"fee_local", momoChannel.FeeLocal,
	)

	// Note: Channel Min/Max are denominated in local currency (e.g. KES),
	// not USD. YellowCard's API enforces limits server-side on submission,
	// so we skip client-side comparison to avoid currency mismatch.

	a.logger.Info("validating network", "loan_id", req.LoanID, "network_code", req.NetworkCode, "country", req.CountryCode)
	networkID, networkName, err := a.validateNetwork(ctx, req.CountryCode, req.NetworkCode, req.NetworkName)
	if err != nil {
		a.logger.Error("network validation failed", "loan_id", req.LoanID, "error", err)
		return nil, err
	}
	a.logger.Info("network resolved", "loan_id", req.LoanID, "network_id", networkID, "network_name", networkName)

	idempotencyKey := req.IdempotencyKey
	if idempotencyKey == "" {
		idempotencyKey = fmt.Sprintf("%s_%s", req.LoanID, uuid.New().String()[:8])
	}

	method := opts.SettlementMethod
	if method == "" {
		method = yellowcard.SettlementMethodDirect
	}

	params := &disbursementParams{
		req:            req,
		momoChannel:    momoChannel,
		networkID:      networkID,
		networkName:    networkName,
		idempotencyKey: idempotencyKey,
	}

	// Direct settlement path with failover.
	if method == yellowcard.SettlementMethodDirect {
		if a.treasury == nil {
			return nil, fmt.Errorf("treasury transfer service is required for direct settlement")
		}

		a.logger.Info("attempting direct settlement", "loan_id", req.LoanID, "idempotency_key", idempotencyKey)
		result, err := a.tryDirectSettlement(ctx, params)
		if err != nil {
			// F1/F2: Direct settlement failed. USDC is still in treasury.
			// Fallback to fiat mode with a differentiated sequenceID.
			a.logger.Warn("direct settlement failed, falling back to fiat",
				"loan_id", req.LoanID,
				"error", err,
			)
			params.idempotencyKey = idempotencyKey + "_fiat"
			return a.tryFiatDisbursement(ctx, params)
		}
		a.logger.Info("direct settlement succeeded",
			"loan_id", req.LoanID,
			"request_id", result.RequestID,
		)
		return result, nil
	}

	// Fiat settlement path (explicit or fallback).
	a.logger.Info("attempting fiat settlement", "loan_id", req.LoanID, "idempotency_key", idempotencyKey)
	return a.tryFiatDisbursement(ctx, params)
}

// disbursementParams holds pre-resolved channel/network data shared by both settlement modes.
type disbursementParams struct {
	req            offramp.Request
	momoChannel    yellowcard.Channel
	networkID      string
	networkName    string
	idempotencyKey string
}

// tryDirectSettlement submits a payment with directSettlement=true and then
// sends USDC from treasury to the YC-issued Stellar wallet address (blocking).
//
// The request includes settlementInfo with cryptoCurrency, cryptoNetwork, and
// cryptoAmount. YC returns the same fields plus walletAddress, cryptoUSDRate,
// cryptoLocalRate, and expiresAt in the response.
func (a *YellowCardOffRampAdapter) tryDirectSettlement(ctx context.Context, p *disbursementParams) (*offramp.Result, error) {
	loanID := p.req.LoanID

	if a.testDestinationAddress != "" {
		a.logger.Warn("YC test destination address override active — checking its trustline before any direct submission to avoid an orphaned YC payment",
			"loan_id", loanID,
			"override_address", a.testDestinationAddress,
		)
		hasTrustline, err := a.treasury.CheckUSDCTrustline(ctx, a.testDestinationAddress)
		if err != nil {
			return nil, fmt.Errorf("test override trustline check failed: %w", err)
		}
		if !hasTrustline {
			return nil, fmt.Errorf("destination wallet is missing a USDC trustline")
		}
	}

	paymentReq := a.buildPaymentRequest(p)
	paymentReq.DirectSettlement = true
	// YellowCard requires amount/localAmount to be absent for direct settlement.
	// Setting to 0 causes omitempty to drop them from the JSON body.
	paymentReq.Amount = 0
	paymentReq.LocalAmount = 0
	paymentReq.SettlementInfo = &yellowcard.SettlementInfo{
		CryptoCurrency: yellowcard.CryptoCurrencyUSDC,
		CryptoNetwork:  yellowcard.CryptoNetworkXLM,
		CryptoAmount:   p.req.AmountUSD,
	}

	a.logger.Info("submitting direct settlement payment to YellowCard",
		"loan_id", loanID,
		"channel_id", paymentReq.ChannelID,
		"sequence_id", paymentReq.SequenceID,
		"amount_usd", p.req.AmountUSD,
		"crypto_currency", yellowcard.CryptoCurrencyUSDC,
		"crypto_network", yellowcard.CryptoNetworkXLM,
	)

	// F1 checkpoint: If YC API call fails, USDC is still in treasury to safe to failover.
	resp, err := a.ycAdapter.SubmitPayment(ctx, paymentReq)
	if err != nil {
		a.logger.Error("direct settlement API call failed (F1)",
			"loan_id", loanID,
			"error", err,
		)
		return nil, fmt.Errorf("direct settlement API call failed: %w", err)
	}

	a.logger.Info("YellowCard payment created",
		"loan_id", loanID,
		"yc_payment_id", resp.ID,
		"status", resp.Status,
		"rate", resp.Rate,
		"amount_usd", resp.Amount,
		"converted_amount", resp.ConvertedAmount,
		"currency", resp.Currency,
		"direct_settlement", resp.DirectSettlement,
		"service_fee_usd", resp.ServiceFeeAmountUSD,
		"network_fee_usd", resp.NetworkFeeAmountUSD,
	)

	// Parse the combined wallet address format: {stellar_address}_{memo}
	if resp.SettlementInfo == nil || resp.SettlementInfo.WalletAddress == "" {
		a.logger.Error("direct settlement response missing wallet address", "loan_id", loanID, "yc_payment_id", resp.ID)
		return nil, fmt.Errorf("direct settlement response missing wallet address")
	}

	a.logger.Info("parsing YellowCard settlement info",
		"loan_id", loanID,
		"wallet_address_raw", resp.SettlementInfo.WalletAddress,
		"crypto_amount", resp.SettlementInfo.CryptoAmount,
		"crypto_usd_rate", resp.SettlementInfo.CryptoUSDRate,
		"crypto_local_rate", resp.SettlementInfo.CryptoLocalRate,
		"expires_at", resp.SettlementInfo.ExpiresAt,
	)

	stellarAddr, stellarMemo, err := yellowcard.ParseStellarWalletAddress(resp.SettlementInfo.WalletAddress)
	if err != nil {
		a.logger.Error("failed to parse YC wallet address",
			"loan_id", loanID,
			"raw_address", resp.SettlementInfo.WalletAddress,
			"error", err,
		)
		return nil, fmt.Errorf("failed to parse YC wallet address: %w", err)
	}

	a.logger.Info("YellowCard wallet address parsed",
		"loan_id", loanID,
		"stellar_address", stellarAddr,
		"stellar_memo", stellarMemo,
	)

	// Proactive Trustline Verification (closes caveat B):
	// Check if YellowCard's destination wallet is missing a USDC trustline before attempting Stellar transfer.
	hasTrustline, err := a.treasury.CheckUSDCTrustline(ctx, stellarAddr)
	if err != nil {
		a.logger.Error("failed to verify trustline for YellowCard wallet",
			"loan_id", loanID,
			"destination", stellarAddr,
			"error", err,
		)
		return nil, fmt.Errorf("failed to verify trustline for YellowCard wallet: %w", err)
	}
	if !hasTrustline {
		a.logger.Warn("YellowCard destination wallet does not have a USDC trustline, aborting on-chain send",
			"loan_id", loanID,
			"destination", stellarAddr,
		)
		return nil, fmt.Errorf("destination wallet is missing a USDC trustline")
	}

	// Determine the USDC amount to send (in stroops).
	// Use the crypto amount from YC response if available, otherwise use request amount.
	amountStroops := p.req.AmountStroops
	if resp.SettlementInfo.CryptoAmount > 0 {
		amountStroops = int64(resp.SettlementInfo.CryptoAmount * 10_000_000)
	}
	if amountStroops <= 0 {
		a.logger.Error("cannot determine USDC amount to send",
			"loan_id", loanID,
			"request_stroops", p.req.AmountStroops,
			"yc_crypto_amount", resp.SettlementInfo.CryptoAmount,
		)
		return nil, fmt.Errorf("direct settlement: cannot determine USDC amount to send")
	}

	// F2 checkpoint: Send USDC from treasury to YellowCard's Stellar wallet.
	// If Stellar tx fails, USDC is still in treasury to safe to failover.
	a.logger.Info("sending USDC from treasury to YellowCard wallet",
		"loan_id", loanID,
		"destination", stellarAddr,
		"memo", stellarMemo,
		"amount_stroops", amountStroops,
		"amount_usdc", float64(amountStroops)/1e7,
	)

	txHash, err := a.treasury.SendUSDC(ctx, stellarAddr, stellarMemo, amountStroops)
	if err != nil {
		a.logger.Error("USDC transfer to YC wallet failed (F2)",
			"loan_id", loanID,
			"destination", stellarAddr,
			"memo", stellarMemo,
			"amount_stroops", amountStroops,
			"error", err,
		)
		return nil, fmt.Errorf("USDC transfer to YC wallet failed: %w", err)
	}

	a.logger.Info("USDC transfer to YellowCard wallet succeeded",
		"loan_id", loanID,
		"tx_hash", txHash,
		"destination", stellarAddr,
		"memo", stellarMemo,
		"amount_stroops", amountStroops,
		"amount_usdc", float64(amountStroops)/1e7,
		"yc_payment_id", resp.ID,
	)

	createdAt, _ := time.Parse(time.RFC3339, resp.CreatedAt)

	return &offramp.Result{
		RequestID:        resp.ID,
		SequenceID:       resp.SequenceID,
		Status:           resp.Status,
		AmountUSD:        float64(resp.Amount),
		AmountLocal:      float64(resp.ConvertedAmount),
		LocalCurrency:    resp.Currency,
		ExchangeRate:     resp.Rate,
		Fee:              resp.ServiceFeeAmountUSD + resp.NetworkFeeAmountUSD,
		FeeLocal:         resp.ServiceFeeAmountLocal + resp.NetworkFeeAmountLocal,
		EstimatedTime:    p.momoChannel.EstimatedSettlementTime,
		CreatedAt:        createdAt,
		SettlementMethod: yellowcard.SettlementMethodDirect,
		Provider: yellowcard.DirectSettlementPayload{
			StellarAddress: stellarAddr,
			StellarMemo:    stellarMemo,
			StellarTxHash:  txHash,
		},
	}, nil
}

// tryFiatDisbursement submits a fiat-mode payment (forceAccept only).
// Includes a balance guard: checks YC account balance before submitting.
func (a *YellowCardOffRampAdapter) tryFiatDisbursement(ctx context.Context, p *disbursementParams) (*offramp.Result, error) {
	loanID := p.req.LoanID

	// Balance guard: check YC has sufficient USD balance for fiat disbursement.
	a.logger.Info("checking YellowCard balance for fiat disbursement", "loan_id", loanID)
	availableBalance, err := a.ycAdapter.GetAvailableBalance(ctx)
	if err != nil {
		a.logger.Error("failed to check YC balance", "loan_id", loanID, "error", err)
		return nil, fmt.Errorf("failed to check available balance: %w", err)
	}

	a.logger.Info("YellowCard balance check",
		"loan_id", loanID,
		"available_usd", availableBalance,
		"requested_usd", p.req.AmountUSD,
	)

	if availableBalance < p.req.AmountUSD {
		a.logger.Warn("insufficient YC balance for fiat disbursement",
			"loan_id", loanID,
			"available", availableBalance,
			"requested", p.req.AmountUSD,
		)
		return nil, &yellowcard.InsufficientBalanceError{
			Available: availableBalance,
			Requested: p.req.AmountUSD,
		}
	}

	paymentReq := a.buildPaymentRequest(p)
	// Fiat mode: forceAccept is set in buildPaymentRequest, no directSettlement.

	a.logger.Info("submitting fiat disbursement to YellowCard",
		"loan_id", loanID,
		"channel_id", paymentReq.ChannelID,
		"sequence_id", paymentReq.SequenceID,
		"amount_usd", p.req.AmountUSD,
	)

	resp, err := a.ycAdapter.SubmitPayment(ctx, paymentReq)
	if err != nil {
		a.logger.Error("fiat disbursement failed", "loan_id", loanID, "error", err)
		return nil, fmt.Errorf("fiat disbursement failed: %w", err)
	}

	a.logger.Info("fiat disbursement submitted",
		"loan_id", loanID,
		"yc_payment_id", resp.ID,
		"status", resp.Status,
		"rate", resp.Rate,
		"amount_usd", resp.Amount,
		"converted_amount", resp.ConvertedAmount,
		"currency", resp.Currency,
		"service_fee_usd", resp.ServiceFeeAmountUSD,
		"network_fee_usd", resp.NetworkFeeAmountUSD,
	)

	createdAt, _ := time.Parse(time.RFC3339, resp.CreatedAt)

	return &offramp.Result{
		RequestID:        resp.ID,
		SequenceID:       resp.SequenceID,
		Status:           resp.Status,
		AmountUSD:        float64(resp.Amount),
		AmountLocal:      float64(resp.ConvertedAmount),
		LocalCurrency:    resp.Currency,
		ExchangeRate:     resp.Rate,
		Fee:              resp.ServiceFeeAmountUSD + resp.NetworkFeeAmountUSD,
		FeeLocal:         resp.ServiceFeeAmountLocal + resp.NetworkFeeAmountLocal,
		EstimatedTime:    p.momoChannel.EstimatedSettlementTime,
		CreatedAt:        createdAt,
		SettlementMethod: yellowcard.SettlementMethodFiat,
		Provider:         yellowcard.FiatPayload{},
	}, nil
}

// readYCOptions extracts the typed yellowcard.Options from a Request. A nil
// Options is acceptable (defaults to direct settlement). Wrong concrete type
// is rejected with a clear error.
func readYCOptions(opts offramp.ProviderOptions) (yellowcard.Options, error) {
	if opts == nil {
		return yellowcard.Options{}, nil
	}
	if v, ok := opts.(yellowcard.Options); ok {
		return v, nil
	}
	return yellowcard.Options{}, fmt.Errorf("yellowcard off-ramp: Request.Options must be yellowcard.Options, got %T", opts)
}

// buildPaymentRequest constructs the base YellowCard PaymentRequest from resolved params.
// Name fields are sanitized to letters and spaces only (YellowCard validation requirement).
// Phone numbers are normalized to international format with '+' prefix.
func (a *YellowCardOffRampAdapter) buildPaymentRequest(p *disbursementParams) yellowcard.PaymentRequest {
	recipientName := sanitizeYCName(p.req.RecipientName)
	if recipientName == "" {
		recipientName = "Customer"
	}

	phone := normalizePhone(p.req.DestinationPhone)

	// Test-only override (see TestDestinationPhoneOverride on the config).
	// Logged loud so it's visible in any production log greppinng.
	if a.testDestinationPhone != "" {
		a.logger.Warn("YC test phone override applied — using simulation number instead of user's",
			"loan_id", p.req.LoanID,
			"original_phone_redacted", redactPhoneForLog(phone),
			"override_phone", a.testDestinationPhone,
		)
		phone = normalizePhone(a.testDestinationPhone)
	}

	return yellowcard.PaymentRequest{
		ChannelID:    p.momoChannel.ID,
		SequenceID:   p.idempotencyKey,
		Amount:       p.req.AmountUSD,
		Reason:       "other",
		CustomerUID:  p.req.UserID,
		CustomerType: yellowcard.CustomerTypeInstitution,
		ForceAccept:  true,
		Sender: yellowcard.Sender{
			BusinessID:   a.businessID,
			BusinessName: sanitizeYCName(a.businessName),
		},
		Destination: yellowcard.Destination{
			AccountNumber: phone,
			AccountName:   recipientName,
			AccountType:   yellowcard.ChannelTypeMomo,
			NetworkID:     p.networkID,
			NetworkName:   p.networkName,
			Country:       p.req.CountryCode,
		},
	}
}

// sanitizeYCName strips non-letter, non-space characters from a name
// to satisfy YellowCard's validation: "must be only characters and spaces".
func sanitizeYCName(name string) string {
	return strings.TrimSpace(ycNameRegexp.ReplaceAllString(name, ""))
}

// redactPhoneForLog masks the middle digits of a phone number for log output.
// Matches the 6-prefix + 3-suffix pattern used elsewhere (see ussd/handler.go).
func redactPhoneForLog(phone string) string {
	digits := phone
	prefix := ""
	if strings.HasPrefix(phone, "+") {
		prefix = "+"
		digits = phone[1:]
	}
	if len(digits) <= 9 {
		return phone
	}
	return prefix + digits[:6] + strings.Repeat("X", len(digits)-9) + digits[len(digits)-3:]
}

// normalizePhone ensures the phone number is in international format with a '+' prefix.
// YellowCard requires "+254..." not "254...".
func normalizePhone(phone string) string {
	phone = strings.TrimSpace(phone)
	if phone == "" {
		return phone
	}
	if !strings.HasPrefix(phone, "+") {
		phone = "+" + phone
	}
	return phone
}

// Status retrieves the status of a disbursement from YellowCard. ref.ID is
// the YC payment ID; ref.Extra is unused.
func (a *YellowCardOffRampAdapter) Status(ctx context.Context, ref offramp.ProviderRef) (*offramp.Status, error) {
	details, err := a.ycAdapter.LookupPayment(ctx, ref.ID)
	if err != nil {
		return nil, err
	}

	var completedAt *time.Time
	if details.Status == yellowcard.StatusComplete {
		t, err := time.Parse(time.RFC3339, details.UpdatedAt)
		if err == nil {
			completedAt = &t
		}
	}

	return &offramp.Status{
		RequestID:     details.ID,
		SequenceID:    details.SequenceID,
		Status:        details.Status,
		AmountLocal:   float64(details.ConvertedAmount),
		LocalCurrency: details.Currency,
		CompletedAt:   completedAt,
		FailureReason: nil,
	}, nil
}

// SupportedProviders returns available MoMo channels for disbursement.
func (a *YellowCardOffRampAdapter) SupportedProviders(ctx context.Context, countryCode string) ([]offramp.ProviderInfo, error) {
	channels, err := a.getChannelsWithCache(ctx, countryCode)
	if err != nil {
		return nil, err
	}

	providers := make([]offramp.ProviderInfo, 0, len(channels))
	for _, ch := range channels {
		if ch.ChannelType != yellowcard.ChannelTypeMomo {
			continue
		}
		if ch.Status != "active" || ch.APIStatus != "active" {
			continue
		}
		if ch.RampType != yellowcard.RampTypeWithdraw {
			continue
		}

		providers = append(providers, offramp.ProviderInfo{
			ID:                      ch.ID,
			Name:                    fmt.Sprintf("%s Mobile Money", ch.Country),
			SupportedMethods:        []string{ch.ChannelType},
			MinAmount:               ch.Min,
			MaxAmount:               ch.Max,
			Currency:                ch.Currency,
			Status:                  ch.Status,
			FeeUSD:                  ch.FeeUSD,
			FeeLocal:                ch.FeeLocal,
			EstimatedSettlementTime: ch.EstimatedSettlementTime,
		})
	}

	return providers, nil
}

// Quote returns the current USD to local currency rate.
func (a *YellowCardOffRampAdapter) Quote(ctx context.Context, q offramp.QuoteRequest) (*offramp.ExchangeRate, error) {
	rates, err := a.ycAdapter.GetRates(ctx, q.Currency)
	if err != nil {
		return nil, err
	}

	if len(rates) == 0 {
		return nil, fmt.Errorf("no rates found for currency: %s", q.Currency)
	}

	rate := rates[0]

	updatedAt, err := time.Parse(time.RFC3339, rate.UpdatedAt)
	if err != nil {
		updatedAt = time.Now()
	}

	return &offramp.ExchangeRate{
		FromCurrency: yellowcard.CurrencyUSD,
		ToCurrency:   rate.Code,
		SellRate:     rate.Sell,
		BuyRate:      rate.Buy,
		RateID:       rate.RateID,
		Locale:       rate.Locale,
		UpdatedAt:    updatedAt,
	}, nil
}

// Networks returns available MoMo operators for a country.
func (a *YellowCardOffRampAdapter) Networks(ctx context.Context, countryCode string) ([]offramp.MobileMoneyNetwork, error) {
	channels, err := a.getChannelsWithCache(ctx, countryCode)
	if err != nil {
		return nil, err
	}

	momoChannels := yellowcard.FilterActiveChannels(channels, yellowcard.ChannelTypeMomo)
	if len(momoChannels) == 0 {
		return nil, fmt.Errorf("no active MoMo channel for %s", countryCode)
	}

	channelIDs := make(map[string]bool)
	for _, ch := range momoChannels {
		channelIDs[ch.ID] = true
	}

	networks, err := a.getNetworksWithCache(ctx, countryCode)
	if err != nil {
		return nil, err
	}

	result := make([]offramp.MobileMoneyNetwork, 0, len(networks))
	for _, n := range networks {
		if n.Status != "active" {
			continue
		}

		for _, cid := range n.ChannelIDs {
			if channelIDs[cid] {
				result = append(result, offramp.MobileMoneyNetwork{
					ID:     n.ID,
					Name:   n.Name,
					Code:   n.CodeString(),
					Status: n.Status,
				})
				break
			}
		}
	}

	return result, nil
}

// AvailableBalance returns the available USD balance for disbursements.
func (a *YellowCardOffRampAdapter) AvailableBalance(ctx context.Context) (float64, error) {
	return a.ycAdapter.GetAvailableBalance(ctx)
}

// validateNetwork resolves a network code or name to a YellowCard network ID
// and verifies the network is active in the given country.
//
// If no exact match is found by code or name, it falls back to the first active
// MoMo network linked to the disbursement channel. This handles cases where the
// stored network code (e.g. from Africa's Talking) doesn't map to a YellowCard
// network (e.g. "SANDBOX" vs "M PESA").
func (a *YellowCardOffRampAdapter) validateNetwork(ctx context.Context, countryCode, networkCode, networkName string) (networkID string, resolvedName string, err error) {
	networks, err := a.getNetworksWithCache(ctx, countryCode)
	if err != nil {
		return "", "", fmt.Errorf("failed to get networks: %w", err)
	}

	a.logger.Info("searching for network",
		"country", countryCode,
		"network_code", networkCode,
		"network_name", networkName,
		"total_networks", len(networks),
	)

	var matchedNetwork *yellowcard.Network
	for i := range networks {
		n := &networks[i]
		if strings.EqualFold(n.CodeString(), networkCode) ||
			strings.EqualFold(n.Name, networkCode) ||
			strings.EqualFold(n.Name, networkName) {
			matchedNetwork = n
			break
		}
	}

	// Fallback: pick the first active MoMo network (accountNumberType=phone).
	// This handles USSD simulator codes like "SANDBOX" that don't exist in YellowCard.
	if matchedNetwork == nil {
		a.logger.Warn("no exact network match, falling back to first active MoMo network",
			"country", countryCode,
			"network_code", networkCode,
			"network_name", networkName,
		)
		for i := range networks {
			n := &networks[i]
			if n.Status == "active" && n.AccountNumberType == "phone" {
				matchedNetwork = n
				a.logger.Info("fallback network selected",
					"network_id", n.ID,
					"network_name", n.Name,
					"network_code", n.CodeString(),
				)
				break
			}
		}
	}

	if matchedNetwork == nil {
		return "", "", &yellowcard.NetworkNotFoundError{
			NetworkCode: networkCode,
			NetworkName: networkName,
			Country:     countryCode,
		}
	}

	if matchedNetwork.Status != "active" {
		return "", "", &yellowcard.NetworkInactiveError{
			NetworkCode: networkCode,
			NetworkName: matchedNetwork.Name,
			Country:     countryCode,
			Status:      matchedNetwork.Status,
		}
	}

	return matchedNetwork.ID, matchedNetwork.Name, nil
}

// getChannelsWithCache retrieves channels with in-memory TTL caching.
func (a *YellowCardOffRampAdapter) getChannelsWithCache(ctx context.Context, country string) ([]yellowcard.Channel, error) {
	a.cacheMu.RLock()
	channels, ok := a.cachedChannels[country]
	expireAt, hasExpire := a.channelsExpireAt[country]
	a.cacheMu.RUnlock()

	if ok && hasExpire && time.Now().Before(expireAt) {
		a.logger.Debug("getChannelsWithCache: cache hit", "country", country)
		return channels, nil
	}

	a.cacheMu.Lock()
	defer a.cacheMu.Unlock()

	// Double-check under write lock
	channels, ok = a.cachedChannels[country]
	expireAt, hasExpire = a.channelsExpireAt[country]
	if ok && hasExpire && time.Now().Before(expireAt) {
		return channels, nil
	}

	a.logger.Info("getChannelsWithCache: cache miss, fetching from API", "country", country)
	fetched, err := a.ycAdapter.GetChannels(ctx, country)
	if err != nil {
		return nil, err
	}

	a.cachedChannels[country] = fetched
	a.channelsExpireAt[country] = time.Now().Add(a.cacheTTL)

	return fetched, nil
}

// getNetworksWithCache retrieves networks with in-memory TTL caching.
func (a *YellowCardOffRampAdapter) getNetworksWithCache(ctx context.Context, country string) ([]yellowcard.Network, error) {
	a.cacheMu.RLock()
	networks, ok := a.cachedNetworks[country]
	expireAt, hasExpire := a.networksExpireAt[country]
	a.cacheMu.RUnlock()

	if ok && hasExpire && time.Now().Before(expireAt) {
		a.logger.Debug("getNetworksWithCache: cache hit", "country", country)
		return networks, nil
	}

	a.cacheMu.Lock()
	defer a.cacheMu.Unlock()

	// Double-check under write lock
	networks, ok = a.cachedNetworks[country]
	expireAt, hasExpire = a.networksExpireAt[country]
	if ok && hasExpire && time.Now().Before(expireAt) {
		return networks, nil
	}

	a.logger.Info("getNetworksWithCache: cache miss, fetching from API", "country", country)
	fetched, err := a.ycAdapter.GetNetworks(ctx, country)
	if err != nil {
		return nil, err
	}

	a.cachedNetworks[country] = fetched
	a.networksExpireAt[country] = time.Now().Add(a.cacheTTL)

	return fetched, nil
}
