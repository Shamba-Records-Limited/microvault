package adapters

import (
	"context"
	"log/slog"
	"maps"
	"strings"
	"sync"
	"time"

	"github.com/govalues/decimal"
	"github.com/samber/lo"
	"github.com/samber/oops"
	"github.com/stellar/go-stellar-sdk/strkey"

	"github.com/Shamba-Records-Limited/microvault/pkg/alerts"
	pkgErrors "github.com/Shamba-Records-Limited/microvault/pkg/errors"
	"github.com/Shamba-Records-Limited/microvault/pkg/logging"
	"github.com/Shamba-Records-Limited/microvault/pkg/payment/chipper"
	"github.com/Shamba-Records-Limited/microvault/pkg/payment/offramp"
	phoneutil "github.com/Shamba-Records-Limited/microvault/pkg/phone"
)

const (
	chipperAsset           = "USDC"
	chipperStatusPending   = "payout_pending"
	chipperCapsTTL         = 15 * time.Minute
	chipperDepositLookback = 5 * time.Minute
	chipperNarration       = "Loan disbursement"
	defaultCreditTimeout   = 90 * time.Second
	defaultPollInterval    = 3 * time.Second
	defaultFeeHeadroomPct  = 2
)

// ChipperClient is the subset of chipper.Client the adapter uses.
type ChipperClient interface {
	CreatePayout(ctx context.Context, req chipper.PayoutRequest) (*chipper.Payout, bool, error)
	GetPayout(ctx context.Context, id string) (*chipper.Payout, error)
	GetPayoutByReference(ctx context.Context, reference string) (*chipper.Payout, error)
	GetRate(ctx context.Context, origin, destination string) (*chipper.Rate, error)
	GetCountryCapabilities(ctx context.Context, country string) (*chipper.Capabilities, error)
	ListAccounts(ctx context.Context) ([]chipper.Account, error)
	CreateCryptoAddress(ctx context.Context, req chipper.CryptoAddressRequest) (*chipper.CryptoAddress, error)
	FindCryptoDeposit(ctx context.Context, addressID, txHash string, since time.Time) (*chipper.Collection, bool, error)
}

func chipperAdapterErr(op string) oops.OopsErrorBuilder {
	return oops.In(pkgErrors.DomainOffRamp).
		Tags(chipper.ProviderName).
		With(pkgErrors.AttrProvider, chipper.ProviderName).
		With(pkgErrors.AttrOperation, op)
}

// ChipperOffRampConfig configures the Chipper off-ramp adapter.
//
// DepositAddress and DepositMemo are the dashboard values, pinned so a
// changed or wrong API response cannot redirect treasury funds. MethodCodes
// maps a mobile network code (MCC+MNC, e.g. 63902) to a Chipper payout
// method code (e.g. ke_mpesa). FeeHeadroomPct is the margin over the payout
// amount the Chipper balance must hold before a fiat payout.
type ChipperOffRampConfig struct {
	Client         ChipperClient
	Treasury       offramp.TreasuryTransfer
	DepositAddress string
	DepositMemo    string
	MethodCodes    map[string]string
	FeeHeadroomPct int64
	CreditTimeout  time.Duration
	PollInterval   time.Duration
	Alerts         alerts.Service
	Logger         *slog.Logger

	// TestDestinationPhoneOverride replaces every destination phone. The
	// calling binary must leave it empty in production.
	TestDestinationPhoneOverride string
}

// ChipperOffRampAdapter disburses through Chipper: the treasury deposits USDC
// to our Chipper address, then a payout from the Chipper USDC balance pays
// the borrower. Chipper's fee is debited from that balance.
type ChipperOffRampAdapter struct {
	client        ChipperClient
	treasury      offramp.TreasuryTransfer
	address       string
	memo          offramp.Memo
	methodCodes   map[string]string
	headroom      decimal.Decimal
	creditTimeout time.Duration
	pollInterval  time.Duration
	testPhone     string
	alerts        alerts.Service
	logger        *slog.Logger
	now           func() time.Time

	targetMu  sync.Mutex
	addressID string

	capsMu      sync.Mutex
	caps        map[string]*chipper.Capabilities
	capsExpires map[string]time.Time
}

var (
	_ offramp.Provider             = (*ChipperOffRampAdapter)(nil)
	_ offramp.StatusReader         = (*ChipperOffRampAdapter)(nil)
	_ offramp.Quoter               = (*ChipperOffRampAdapter)(nil)
	_ offramp.Directory            = (*ChipperOffRampAdapter)(nil)
	_ offramp.MobileMoneyDirectory = (*ChipperOffRampAdapter)(nil)
	_ offramp.BalanceReporter      = (*ChipperOffRampAdapter)(nil)
)

// NewChipperOffRampAdapter validates the config. It makes no network call;
// VerifyDepositTarget checks the pinned address against the API.
func NewChipperOffRampAdapter(cfg ChipperOffRampConfig) (*ChipperOffRampAdapter, error) {
	errb := chipperAdapterErr("new")

	switch {
	case cfg.Client == nil:
		return nil, errb.Code(pkgErrors.CodeMissingDependency).Errorf("chipper client is nil")
	case cfg.Treasury == nil:
		return nil, errb.Code(pkgErrors.CodeMissingDependency).Errorf("treasury transfer is nil")
	case !strkey.IsValidEd25519PublicKey(cfg.DepositAddress):
		return nil, errb.Code(pkgErrors.CodeInvalidAddress).Errorf("deposit address is not a stellar account")
	case len(cfg.MethodCodes) == 0:
		return nil, errb.Code(pkgErrors.CodeMissingDependency).Errorf("no payout method codes configured")
	case cfg.FeeHeadroomPct < 0:
		return nil, errb.Code(pkgErrors.CodeInvalidConfig).Errorf("fee headroom cannot be negative")
	}
	memo := offramp.Memo{Value: cfg.DepositMemo, Type: offramp.MemoTypeID}
	if cfg.DepositMemo == "" {
		return nil, errb.Code(pkgErrors.CodeInvalidMemo).Errorf("deposit memo is required")
	}
	if err := memo.Validate(); err != nil {
		return nil, errb.Code(pkgErrors.CodeInvalidMemo).Wrapf(err, "deposit memo is not a memo id")
	}

	headroomPct := lo.Ternary(cfg.FeeHeadroomPct == 0, defaultFeeHeadroomPct, cfg.FeeHeadroomPct)
	headroom, err := decimal.New(100+headroomPct, 2)
	if err != nil {
		return nil, errb.Code(pkgErrors.CodeInvalidConfig).Wrapf(err, "fee headroom is out of range")
	}

	logger := lo.CoalesceOrEmpty(cfg.Logger, slog.Default()).With("component", "chipper_offramp")
	if cfg.TestDestinationPhoneOverride != "" {
		logger.Warn("chipper test phone override active; every payout goes to a fixed number",
			"override_phone", phoneutil.Redact(cfg.TestDestinationPhoneOverride))
	}

	return &ChipperOffRampAdapter{
		client:        cfg.Client,
		treasury:      cfg.Treasury,
		address:       cfg.DepositAddress,
		memo:          memo,
		methodCodes:   maps.Clone(cfg.MethodCodes),
		headroom:      headroom,
		creditTimeout: lo.CoalesceOrEmpty(cfg.CreditTimeout, defaultCreditTimeout),
		pollInterval:  lo.CoalesceOrEmpty(cfg.PollInterval, defaultPollInterval),
		testPhone:     cfg.TestDestinationPhoneOverride,
		alerts:        cfg.Alerts,
		logger:        logger,
		now:           time.Now,
		caps:          make(map[string]*chipper.Capabilities),
		capsExpires:   make(map[string]time.Time),
	}, nil
}

// ID identifies this provider in the registry.
func (a *ChipperOffRampAdapter) ID() offramp.ProviderID { return offramp.ProviderChipper }

// VerifyDepositTarget checks that the API's Stellar USDC deposit address and
// memo match the pinned values. Call it at boot; Initiate also calls it until
// it succeeds.
func (a *ChipperOffRampAdapter) VerifyDepositTarget(ctx context.Context) error {
	_, err := a.depositAddressID(ctx)
	return err
}

func (a *ChipperOffRampAdapter) depositAddressID(ctx context.Context) (string, error) {
	a.targetMu.Lock()
	cached := a.addressID
	a.targetMu.Unlock()
	if cached != "" {
		return cached, nil
	}

	errb := chipperAdapterErr("verify_deposit_target")
	addr, err := a.client.CreateCryptoAddress(ctx, chipper.CryptoAddressRequest{Currency: chipperAsset, Chain: chipper.ChainStellar})
	if err != nil {
		return "", errb.Code(pkgErrors.CodeHTTPError).Wrapf(err, "could not read the chipper deposit address")
	}
	gotAddress, gotTag := lo.FromPtr(addr.Address), lo.FromPtr(addr.Tag)
	if gotAddress != a.address || gotTag != a.memo.Value {
		return "", errb.Code(pkgErrors.CodeInvalidAddress).
			With("pinned_address", a.address).With("api_address", gotAddress).
			With("pinned_memo", a.memo.Value).With("api_memo", gotTag).
			Errorf("chipper deposit address does not match the pinned value")
	}
	a.targetMu.Lock()
	a.addressID = addr.ID
	a.targetMu.Unlock()
	return addr.ID, nil
}

// disbursement is the validated input shared by both settlement paths.
type disbursement struct {
	req       offramp.Request
	reference string
	method    chipper.PaymentMethod
	currency  string
	phone     string
	amount    decimal.Decimal
}

// Initiate disburses a loan. In direct mode the treasury deposits the USDC
// and Chipper pays out from the balance; in fiat mode the balance pays and
// the USDC stays in treasury. Once the treasury send succeeds Initiate never
// returns an error: the USDC is no longer in treasury, so the payout is
// reported pending and finished by ReleasePayout.
func (a *ChipperOffRampAdapter) Initiate(ctx context.Context, req offramp.Request) (*offramp.Result, error) {
	opts, err := readChipperOptions(req.Options)
	if err != nil {
		return nil, err
	}
	d, err := a.prepare(ctx, req)
	if err != nil {
		return nil, err
	}
	ctx = logging.WithLoan(ctx, req.LoanID, "")

	if existing, err := a.client.GetPayoutByReference(ctx, d.reference); err == nil {
		a.logger.WarnContext(ctx, "chipper payout already exists for this reference; returning it",
			pkgErrors.AttrLoanID, req.LoanID, "payout_id", existing.ID)
		return a.payoutResult(d, existing, chipper.SettlementDirect, "", ""), nil
	} else if !isChipperNotFound(err) {
		return nil, chipperAdapterErr("initiate").With(pkgErrors.AttrLoanID, req.LoanID).
			Code(pkgErrors.CodeHTTPError).Wrapf(err, "could not check for an existing payout")
	}

	if opts.SettlementMethod == chipper.SettlementFiat {
		return a.payFromBalance(ctx, d)
	}
	return a.payDirect(ctx, d)
}

func (a *ChipperOffRampAdapter) prepare(ctx context.Context, req offramp.Request) (*disbursement, error) {
	errb := chipperAdapterErr("prepare").With(pkgErrors.AttrLoanID, req.LoanID)

	if req.AmountStroops <= 0 {
		return nil, errb.With(pkgErrors.AttrAmountStroops, req.AmountStroops).
			Code(pkgErrors.CodeInvalidAmount).Errorf("amount must be positive")
	}
	currency := offramp.LocalCurrency(req.CountryCode)
	if currency == "" {
		return nil, errb.With("country", req.CountryCode).
			Code(pkgErrors.CodeCorridorUnavailable).Errorf("no local currency for this country")
	}
	phone := strings.TrimPrefix(phoneutil.E164(lo.CoalesceOrEmpty(a.testPhone, req.DestinationPhone)), "+")
	if phone == "" {
		return nil, errb.Code(pkgErrors.CodeMissingPhoneNumber).Errorf("destination phone is not in international format")
	}
	if a.testPhone != "" {
		a.logger.WarnContext(ctx, "chipper test phone override applied",
			pkgErrors.AttrLoanID, req.LoanID, "original_phone", phoneutil.Redact(req.DestinationPhone))
	}

	method, err := a.payoutMethod(ctx, req.CountryCode, req.NetworkCode)
	if err != nil {
		return nil, err
	}

	amount, err := decimal.New(req.AmountStroops, 7)
	if err != nil {
		return nil, errb.Code(pkgErrors.CodeInvalidAmount).Wrapf(err, "amount is out of range")
	}
	amount = amount.Trunc(6)
	if !amount.IsPos() {
		return nil, errb.Code(pkgErrors.CodeInvalidAmount).Errorf("amount is below the smallest usdc unit")
	}

	reference := lo.CoalesceOrEmpty(req.IdempotencyKey, req.LoanID)
	if reference == "" {
		return nil, errb.Code(pkgErrors.CodeMissingAccount).Errorf("an idempotency key or loan id is required")
	}

	return &disbursement{req: req, reference: reference, method: method, currency: currency, phone: phone, amount: amount}, nil
}

func (a *ChipperOffRampAdapter) payDirect(ctx context.Context, d *disbursement) (*offramp.Result, error) {
	errb := chipperAdapterErr("direct").With(pkgErrors.AttrLoanID, d.req.LoanID)

	addressID, err := a.depositAddressID(ctx)
	if err != nil {
		return nil, err
	}

	sentAt := a.now()
	txHash, err := a.treasury.SendUSDC(ctx, a.address, a.memo, d.req.AmountStroops)
	if err != nil {
		a.logger.WarnContext(ctx, "treasury deposit to chipper failed; trying the chipper balance",
			pkgErrors.AttrLoanID, d.req.LoanID, "error", err)
		if res, fbErr := a.payFromBalance(ctx, d); fbErr == nil {
			return res, nil
		}
		return nil, errb.Code(pkgErrors.CodeSubmitFailed).With(pkgErrors.AttrAmountStroops, d.req.AmountStroops).
			Wrapf(err, "treasury could not deposit usdc to chipper")
	}

	a.logger.InfoContext(ctx, "usdc deposited to chipper",
		pkgErrors.AttrLoanID, d.req.LoanID, pkgErrors.AttrTxHash, txHash, pkgErrors.AttrAmountStroops, d.req.AmountStroops)

	deposit, pendingReason := a.awaitCredit(ctx, addressID, txHash, sentAt)
	if deposit == nil {
		a.raise(ctx, d.req.LoanID, "Chipper deposit not credited",
			"USDC was deposited to Chipper but no credited deposit was found; the payout is pending.",
			txHash, pendingReason)
		return a.pendingResult(d, txHash, "", pendingReason), nil
	}

	payout, err := a.createPayout(ctx, d)
	if err != nil {
		reason := "payout_rejected"
		if apiErr, ok := chipper.AsAPIError(err); ok {
			reason = apiErr.Code
		}
		a.raise(ctx, d.req.LoanID, "Chipper payout not created",
			"USDC is on the Chipper balance but the payout could not be created; the payout is pending.",
			txHash, reason)
		return a.pendingResult(d, txHash, deposit.ID, reason), nil
	}
	res := a.payoutResult(d, payout, chipper.SettlementDirect, txHash, deposit.ID)
	return res, nil
}

// awaitCredit polls for the credited deposit made by txHash. A nil
// collection comes with the reason it is still pending.
func (a *ChipperOffRampAdapter) awaitCredit(ctx context.Context, addressID, txHash string, sentAt time.Time) (deposit *chipper.Collection, pendingReason string) {
	ctx, cancel := context.WithTimeout(ctx, a.creditTimeout)
	defer cancel()

	since := sentAt.Add(-chipperDepositLookback)
	ticker := time.NewTicker(a.pollInterval)
	defer ticker.Stop()

	for {
		got, found, err := a.client.FindCryptoDeposit(ctx, addressID, txHash, since)
		switch {
		case err != nil:
			a.logger.WarnContext(ctx, "chipper deposit lookup failed; retrying", pkgErrors.AttrTxHash, txHash, "error", err)
		case found && got.Status == chipper.CollectionCompleted:
			return got, ""
		case found && (got.Status == chipper.CollectionFailed || got.Status == chipper.CollectionReversed):
			return nil, "deposit_" + got.Status
		}
		select {
		case <-ctx.Done():
			return nil, "deposit_not_credited"
		case <-ticker.C:
		}
	}
}

func (a *ChipperOffRampAdapter) payFromBalance(ctx context.Context, d *disbursement) (*offramp.Result, error) {
	errb := chipperAdapterErr("fiat").With(pkgErrors.AttrLoanID, d.req.LoanID)

	available, err := a.usdcAvailable(ctx)
	if err != nil {
		return nil, err
	}
	needed, err := d.amount.Mul(a.headroom)
	if err != nil {
		return nil, errb.Code(pkgErrors.CodeInvalidAmount).Wrapf(err, "could not size the fee headroom")
	}
	if available.Cmp(needed) < 0 {
		return nil, errb.Code(pkgErrors.CodeInsufficientLiquidity).
			With("available", available.String()).With("needed", needed.String()).
			Errorf("chipper usdc balance cannot cover the payout")
	}

	payout, err := a.createPayout(ctx, d)
	if err != nil {
		return nil, errb.Code(pkgErrors.CodeSubmitFailed).Wrapf(err, "chipper rejected the payout")
	}
	return a.payoutResult(d, payout, chipper.SettlementFiat, "", ""), nil
}

// createPayout submits the payout. An ambiguous failure is resolved by
// reading the payout back, so a timed-out create that went through is not
// reported as a failure.
func (a *ChipperOffRampAdapter) createPayout(ctx context.Context, d *disbursement) (*chipper.Payout, error) {
	req := chipper.PayoutRequest{
		From: &chipper.PayoutSource{Amount: chipper.NewAmount(d.amount), Currency: chipperAsset},
		To: chipper.PayoutDestination{
			Code:          d.method.Code,
			AccountNumber: d.phone,
			Currency:      d.currency,
		},
		Narration:         chipperNarration,
		ExternalReference: d.reference,
		Metadata:          map[string]string{"loan_id": d.req.LoanID},
	}
	if name := strings.TrimSpace(d.req.RecipientName); name != "" && len(name) <= 200 {
		req.To.Recipient = &chipper.Recipient{Name: name}
	}

	payout, _, err := a.client.CreatePayout(ctx, req)
	if err == nil {
		return payout, nil
	}
	if apiErr, ok := chipper.AsAPIError(err); ok && !apiErr.Retryable() {
		return nil, err
	}
	if existing, getErr := a.client.GetPayoutByReference(ctx, d.reference); getErr == nil {
		return existing, nil
	}
	return nil, err
}

// ReleasePayout finishes a disbursement whose deposit reached Chipper but
// whose payout was left pending. The payout amount is the deposit's own
// on-chain amount. It is idempotent on the reference.
func (a *ChipperOffRampAdapter) ReleasePayout(ctx context.Context, rel chipper.Release) (*offramp.Result, error) {
	errb := chipperAdapterErr("release_payout").With(pkgErrors.AttrLoanID, rel.LoanID).With(pkgErrors.AttrTxHash, rel.DepositTxHash)
	if rel.DepositTxHash == "" {
		return nil, errb.Code(pkgErrors.CodeMissingAccount).Errorf("deposit tx hash is required")
	}

	addressID, err := a.depositAddressID(ctx)
	if err != nil {
		return nil, err
	}
	deposit, found, err := a.client.FindCryptoDeposit(ctx, addressID, rel.DepositTxHash, time.Time{})
	if err != nil {
		return nil, errb.Code(pkgErrors.CodeHTTPError).Wrapf(err, "could not look up the deposit")
	}
	if !found || deposit.Status != chipper.CollectionCompleted {
		return nil, errb.Code(pkgErrors.CodeIncompleteResponse).Errorf("deposit is not credited yet")
	}
	stroops, err := usdcToStroops(deposit.From.Amount)
	if err != nil {
		return nil, errb.Code(pkgErrors.CodeInvalidAmount).Wrapf(err, "deposit amount is out of range")
	}

	d, err := a.prepare(ctx, offramp.Request{
		LoanID:           rel.LoanID,
		RecipientName:    rel.RecipientName,
		AmountStroops:    stroops,
		DestinationPhone: rel.DestinationPhone,
		CountryCode:      rel.CountryCode,
		NetworkCode:      rel.NetworkCode,
		IdempotencyKey:   rel.Reference,
	})
	if err != nil {
		return nil, err
	}
	if existing, err := a.client.GetPayoutByReference(ctx, d.reference); err == nil {
		return a.payoutResult(d, existing, chipper.SettlementDirect, rel.DepositTxHash, deposit.ID), nil
	} else if !isChipperNotFound(err) {
		return nil, errb.Code(pkgErrors.CodeHTTPError).Wrapf(err, "could not check for an existing payout")
	}

	payout, err := a.createPayout(ctx, d)
	if err != nil {
		return nil, errb.Code(pkgErrors.CodeSubmitFailed).Wrapf(err, "chipper rejected the payout")
	}
	return a.payoutResult(d, payout, chipper.SettlementDirect, rel.DepositTxHash, deposit.ID), nil
}

// ReissuePayout repeats a failed payout from the Chipper balance under a new
// reference, to the same destination and for the same debit.
func (a *ChipperOffRampAdapter) ReissuePayout(ctx context.Context, failed *chipper.Payout, reference string) (*chipper.Payout, error) {
	errb := chipperAdapterErr("reissue_payout").With("failed_payout_id", failed.ID).With(pkgErrors.AttrSequenceID, reference)

	code, account := lo.FromPtr(failed.To.Code), lo.FromPtr(failed.To.AccountNumber)
	if code == "" || account == "" {
		return nil, errb.Code(pkgErrors.CodeIncompleteResponse).Errorf("failed payout carries no destination to repeat")
	}
	req := chipper.PayoutRequest{
		From:              &chipper.PayoutSource{Amount: chipper.NewAmount(failed.From.Amount), Currency: failed.From.Currency},
		To:                chipper.PayoutDestination{Code: code, AccountNumber: account, Currency: failed.To.Currency},
		Narration:         chipperNarration,
		ExternalReference: reference,
	}
	if loanID, ok := failed.Metadata["loan_id"].(string); ok && loanID != "" {
		req.Metadata = map[string]string{"loan_id": loanID}
	}

	payout, _, err := a.client.CreatePayout(ctx, req)
	if err == nil {
		return payout, nil
	}
	if apiErr, ok := chipper.AsAPIError(err); !ok || apiErr.Retryable() {
		if existing, getErr := a.client.GetPayoutByReference(ctx, reference); getErr == nil {
			return existing, nil
		}
	}
	return nil, errb.Code(pkgErrors.CodeSubmitFailed).Wrapf(err, "chipper rejected the repeated payout")
}

// Payout reads a payout by id.
func (a *ChipperOffRampAdapter) Payout(ctx context.Context, id string) (*chipper.Payout, error) {
	payout, err := a.client.GetPayout(ctx, id)
	if err != nil {
		return nil, chipperAdapterErr("payout").With("payout_id", id).Code(pkgErrors.CodeHTTPError).Wrapf(err, "could not read the payout")
	}
	return payout, nil
}

func usdcToStroops(amount decimal.Decimal) (int64, error) {
	scaled, err := amount.Mul(decimal.MustNew(10_000_000, 0))
	if err != nil {
		return 0, err
	}
	whole, frac, ok := scaled.Int64(0)
	if !ok || frac != 0 || whole <= 0 {
		return 0, chipperAdapterErr("usdc_to_stroops").With("amount", amount.String()).
			Code(pkgErrors.CodeInvalidAmount).Errorf("amount is not a positive whole number of stroops")
	}
	return whole, nil
}

// Status reads a payout. ref.ID is a payout id (pay_…) or the reference.
func (a *ChipperOffRampAdapter) Status(ctx context.Context, ref offramp.ProviderRef) (*offramp.Status, error) {
	errb := chipperAdapterErr("status").With("ref", ref.ID)
	if ref.ID == "" {
		return nil, errb.Code(pkgErrors.CodeMissingAccount).Errorf("payout id or reference is required")
	}

	var (
		payout *chipper.Payout
		err    error
	)
	if strings.HasPrefix(ref.ID, "pay_") {
		payout, err = a.client.GetPayout(ctx, ref.ID)
	} else {
		payout, err = a.client.GetPayoutByReference(ctx, ref.ID)
	}
	if err != nil {
		return nil, errb.Code(pkgErrors.CodeHTTPError).Wrapf(err, "could not read the payout")
	}

	st := &offramp.Status{
		RequestID:     payout.ID,
		SequenceID:    payout.ExternalReference,
		Status:        payout.Status,
		AmountLocal:   decimalFloat(payout.To.Amount),
		LocalCurrency: payout.To.Currency,
		CompletedAt:   payout.CompletedAt,
	}
	if payout.Status == chipper.PayoutFailed {
		st.FailureReason = lo.ToPtr(lo.CoalesceOrEmpty(lo.FromPtr(payout.StatusMessage), chipper.PayoutFailed))
	}
	return st, nil
}

// Quote returns Chipper's all-in rates. SellRate is local per USDC; BuyRate
// is local per USDC when buying, zero when Chipper has no reverse rate.
func (a *ChipperOffRampAdapter) Quote(ctx context.Context, q offramp.QuoteRequest) (*offramp.ExchangeRate, error) {
	errb := chipperAdapterErr("quote").With(pkgErrors.AttrCurrency, q.Currency)

	sell, err := a.client.GetRate(ctx, chipperAsset, q.Currency)
	if err != nil {
		return nil, errb.Code(pkgErrors.CodeRateUnavailable).Wrapf(err, "could not read the chipper rate")
	}

	rate := &offramp.ExchangeRate{
		FromCurrency: chipperAsset,
		ToCurrency:   strings.ToUpper(q.Currency),
		SellRate:     decimalFloat(sell.Rate),
		UpdatedAt:    a.now(),
	}
	if buy, err := a.client.GetRate(ctx, q.Currency, chipperAsset); err == nil {
		if inv, err := decimal.One.Quo(buy.Rate); err == nil {
			rate.BuyRate = decimalFloat(inv)
		}
	} else {
		a.logger.WarnContext(ctx, "chipper reverse rate unavailable; buy rate left empty", pkgErrors.AttrCurrency, q.Currency, "error", err)
	}
	return rate, nil
}

// SupportedProviders lists Chipper's usable mobile money methods for a
// country.
func (a *ChipperOffRampAdapter) SupportedProviders(ctx context.Context, countryCode string) ([]offramp.ProviderInfo, error) {
	methods, currency, err := a.mobileMoneyMethods(ctx, countryCode)
	if err != nil {
		return nil, err
	}
	return lo.Map(methods, func(m chipper.PaymentMethod, _ int) offramp.ProviderInfo {
		info := offramp.ProviderInfo{
			ID:               m.Code,
			Name:             m.Name,
			SupportedMethods: []string{offramp.PayoutMethodMobileMoney},
			Currency:         currency,
			Status:           m.Status,
		}
		if m.Limits != nil {
			info.MinAmount, info.MaxAmount = decimalFloat(m.Limits.Min), decimalFloat(m.Limits.Max)
		}
		return info
	}), nil
}

// Networks lists Chipper's usable mobile money methods as networks.
func (a *ChipperOffRampAdapter) Networks(ctx context.Context, countryCode string) ([]offramp.MobileMoneyNetwork, error) {
	methods, _, err := a.mobileMoneyMethods(ctx, countryCode)
	if err != nil {
		return nil, err
	}
	return lo.Map(methods, func(m chipper.PaymentMethod, _ int) offramp.MobileMoneyNetwork {
		return offramp.MobileMoneyNetwork{ID: m.Code, Name: m.Name, Code: m.Code, Status: m.Status}
	}), nil
}

// AvailableBalance reports the spendable USDC on the Chipper balance.
func (a *ChipperOffRampAdapter) AvailableBalance(ctx context.Context) (float64, error) {
	available, err := a.usdcAvailable(ctx)
	if err != nil {
		return 0, err
	}
	return decimalFloat(available), nil
}

func (a *ChipperOffRampAdapter) usdcAvailable(ctx context.Context) (decimal.Decimal, error) {
	accounts, err := a.client.ListAccounts(ctx)
	if err != nil {
		return decimal.Zero, chipperAdapterErr("balance").Code(pkgErrors.CodeHTTPError).Wrapf(err, "could not read chipper balances")
	}
	for _, acct := range accounts {
		if strings.EqualFold(acct.Currency, chipperAsset) {
			return acct.AvailableBalance, nil
		}
	}
	return decimal.Zero, nil
}

// payoutMethod resolves the configured method for a network and checks that
// Chipper offers it and it is not in outage.
func (a *ChipperOffRampAdapter) payoutMethod(ctx context.Context, country, networkCode string) (chipper.PaymentMethod, error) {
	errb := chipperAdapterErr("payout_method").With("country", country).With("network_code", networkCode)

	code, ok := a.methodCodes[networkCode]
	if !ok {
		return chipper.PaymentMethod{}, errb.Code(pkgErrors.CodeCorridorUnavailable).Errorf("no chipper payout method for this network")
	}
	caps, err := a.capabilities(ctx, country)
	if err != nil {
		return chipper.PaymentMethod{}, err
	}
	method, ok := caps.PayoutMethod(country, code)
	if !ok {
		return chipper.PaymentMethod{}, errb.With("method_code", code).
			Code(pkgErrors.CodeCorridorUnavailable).Errorf("chipper does not offer this payout method")
	}
	if !method.IsUsable() {
		return chipper.PaymentMethod{}, errb.With("method_code", code).
			Code(pkgErrors.CodeCorridorUnavailable).Errorf("chipper payout method is in outage")
	}
	return method, nil
}

func (a *ChipperOffRampAdapter) mobileMoneyMethods(ctx context.Context, country string) ([]chipper.PaymentMethod, string, error) {
	caps, err := a.capabilities(ctx, country)
	if err != nil {
		return nil, "", err
	}
	for _, cc := range caps.Payouts {
		if !strings.EqualFold(cc.Country.Code, country) {
			continue
		}
		methods := lo.Filter(cc.Methods, func(m chipper.PaymentMethod, _ int) bool {
			return m.Type == "mobile_money" && m.IsUsable()
		})
		return methods, cc.Currency.Code, nil
	}
	return nil, "", nil
}

func (a *ChipperOffRampAdapter) capabilities(ctx context.Context, country string) (*chipper.Capabilities, error) {
	key := strings.ToUpper(country)
	a.capsMu.Lock()
	c, ok := a.caps[key]
	fresh := ok && a.now().Before(a.capsExpires[key])
	a.capsMu.Unlock()
	if fresh {
		return c, nil
	}

	c, err := a.client.GetCountryCapabilities(ctx, key)
	if err != nil {
		return nil, chipperAdapterErr("capabilities").With("country", key).
			Code(pkgErrors.CodeHTTPError).Wrapf(err, "could not read chipper capabilities")
	}
	a.capsMu.Lock()
	a.caps[key], a.capsExpires[key] = c, a.now().Add(chipperCapsTTL)
	a.capsMu.Unlock()
	return c, nil
}

func (a *ChipperOffRampAdapter) payoutResult(d *disbursement, p *chipper.Payout, method, txHash, depositID string) *offramp.Result {
	res := &offramp.Result{
		RequestID:        p.ID,
		SequenceID:       lo.CoalesceOrEmpty(p.ExternalReference, d.reference),
		Status:           p.Status,
		AmountUSD:        decimalFloat(p.From.Amount),
		AmountLocal:      decimalFloat(p.To.Amount),
		LocalCurrency:    p.To.Currency,
		CreatedAt:        p.CreatedAt,
		SettlementMethod: method,
		Provider: chipper.OffRampPayload{
			MethodCode:          d.method.Code,
			PayoutID:            p.ID,
			DepositTxHash:       txHash,
			DepositCollectionID: depositID,
		},
	}
	if p.Rate != nil {
		res.ExchangeRate = decimalFloat(*p.Rate)
	}
	if p.Fee != nil {
		res.Fee = decimalFloat(p.Fee.Amount)
		if p.Rate != nil && strings.EqualFold(p.Fee.Currency, chipperAsset) {
			if local, err := p.Fee.Amount.Mul(*p.Rate); err == nil {
				res.FeeLocal = decimalFloat(local)
			}
		}
	}
	return res
}

func (a *ChipperOffRampAdapter) pendingResult(d *disbursement, txHash, depositID, reason string) *offramp.Result {
	return &offramp.Result{
		RequestID:        txHash,
		SequenceID:       d.reference,
		Status:           chipperStatusPending,
		AmountUSD:        decimalFloat(d.amount),
		LocalCurrency:    d.currency,
		CreatedAt:        a.now(),
		SettlementMethod: chipper.SettlementDirect,
		Provider: chipper.OffRampPayload{
			MethodCode:          d.method.Code,
			DepositTxHash:       txHash,
			DepositCollectionID: depositID,
			PayoutPending:       true,
			PendingReason:       reason,
		},
	}
}

func (a *ChipperOffRampAdapter) raise(ctx context.Context, loanID, subject, message, txHash, reason string) {
	alerts.Raise(ctx, a.alerts, a.logger, subject,
		message+" loan="+loanID+" tx="+txHash+" reason="+reason)
}

func readChipperOptions(opts offramp.ProviderOptions) (chipper.Options, error) {
	if opts == nil {
		return chipper.Options{SettlementMethod: chipper.SettlementDirect}, nil
	}
	o, ok := opts.(chipper.Options)
	if !ok {
		return chipper.Options{}, chipperAdapterErr("options").With("got", string(opts.ProviderID())).
			Code(pkgErrors.CodeInvalidConfig).Errorf("options belong to another provider")
	}
	switch o.SettlementMethod {
	case "":
		o.SettlementMethod = chipper.SettlementDirect
	case chipper.SettlementDirect, chipper.SettlementFiat:
	default:
		return chipper.Options{}, chipperAdapterErr("options").With("settlement_method", o.SettlementMethod).
			Code(pkgErrors.CodeInvalidConfig).Errorf("unknown settlement method")
	}
	return o, nil
}

func isChipperNotFound(err error) bool {
	apiErr, ok := chipper.AsAPIError(err)
	return ok && apiErr.Code == chipper.CodeNotFound
}

func decimalFloat(d decimal.Decimal) float64 {
	f, _ := d.Float64()
	return f
}
