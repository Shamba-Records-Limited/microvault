// Package adapters provides core implementation for USSD integration with
// the MoneyGram cash-pickup off-ramp provider.
package adapters

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"time"

	"github.com/Shamba-Records-Limited/microvault/pkg/payment/moneygram"
	"github.com/Shamba-Records-Limited/microvault/pkg/payment/offramp"
)

// MoneyGramOffRampAdapter implements OffRampService for MoneyGram Ramps
// cash-pickup withdrawals.
//
// Unlike YellowCard's mobile-money flow, MoneyGram returns an interactive
// webview URL that the user must open to complete KYC and confirm the
// payout currency/amount. The URL is delivered to the user via SMS by the
// USSD layer; this adapter's job is to obtain it and surface it on the
// OffRampResult.
//
// Treasury USDC transfer happens later in the lifecycle, after the user
// completes the webview and MoneyGram transitions the transaction to
// pending_user_transfer_start. That step is driven by the background poller
// (see pkg/services/moneygram/poller.go in microvault-credit), not by this
// adapter — InitiateOffRamp only registers intent.
type MoneyGramOffRampAdapter struct {
	client          *moneygram.Client
	treasuryPubkey  string // for child memo derivation
	logger          *slog.Logger
}

// MoneyGramOffRampConfig contains configuration for the MoneyGram off-ramp adapter.
type MoneyGramOffRampConfig struct {
	Client *moneygram.Client
	Logger *slog.Logger
}

// NewMoneyGramOffRampAdapter creates a new MoneyGram off-ramp adapter. The
// client must already be initialised (TOML fetched and validated) by the
// caller — see microvault-credit/cmd/credit/main.go.
func NewMoneyGramOffRampAdapter(cfg MoneyGramOffRampConfig) (*MoneyGramOffRampAdapter, error) {
	if cfg.Client == nil {
		return nil, fmt.Errorf("moneygram off-ramp: client is required")
	}
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}
	return &MoneyGramOffRampAdapter{
		client:         cfg.Client,
		treasuryPubkey: cfg.Client.TreasuryAddress(),
		logger:         logger.With("component", "moneygram_offramp"),
	}, nil
}

var _ offramp.Service = (*MoneyGramOffRampAdapter)(nil)

// InitiateOffRamp creates a SEP-24 interactive withdrawal on MoneyGram and
// returns the webview URL plus the MG transaction ID. The caller (USSD layer)
// is responsible for delivering OffRampResult.InteractiveURL to the user via
// SMS so they can complete KYC.
//
// The returned OffRampResult populates:
//   - RequestID         — MG's transaction ID; persist as loans.ramp_request_id
//   - InteractiveURL    — webview URL; persist as loans.ramp_interactive_url, SMS to user
//   - ChildAccountMemo  — SEP-10 child memo; persist as loans.ramp_child_account_index
//                         input so the poller can re-derive it on restart
//   - SettlementMethod  — always "cash_pickup"
//
// ExternalReference and AmountLocal/LocalCurrency are populated later by the
// poller once MG transitions the transaction to pending_user_transfer_complete.
func (a *MoneyGramOffRampAdapter) InitiateOffRamp(ctx context.Context, req offramp.Request) (*offramp.Result, error) {
	a.logger.Info("moneygram off-ramp initiated",
		"loan_id", req.LoanID,
		"user_id", req.UserID,
		"amount_usd", req.AmountUSD,
		"country", req.CountryCode,
		"child_account_index", req.ChildAccountIndex,
	)

	if req.AmountUSD <= 0 {
		return nil, fmt.Errorf("moneygram off-ramp: amount must be positive (got %.2f)", req.AmountUSD)
	}
	if req.RecipientName == "" {
		return nil, fmt.Errorf("moneygram off-ramp: recipient name is required for SEP-9 prefill")
	}

	childMemo := moneygram.ChildAccountMemo(a.treasuryPubkey, req.ChildAccountIndex)

	first, last := moneygram.SplitFullName(req.RecipientName)
	customer := moneygram.Customer{
		FirstName:    first,
		LastName:     last,
		MobileNumber: req.DestinationPhone,
		BirthDate:    req.BirthDate,
	}
	if iso3 := moneygram.CountryISO3(req.CountryCode); iso3 != "" {
		customer.AddressCountryCode = iso3
	}

	withdrawReq := moneygram.WithdrawRequest{
		AssetCode: "USDC",
		Amount:    formatUSDAmount(req.AmountUSD),
		Lang:      "en",
		Customer:  customer,
	}

	resp, err := a.client.InitiateWithdrawal(ctx, childMemo, withdrawReq)
	if err != nil {
		a.logger.Error("moneygram withdraw failed",
			"loan_id", req.LoanID,
			"child_memo", childMemo,
			"error", err,
		)
		return nil, fmt.Errorf("moneygram withdraw: %w", err)
	}

	a.logger.Info("moneygram withdraw created",
		"loan_id", req.LoanID,
		"mg_transaction_id", resp.ID,
		"interactive_url", resp.URL,
	)

	idempotencyKey := req.IdempotencyKey
	if idempotencyKey == "" {
		idempotencyKey = req.LoanID
	}

	return &offramp.Result{
		RequestID:        resp.ID,
		SequenceID:       idempotencyKey,
		Status:           string(moneygram.StatusIncomplete),
		AmountUSD:        req.AmountUSD,
		LocalCurrency:    "", // unknown until pending_user_transfer_complete
		EstimatedTime:    0,  // unknown — depends on user opening the webview
		CreatedAt:        time.Now(),
		SettlementMethod: "cash_pickup",
		InteractiveURL:   resp.URL,
		ChildAccountMemo: childMemo,
	}, nil
}

// GetOffRampStatus retrieves the status of a MoneyGram withdrawal.
//
// Note: requestID is MG's transaction ID. Resolving it back to a child memo
// requires the caller to provide that index — but OffRampService's signature
// is fixed. Today we accept the limitation by relying on the JWT cache
// having the relevant memo already (the adapter is typically called by the
// poller right after InitiateOffRamp on the same process).
//
// For long-lived process restarts, the poller in microvault-credit must
// re-prime the cache by reading loans.ramp_child_account_index — outside the
// scope of this interface.
func (a *MoneyGramOffRampAdapter) GetOffRampStatus(ctx context.Context, requestID string) (*offramp.Status, error) {
	return nil, fmt.Errorf("moneygram: GetOffRampStatus by requestID alone is not supported — use the poller in microvault-credit which reads loans.ramp_child_account_index")
}

// GetSupportedProviders advertises a single cash-pickup option.
func (a *MoneyGramOffRampAdapter) GetSupportedProviders(ctx context.Context, countryCode string) ([]offramp.ProviderInfo, error) {
	return []offramp.ProviderInfo{{
		ID:               "moneygram_cash_pickup",
		Name:             "MoneyGram Cash Pickup",
		SupportedMethods: []string{"cash_pickup"},
		Currency:         "USD", // payout currency is chosen in MG's webview
		Status:           "active",
	}}, nil
}

// GetExchangeRate is delegated to the SDK's FX rate client when REST API
// credentials are available. Returns an error otherwise — callers should
// fall back to the YC rate adapter via the routing service.
func (a *MoneyGramOffRampAdapter) GetExchangeRate(ctx context.Context, currency string) (*offramp.ExchangeRate, error) {
	if a.client.FXRate == nil {
		return nil, errors.New("moneygram: FX rate API not configured (REST credentials missing)")
	}

	// Default originating country is USA — matches MoneyGram's primary corridor
	// and is what's documented for the FX Rate endpoint.
	got, err := a.client.FXRate.Get(ctx, moneygram.FXRateRequest{
		OriginatingCountry: "USA",
		SendCurrency:       "USD",
		DestinationCountry: "KEN", // hardcoded for now — extend when corridors broaden
		ServiceOption:      moneygram.ServiceOptionCashPickup,
	})
	if err != nil {
		return nil, fmt.Errorf("moneygram fx rate: %w", err)
	}

	return &offramp.ExchangeRate{
		FromCurrency: "USD",
		ToCurrency:   currency,
		Rate:         got.Rate,
		BuyRate:      got.Rate,
		UpdatedAt:    got.FetchedAt,
	}, nil
}

// GetMobileMoneyNetworks returns an empty list — MoneyGram is cash-pickup only.
func (a *MoneyGramOffRampAdapter) GetMobileMoneyNetworks(ctx context.Context, countryCode string) ([]offramp.MobileMoneyNetwork, error) {
	return []offramp.MobileMoneyNetwork{}, nil
}

// GetAvailableBalance returns 0 — MoneyGram off-ramp is funded per-transaction
// from treasury via SEP-24, not from a pre-funded provider balance.
func (a *MoneyGramOffRampAdapter) GetAvailableBalance(ctx context.Context) (float64, error) {
	return 0, nil
}

// formatUSDAmount renders a USD amount with two decimal places, the format
// MoneyGram's SEP-24 endpoint expects in the `amount` field.
func formatUSDAmount(usd float64) string {
	return strconv.FormatFloat(usd, 'f', 2, 64)
}
