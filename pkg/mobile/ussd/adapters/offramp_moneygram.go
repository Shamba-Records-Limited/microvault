package adapters

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/Shamba-Records-Limited/microvault/pkg/payment/moneygram"
	"github.com/Shamba-Records-Limited/microvault/pkg/payment/offramp"
	"github.com/Shamba-Records-Limited/microvault/pkg/payment/stellaranchor"
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
// pending_user_transfer_start. That step is driven by the background poller in
// the lending module, not by this adapter — InitiateOffRamp only registers
// intent.
type MoneyGramOffRampAdapter struct {
	client         *moneygram.Client
	treasuryPubkey string // auth wallet — for child memo derivation
	fundsPubkey    string // funds wallet — SEP-24 account / withdrawal source
	logger         *slog.Logger
}

// MoneyGramOffRampConfig contains configuration for the MoneyGram off-ramp adapter.
type MoneyGramOffRampConfig struct {
	Client      *moneygram.Client
	FundsPubkey string
	Logger      *slog.Logger
}

// NewMoneyGramOffRampAdapter creates a new MoneyGram off-ramp adapter. The
// client must already be initialised (TOML fetched and validated) by the
// calling binary at startup.
func NewMoneyGramOffRampAdapter(cfg MoneyGramOffRampConfig) (*MoneyGramOffRampAdapter, error) {
	if cfg.Client == nil {
		return nil, fmt.Errorf("moneygram off-ramp: client is required")
	}
	if cfg.FundsPubkey == "" {
		return nil, fmt.Errorf("moneygram off-ramp: funds pubkey is required")
	}
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}
	return &MoneyGramOffRampAdapter{
		client:         cfg.Client,
		treasuryPubkey: cfg.Client.TreasuryAddress(),
		fundsPubkey:    cfg.FundsPubkey,
		logger:         logger.With("component", "moneygram_offramp"),
	}, nil
}

var (
	_ offramp.Provider     = (*MoneyGramOffRampAdapter)(nil)
	_ offramp.StatusReader = (*MoneyGramOffRampAdapter)(nil)
	_ offramp.Quoter       = (*MoneyGramOffRampAdapter)(nil)
	_ offramp.Directory    = (*MoneyGramOffRampAdapter)(nil)
)

// MoneyGramRefChildMemoKey is the ProviderRef.Extra key the MG adapter
// expects for Status lookups — the SEP-10 child memo that authenticates
// the cached JWT. Callers reconstruct it from loans.ramp_child_account_index
// via stellaranchor.ChildAccountMemo.
const MoneyGramRefChildMemoKey = "child_memo"

// ID identifies this provider in the registry.
func (a *MoneyGramOffRampAdapter) ID() offramp.ProviderID { return offramp.ProviderMoneyGram }

// Initiate creates a SEP-24 interactive withdrawal on MoneyGram and
// returns the webview URL plus the MG transaction ID. The caller (USSD layer)
// is responsible for delivering OffRampResult.InteractiveURL to the user via
// SMS so they can complete KYC.
//
// The returned OffRampResult populates:
//   - RequestID         — MG's transaction ID; persist as loans.ramp_request_id
//   - InteractiveURL    — webview URL; persist as loans.ramp_interactive_url, SMS to user
//   - ChildAccountMemo  — SEP-10 child memo; persist as loans.ramp_child_account_index
//     input so the poller can re-derive it on restart
//   - SettlementMethod  — always "cash_pickup"
//
// ExternalReference and AmountLocal/LocalCurrency are populated later by the
// poller once MG transitions the transaction to pending_user_transfer_complete.
func (a *MoneyGramOffRampAdapter) Initiate(ctx context.Context, req offramp.Request) (*offramp.Result, error) {
	opts, err := readMGOptions(req.Options)
	if err != nil {
		return nil, err
	}

	a.logger.Info("moneygram off-ramp initiated",
		"loan_id", req.LoanID,
		"user_id", req.UserID,
		"amount_usd", req.AmountUSD,
		"country", req.CountryCode,
		"child_account_index", opts.ChildAccountIndex,
	)

	if req.AmountUSD <= 0 {
		return nil, fmt.Errorf("moneygram off-ramp: amount must be positive (got %.2f)", req.AmountUSD)
	}
	if req.RecipientName == "" {
		return nil, fmt.Errorf("moneygram off-ramp: recipient name is required for SEP-9 prefill")
	}

	childMemo := stellaranchor.ChildAccountMemo(a.treasuryPubkey, opts.ChildAccountIndex)

	first, last := stellaranchor.SplitFullName(req.RecipientName)
	customer := stellaranchor.Customer{
		FirstName:       first,
		LastName:        last,
		MobileNumber:    req.DestinationPhone,
		BirthDate:       opts.BirthDate,
		Address:         opts.Address,
		PostalCode:      opts.PostalCode,
		StateOrProvince: opts.StateOrProvince,
		City:            opts.City,
	}
	if iso3 := stellaranchor.CountryISO3(req.CountryCode); iso3 != "" {
		customer.AddressCountryCode = iso3
	}

	withdrawReq := stellaranchor.WithdrawRequest{
		AssetCode: "USDC",
		Amount:    formatUSDAmount(req.AmountUSD),
		Lang:      "en",
		Account:   a.fundsPubkey,
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
		Status:           string(stellaranchor.StatusIncomplete),
		AmountUSD:        req.AmountUSD,
		LocalCurrency:    "", // unknown until pending_user_transfer_complete
		EstimatedTime:    0,  // unknown — depends on user opening the webview
		CreatedAt:        time.Now(),
		SettlementMethod: "cash_pickup",
		Provider: moneygram.CashPickupPayload{
			InteractiveURL:   resp.URL,
			ChildAccountMemo: childMemo,
		},
	}, nil
}

// readMGOptions extracts the typed moneygram.Options from a Request.
// MoneyGram requires Options — nil or wrong concrete type is rejected.
func readMGOptions(opts offramp.ProviderOptions) (moneygram.Options, error) {
	if opts == nil {
		return moneygram.Options{}, fmt.Errorf("moneygram off-ramp: Request.Options is required (moneygram.Options)")
	}
	if v, ok := opts.(moneygram.Options); ok {
		return v, nil
	}
	return moneygram.Options{}, fmt.Errorf("moneygram off-ramp: Request.Options must be moneygram.Options, got %T", opts)
}

// Status retrieves the status of a MoneyGram withdrawal. ref.ID is MG's
// transaction ID; ref.Extra[MoneyGramRefChildMemoKey] must carry the int64
// SEP-10 child memo so the SDK can resolve the JWT for this user.
func (a *MoneyGramOffRampAdapter) Status(ctx context.Context, ref offramp.ProviderRef) (*offramp.Status, error) {
	childMemo, err := extractChildMemo(ref)
	if err != nil {
		return nil, err
	}

	tx, err := a.client.GetTransaction(ctx, childMemo, ref.ID)
	if err != nil {
		return nil, fmt.Errorf("moneygram: get transaction: %w", err)
	}

	out := &offramp.Status{
		RequestID:     tx.ID,
		Status:        string(tx.Status),
		LocalCurrency: strings.TrimPrefix(tx.AmountOutAsset, "iso4217:"),
	}
	if tx.AmountOut != "" {
		if v, perr := strconv.ParseFloat(tx.AmountOut, 64); perr == nil {
			out.AmountLocal = v
		}
	}
	if tx.CompletedAt != "" {
		if t, perr := time.Parse(time.RFC3339, tx.CompletedAt); perr == nil {
			out.CompletedAt = &t
		}
	}
	if tx.Message != "" {
		msg := tx.Message
		out.FailureReason = &msg
	}
	return out, nil
}

// extractChildMemo pulls the SEP-10 child memo out of ref.Extra, accepting
// int64 (preferred) or uint32 for ergonomics with the persisted column type.
func extractChildMemo(ref offramp.ProviderRef) (int64, error) {
	v, ok := ref.Extra[MoneyGramRefChildMemoKey]
	if !ok {
		return 0, fmt.Errorf("moneygram: ProviderRef.Extra[%q] is required", MoneyGramRefChildMemoKey)
	}
	switch x := v.(type) {
	case int64:
		return x, nil
	case int:
		return int64(x), nil
	case uint32:
		return int64(x), nil
	default:
		return 0, fmt.Errorf("moneygram: ProviderRef.Extra[%q] must be int64, got %T", MoneyGramRefChildMemoKey, v)
	}
}

// SupportedProviders advertises a single cash-pickup option.
func (a *MoneyGramOffRampAdapter) SupportedProviders(ctx context.Context, countryCode string) ([]offramp.ProviderInfo, error) {
	return []offramp.ProviderInfo{{
		ID:               "moneygram_cash_pickup",
		Name:             "MoneyGram Cash Pickup",
		SupportedMethods: []string{"cash_pickup"},
		Currency:         "USD", // payout currency is chosen in MG's webview
		Status:           "active",
	}}, nil
}

// Quote delegates to the SDK's FX rate client when REST API credentials are
// available. Returns an error otherwise — callers should fall back to a
// different Quoter (e.g. the YC adapter).
func (a *MoneyGramOffRampAdapter) Quote(ctx context.Context, q offramp.QuoteRequest) (*offramp.ExchangeRate, error) {
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
		ToCurrency:   q.Currency,
		SellRate:     got.Rate,
		BuyRate:      got.Rate,
		UpdatedAt:    got.FetchedAt,
	}, nil
}

// formatUSDAmount renders a USD amount with two decimal places, the format
// MoneyGram's SEP-24 endpoint expects in the `amount` field.
func formatUSDAmount(usd float64) string {
	return strconv.FormatFloat(usd, 'f', 2, 64)
}
