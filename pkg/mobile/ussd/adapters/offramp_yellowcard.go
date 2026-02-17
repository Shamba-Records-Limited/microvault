package adapters

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/Shamba-Records-Limited/microvault/internal/core/pkg/payment/yellowcard"
)

// YellowCardOffRampAdapter implements OffRampService using YellowCard with
// dual-mode settlement: direct (crypto-funded) and fiat (YC balance-funded).
type YellowCardOffRampAdapter struct {
	ycAdapter    *yellowcard.YellowcardAdapter
	treasury     TreasuryTransfer
	businessID   string
	businessName string
}

// YellowCardOffRampConfig contains configuration for the YellowCard off-ramp adapter.
type YellowCardOffRampConfig struct {
	Adapter      *yellowcard.YellowcardAdapter
	Treasury     TreasuryTransfer // Required for direct settlement mode
	BusinessID   string
	BusinessName string
}

// NewYellowCardOffRampAdapter creates a new YellowCard off-ramp adapter.
func NewYellowCardOffRampAdapter(cfg YellowCardOffRampConfig) *YellowCardOffRampAdapter {
	return &YellowCardOffRampAdapter{
		ycAdapter:    cfg.Adapter,
		treasury:     cfg.Treasury,
		businessID:   cfg.BusinessID,
		businessName: cfg.BusinessName,
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
	if req.NetworkCode == "" {
		return nil, fmt.Errorf("network code is required for disbursement")
	}

	// Resolve channel and network upfront (shared by both modes).
	channels, err := a.ycAdapter.GetChannels(ctx, req.CountryCode)
	if err != nil {
		return nil, fmt.Errorf("failed to get channels: %w", err)
	}

	activeChannels := yellowcard.FilterActiveChannels(channels, yellowcard.ChannelTypeMomo)
	if len(activeChannels) == 0 {
		return nil, fmt.Errorf("no active mobile money channel found for %s", req.CountryCode)
	}

	momoChannel := activeChannels[0]

	if req.AmountUSD < momoChannel.Min || req.AmountUSD > momoChannel.Max {
		return nil, fmt.Errorf("amount %.2f USD is outside limits [%.2f, %.2f]",
			req.AmountUSD, momoChannel.Min, momoChannel.Max)
	}

	networkID, networkName, err := a.validateNetwork(ctx, req.CountryCode, req.NetworkCode, req.NetworkName)
	if err != nil {
		return nil, err
	}

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

		result, err := a.tryDirectSettlement(ctx, params)
		if err != nil {
			// F1/F2: Direct settlement failed. USDC is still in treasury.
			// Fallback to fiat mode with a differentiated sequenceID.
			log.Printf("yellowcard: direct settlement failed, falling back to fiat: %v", err)
			params.idempotencyKey = idempotencyKey + "_fiat"
			return a.tryFiatDisbursement(ctx, params)
		}
		return result, nil
	}

	// Fiat settlement path (explicit or fallback).
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
func (a *YellowCardOffRampAdapter) tryDirectSettlement(ctx context.Context, p *disbursementParams) (*OffRampResult, error) {
	paymentReq := a.buildPaymentRequest(p)
	paymentReq.DirectSettlement = true
	paymentReq.SettlementInfo = &yellowcard.SettlementInfo{
		CryptoCurrency: yellowcard.CryptoCurrencyUSDC,
		CryptoNetwork:  yellowcard.CryptoNetworkXLM,
	}

	// F1 checkpoint: If YC API call fails, USDC is still in treasury → safe to failover.
	resp, err := a.ycAdapter.SubmitPayment(ctx, paymentReq)
	if err != nil {
		return nil, fmt.Errorf("direct settlement API call failed: %w", err)
	}

	// Parse the combined wallet address format: {stellar_address}_{memo}
	if resp.SettlementInfo == nil || resp.SettlementInfo.WalletAddress == "" {
		return nil, fmt.Errorf("direct settlement response missing wallet address")
	}

	stellarAddr, stellarMemo, err := yellowcard.ParseStellarWalletAddress(resp.SettlementInfo.WalletAddress)
	if err != nil {
		return nil, fmt.Errorf("failed to parse YC wallet address: %w", err)
	}

	// Determine the USDC amount to send (in stroops).
	// Use the crypto amount from YC response if available, otherwise use request amount.
	amountStroops := p.req.AmountStroops
	if resp.SettlementInfo.CryptoAmount > 0 {
		amountStroops = int64(resp.SettlementInfo.CryptoAmount * 10_000_000)
	}
	if amountStroops <= 0 {
		return nil, fmt.Errorf("direct settlement: cannot determine USDC amount to send")
	}

	// F2 checkpoint: If Stellar tx fails, USDC is still in treasury → safe to failover.
	txHash, err := a.treasury.SendUSDC(ctx, stellarAddr, stellarMemo, amountStroops)
	if err != nil {
		return nil, fmt.Errorf("USDC transfer to YC wallet failed: %w", err)
	}

	log.Printf("yellowcard: direct settlement USDC sent tx=%s to=%s memo=%s amount=%d stroops",
		txHash, stellarAddr, stellarMemo, amountStroops)

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
	// Balance guard: check YC has sufficient USD balance for fiat disbursement.
	availableBalance, err := a.ycAdapter.GetAvailableBalance(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to check available balance: %w", err)
	}

	if availableBalance < p.req.AmountUSD {
		return nil, &InsufficientBalanceError{
			Available: availableBalance,
			Requested: p.req.AmountUSD,
		}
	}

	paymentReq := a.buildPaymentRequest(p)
	// Fiat mode: forceAccept is set in buildPaymentRequest, no directSettlement.

	resp, err := a.ycAdapter.SubmitPayment(ctx, paymentReq)
	if err != nil {
		return nil, fmt.Errorf("fiat disbursement failed: %w", err)
	}

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
		EstimatedTime:    p.momoChannel.EstimatedSettlementTime,
		CreatedAt:        createdAt,
		SettlementMethod: yellowcard.SettlementMethodFiat,
	}, nil
}

// buildPaymentRequest constructs the base YellowCard PaymentRequest from resolved params.
func (a *YellowCardOffRampAdapter) buildPaymentRequest(p *disbursementParams) yellowcard.PaymentRequest {
	return yellowcard.PaymentRequest{
		ChannelID:    p.momoChannel.ID,
		SequenceID:   p.idempotencyKey,
		Amount:       int(p.req.AmountUSD),
		Reason:       "loan_disbursement",
		CustomerUID:  p.req.UserID,
		CustomerType: yellowcard.CustomerTypeInstitution,
		ForceAccept:  true,
		Sender: yellowcard.Sender{
			BusinessID:   a.businessID,
			BusinessName: a.businessName,
		},
		Destination: yellowcard.Destination{
			AccountNumber: p.req.DestinationPhone,
			AccountName:   p.req.RecipientName,
			AccountType:   yellowcard.ChannelTypeMomo,
			NetworkID:     p.networkID,
			NetworkName:   p.networkName,
			Country:       p.req.CountryCode,
		},
	}
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
					Code:   n.Code,
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
func (a *YellowCardOffRampAdapter) validateNetwork(ctx context.Context, countryCode, networkCode, networkName string) (networkID string, resolvedName string, err error) {
	networks, err := a.ycAdapter.GetNetworks(ctx, countryCode)
	if err != nil {
		return "", "", fmt.Errorf("failed to get networks: %w", err)
	}

	var matchedNetwork *yellowcard.Network
	for i := range networks {
		n := &networks[i]
		if strings.EqualFold(n.Code, networkCode) ||
			strings.EqualFold(n.Name, networkCode) ||
			strings.EqualFold(n.Name, networkName) {
			matchedNetwork = n
			break
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
