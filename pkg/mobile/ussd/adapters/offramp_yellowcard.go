// Package adapters provides core implementation for USSD integration with yellowcard off-ramp provider.
package adapters

import (
	"context"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/Shamba-Records-Limited/microvault/pkg/payment/yellowcard"
)

// ycNameRegexp strips everything except letters and spaces.
var ycNameRegexp = regexp.MustCompile(`[^a-zA-Z ]+`)

// YellowCardOffRampAdapter implements OffRampService using YellowCard with
// dual-mode settlement: direct (crypto-funded) and fiat (YC balance-funded).
type YellowCardOffRampAdapter struct {
	ycAdapter    *yellowcard.YellowcardAdapter
	treasury     TreasuryTransfer
	businessID   string
	businessName string
	logger       *slog.Logger
}

// YellowCardOffRampConfig contains configuration for the YellowCard off-ramp adapter.
type YellowCardOffRampConfig struct {
	Adapter      *yellowcard.YellowcardAdapter
	Treasury     TreasuryTransfer // Required for direct settlement mode
	BusinessID   string
	BusinessName string
	Logger       *slog.Logger
}

// NewYellowCardOffRampAdapter creates a new YellowCard off-ramp adapter.
func NewYellowCardOffRampAdapter(cfg YellowCardOffRampConfig) *YellowCardOffRampAdapter {
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}
	return &YellowCardOffRampAdapter{
		ycAdapter:    cfg.Adapter,
		treasury:     cfg.Treasury,
		businessID:   cfg.BusinessID,
		businessName: cfg.BusinessName,
		logger:       logger.With("component", "yellowcard_offramp"),
	}
}

var _ OffRampService = (*YellowCardOffRampAdapter)(nil)

// InitiateOffRamp is the dual-mode orchestrator for loan disbursement.
//
// Settlement flow:
//   - "direct" (default): Submit with directSettlement=true → send USDC to YC wallet → YC disburses fiat
//     Failover F1: If YC API call fails → fallback to fiat
//     Failover F2: If Stellar USDC transfer fails → fallback to fiat (USDC still in treasury)
//   - "fiat": Check YC balance → submit with forceAccept only → YC disburses from pre-funded balance
func (a *YellowCardOffRampAdapter) InitiateOffRamp(ctx context.Context, req OffRampRequest) (*OffRampResult, error) {
	a.logger.Info("off-ramp initiated",
		"loan_id", req.LoanID,
		"user_id", req.UserID,
		"amount_usd", req.AmountUSD,
		"amount_stroops", req.AmountStroops,
		"destination_phone", req.DestinationPhone,
		"country", req.CountryCode,
		"network_code", req.NetworkCode,
		"settlement_method", req.SettlementMethod,
	)

	if req.NetworkCode == "" {
		return nil, fmt.Errorf("network code is required for disbursement")
	}

	// Resolve channel and network upfront (shared by both modes).
	a.logger.Info("fetching channels", "loan_id", req.LoanID, "country", req.CountryCode)
	channels, err := a.ycAdapter.GetChannels(ctx, req.CountryCode)
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

	method := req.SettlementMethod
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
			"stellar_tx_hash", result.StellarTxHash,
		)
		return result, nil
	}

	// Fiat settlement path (explicit or fallback).
	a.logger.Info("attempting fiat settlement", "loan_id", req.LoanID, "idempotency_key", idempotencyKey)
	return a.tryFiatDisbursement(ctx, params)
}

// disbursementParams holds pre-resolved channel/network data shared by both settlement modes.
type disbursementParams struct {
	req            OffRampRequest
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
func (a *YellowCardOffRampAdapter) tryDirectSettlement(ctx context.Context, p *disbursementParams) (*OffRampResult, error) {
	loanID := p.req.LoanID

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

	// F1 checkpoint: If YC API call fails, USDC is still in treasury → safe to failover.
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
	// If Stellar tx fails, USDC is still in treasury → safe to failover.
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

	return &OffRampResult{
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
		StellarAddress:   stellarAddr,
		StellarMemo:      stellarMemo,
		StellarTxHash:    txHash,
	}, nil
}

// tryFiatDisbursement submits a fiat-mode payment (forceAccept only).
// Includes a balance guard: checks YC account balance before submitting.
func (a *YellowCardOffRampAdapter) tryFiatDisbursement(ctx context.Context, p *disbursementParams) (*OffRampResult, error) {
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
		return nil, &InsufficientBalanceError{
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

	return &OffRampResult{
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
	}, nil
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

// GetOffRampStatus retrieves the status of a disbursement from YellowCard.
func (a *YellowCardOffRampAdapter) GetOffRampStatus(ctx context.Context, requestID string) (*OffRampStatus, error) {
	details, err := a.ycAdapter.LookupPayment(ctx, requestID)
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

	return &OffRampStatus{
		RequestID:     details.ID,
		SequenceID:    details.SequenceID,
		Status:        details.Status,
		AmountLocal:   float64(details.ConvertedAmount),
		LocalCurrency: details.Currency,
		CompletedAt:   completedAt,
		FailureReason: nil,
	}, nil
}

// GetSupportedProviders returns available MoMo channels for disbursement.
func (a *YellowCardOffRampAdapter) GetSupportedProviders(ctx context.Context, countryCode string) ([]OffRampProvider, error) {
	channels, err := a.ycAdapter.GetChannels(ctx, countryCode)
	if err != nil {
		return nil, err
	}

	providers := make([]OffRampProvider, 0, len(channels))
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

		providers = append(providers, OffRampProvider{
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

// GetExchangeRate returns the current USD to local currency rate.
func (a *YellowCardOffRampAdapter) GetExchangeRate(ctx context.Context, currency string) (*ExchangeRate, error) {
	rates, err := a.ycAdapter.GetRates(ctx, currency)
	if err != nil {
		return nil, err
	}

	if len(rates) == 0 {
		return nil, fmt.Errorf("no rates found for currency: %s", currency)
	}

	rate := rates[0]

	updatedAt, err := time.Parse(time.RFC3339, rate.UpdatedAt)
	if err != nil {
		updatedAt = time.Now()
	}

	return &ExchangeRate{
		FromCurrency: yellowcard.CurrencyUSD,
		ToCurrency:   rate.Code,
		Rate:         rate.Sell,
		BuyRate:      rate.Buy,
		RateID:       rate.RateID,
		Locale:       rate.Locale,
		UpdatedAt:    updatedAt,
	}, nil
}

// GetMobileMoneyNetworks returns available MoMo operators for a country.
func (a *YellowCardOffRampAdapter) GetMobileMoneyNetworks(ctx context.Context, countryCode string) ([]MobileMoneyNetwork, error) {
	channels, err := a.ycAdapter.GetChannels(ctx, countryCode)
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

	networks, err := a.ycAdapter.GetNetworks(ctx, countryCode)
	if err != nil {
		return nil, err
	}

	result := make([]MobileMoneyNetwork, 0, len(networks))
	for _, n := range networks {
		if n.Status != "active" {
			continue
		}

		for _, cid := range n.ChannelIDs {
			if channelIDs[cid] {
				result = append(result, MobileMoneyNetwork{
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

// GetAvailableBalance returns the available USD balance for disbursements.
func (a *YellowCardOffRampAdapter) GetAvailableBalance(ctx context.Context) (float64, error) {
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
	networks, err := a.ycAdapter.GetNetworks(ctx, countryCode)
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
		return "", "", &NetworkNotFoundError{
			NetworkCode: networkCode,
			NetworkName: networkName,
			Country:     countryCode,
		}
	}

	if matchedNetwork.Status != "active" {
		return "", "", &NetworkInactiveError{
			NetworkCode: networkCode,
			NetworkName: matchedNetwork.Name,
			Country:     countryCode,
			Status:      matchedNetwork.Status,
		}
	}

	return matchedNetwork.ID, matchedNetwork.Name, nil
}

// InsufficientBalanceError is returned when the YellowCard account balance
// is too low for a fiat disbursement.
type InsufficientBalanceError struct {
	Available float64
	Requested float64
}

// Error returns a human-readable description of the insufficient balance condition.
func (e *InsufficientBalanceError) Error() string {
	return fmt.Sprintf("insufficient YC balance: available %.2f USD, requested %.2f USD",
		e.Available, e.Requested)
}

// NetworkNotFoundError is returned when a network is not found in YellowCard.
type NetworkNotFoundError struct {
	NetworkCode string
	NetworkName string
	Country     string
}

// Error returns a human-readable description of the missing network.
func (e *NetworkNotFoundError) Error() string {
	return fmt.Sprintf("network '%s' (%s) not found in country %s", e.NetworkName, e.NetworkCode, e.Country)
}

// NetworkInactiveError is returned when a network is not currently active.
type NetworkInactiveError struct {
	NetworkCode string
	NetworkName string
	Country     string
	Status      string
}

// Error returns a human-readable description of the inactive network.
func (e *NetworkInactiveError) Error() string {
	return fmt.Sprintf("network '%s' (%s) is currently %s in country %s", e.NetworkName, e.NetworkCode, e.Status, e.Country)
}
