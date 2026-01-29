package adapters

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/Shamba-Records-Limited/microvault/internal/core/pkg/payment/yellowcard"
)

// YellowCardOffRampAdapter implements OffRampService using YellowCard.
type YellowCardOffRampAdapter struct {
	ycAdapter    *yellowcard.YellowcardAdapter
	businessID   string
	businessName string
}

// YellowCardOffRampConfig contains configuration for the YellowCard off-ramp adapter.
type YellowCardOffRampConfig struct {
	Adapter      *yellowcard.YellowcardAdapter
	BusinessID   string
	BusinessName string
}

// NewYellowCardOffRampAdapter creates a new YellowCard off-ramp adapter.
func NewYellowCardOffRampAdapter(cfg YellowCardOffRampConfig) *YellowCardOffRampAdapter {
	return &YellowCardOffRampAdapter{
		ycAdapter:    cfg.Adapter,
		businessID:   cfg.BusinessID,
		businessName: cfg.BusinessName,
	}
}

var _ OffRampService = (*YellowCardOffRampAdapter)(nil)

// InitiateOffRamp submits a Mobile Money disbursement to YellowCard.
func (a *YellowCardOffRampAdapter) InitiateOffRamp(ctx context.Context, req OffRampRequest) (*OffRampResult, error) {
	if req.NetworkCode == "" {
		return nil, fmt.Errorf("network code is required for disbursement")
	}

	availableBalance, err := a.ycAdapter.GetAvailableBalance(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to check available balance: %w", err)
	}

	if availableBalance < req.AmountUSD {
		return nil, fmt.Errorf("insufficient balance: available %.2f USD, requested %.2f USD",
			availableBalance, req.AmountUSD)
	}

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

	paymentReq := yellowcard.PaymentRequest{
		ChannelID:    momoChannel.ID,
		SequenceID:   idempotencyKey,
		Amount:       int(req.AmountUSD),
		Reason:       "loan_disbursement",
		CustomerUID:  req.UserID,
		CustomerType: yellowcard.CustomerTypeInstitution,
		ForceAccept:  true,
		Sender: yellowcard.Sender{
			BusinessID:   a.businessID,
			BusinessName: a.businessName,
		},
		Destination: yellowcard.Destination{
			AccountNumber: req.DestinationPhone,
			AccountName:   req.RecipientName,
			AccountType:   yellowcard.ChannelTypeMomo,
			NetworkID:     networkID,
			NetworkName:   networkName,
			Country:       req.CountryCode,
		},
	}

	resp, err := a.ycAdapter.SubmitPayment(ctx, paymentReq)
	if err != nil {
		return nil, fmt.Errorf("disbursement failed: %w", err)
	}

	createdAt, _ := time.Parse(time.RFC3339, resp.CreatedAt)

	return &OffRampResult{
		RequestID:     resp.ID,
		SequenceID:    resp.SequenceID,
		Status:        resp.Status,
		AmountUSD:     float64(resp.Amount),
		AmountLocal:   float64(resp.ConvertedAmount),
		LocalCurrency: resp.Currency,
		ExchangeRate:  resp.Rate,
		Fee:           0,
		EstimatedTime: momoChannel.EstimatedSettlementTime,
		CreatedAt:     createdAt,
	}, nil
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

// NetworkNotFoundError is returned when a network is not found in YellowCard.
type NetworkNotFoundError struct {
	NetworkCode string
	NetworkName string
	Country     string
}

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

func (e *NetworkInactiveError) Error() string {
	return fmt.Sprintf("network '%s' (%s) is currently %s in country %s", e.NetworkName, e.NetworkCode, e.Status, e.Country)
}
