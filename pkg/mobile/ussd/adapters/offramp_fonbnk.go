package adapters

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/samber/lo"
	"github.com/samber/oops"

	pkgErrors "github.com/Shamba-Records-Limited/microvault/pkg/errors"
	"github.com/Shamba-Records-Limited/microvault/pkg/payment/fonbnk"
	"github.com/Shamba-Records-Limited/microvault/pkg/payment/offramp"
	phoneutil "github.com/Shamba-Records-Limited/microvault/pkg/phone"
)

func fonbnkAdapterErr(op string) oops.OopsErrorBuilder {
	return oops.In(pkgErrors.DomainOffRamp).
		Tags("fonbnk").
		With(pkgErrors.AttrProvider, fonbnk.ProviderName).
		With(pkgErrors.AttrOperation, op)
}

// FonbnkClient is the slice of the Fonbnk client this adapter needs.
type FonbnkClient interface {
	CreateOrder(ctx context.Context, req fonbnk.CreateOrderRequest) (*fonbnk.CreateOrderResponse, error)
	CreateOrderFromQuote(ctx context.Context, quote *fonbnk.Quote, req fonbnk.CreateOrderRequest) (*fonbnk.CreateOrderResponse, error)
	ConfirmOrder(ctx context.Context, orderID string, fields map[string]string) (*fonbnk.Order, error)
	CancelOrder(ctx context.Context, orderID string) (*fonbnk.Order, error)
	GetOrder(ctx context.Context, orderID string) (*fonbnk.Order, error)
	QuoteOffRamp(ctx context.Context, crypto fonbnk.CryptoLeg, fiat fonbnk.FiatLeg, cryptoAmount float64) (*fonbnk.Quote, error)
}

// FonbnkOffRampAdapter disburses a loan through Fonbnk: the treasury sends
// USDC against an order, Fonbnk pays out mobile money.
type FonbnkOffRampAdapter struct {
	client         FonbnkClient
	treasury       offramp.TreasuryTransfer
	cryptoCode     string
	treasuryPubkey string
	channel        string
	logger         *slog.Logger
}

// FonbnkOffRampConfig contains configuration for the Fonbnk off-ramp adapter.
type FonbnkOffRampConfig struct {
	Client   FonbnkClient
	Treasury offramp.TreasuryTransfer

	// CryptoCurrencyCode is Fonbnk's own code, e.g. STELLAR_USDC.
	CryptoCurrencyCode string

	// TreasuryAddress is echoed to Fonbnk as the sending wallet.
	TreasuryAddress string

	PaymentChannel string
	Logger         *slog.Logger
}

var (
	_ offramp.Provider     = (*FonbnkOffRampAdapter)(nil)
	_ offramp.StatusReader = (*FonbnkOffRampAdapter)(nil)
)

// NewFonbnkOffRampAdapter validates the config and returns an adapter.
func NewFonbnkOffRampAdapter(cfg FonbnkOffRampConfig) (*FonbnkOffRampAdapter, error) {
	errb := fonbnkAdapterErr("new")

	if cfg.Client == nil {
		return nil, errb.Code(pkgErrors.CodeMissingDependency).Errorf("fonbnk client is nil")
	}
	if cfg.Treasury == nil {
		return nil, errb.Code(pkgErrors.CodeMissingDependency).Errorf("treasury transfer is nil")
	}
	if cfg.CryptoCurrencyCode == "" {
		return nil, errb.Code(pkgErrors.CodeMissingDependency).Errorf("crypto currency code is required")
	}

	channel := cfg.PaymentChannel
	if channel == "" {
		channel = fonbnk.ChannelMobileMoney
	}
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}

	return &FonbnkOffRampAdapter{
		client:         cfg.Client,
		treasury:       cfg.Treasury,
		cryptoCode:     cfg.CryptoCurrencyCode,
		treasuryPubkey: cfg.TreasuryAddress,
		channel:        channel,
		logger:         logger.With("component", "fonbnk_offramp"),
	}, nil
}

// ID identifies this provider in the registry.
func (a *FonbnkOffRampAdapter) ID() offramp.ProviderID { return offramp.ProviderFonbnk }

// Initiate opens an order, sends the treasury's USDC against it and confirms
// the deposit.
func (a *FonbnkOffRampAdapter) Initiate(ctx context.Context, req offramp.Request) (*offramp.Result, error) {
	opts, err := readFonbnkOptions(req.Options)
	if err != nil {
		return nil, err
	}
	errb := fonbnkAdapterErr("initiate").With(pkgErrors.AttrLoanID, req.LoanID)

	if req.AmountStroops <= 0 {
		return nil, errb.With(pkgErrors.AttrAmountStroops, req.AmountStroops).
			Code(pkgErrors.CodeInvalidAmount).Errorf("amount must be positive")
	}
	if opts.UserEmail == "" || opts.UserIP == "" {
		return nil, errb.Code(pkgErrors.CodeMissingAccount).
			Errorf("fonbnk requires a user email and IP on every order")
	}

	cryptoAmount := float64(req.AmountStroops) / 1e7
	orderParams := lo.CoalesceOrEmpty(req.IdempotencyKey, req.LoanID)

	order, err := a.createOrder(ctx, req, opts, cryptoAmount, orderParams)
	if err != nil {
		return nil, errb.Code(pkgErrors.CodeDepositInitFailed).Wrapf(err, "fonbnk refused the order")
	}

	address, memo, err := depositTarget(order)
	if err != nil {
		a.cancelQuietly(ctx, order.ID, req.LoanID)
		return nil, errb.With(pkgErrors.AttrOrderID, order.ID).
			Code(pkgErrors.CodeIncompleteResponse).Wrapf(err, "order carries no usable deposit target")
	}

	a.logger.Info("fonbnk order created",
		pkgErrors.AttrLoanID, req.LoanID,
		pkgErrors.AttrOrderID, order.ID,
		"status", order.Status,
		"deposit_address", address,
		"amount_stroops", req.AmountStroops)

	txHash, err := a.treasury.SendUSDC(ctx, address, memo, req.AmountStroops)
	if err != nil {
		// No funds moved, so the order is dead weight. Cancelling keeps
		// Fonbnk's dashboard honest but must not mask the send failure.
		a.cancelQuietly(ctx, order.ID, req.LoanID)
		return nil, errb.With(pkgErrors.AttrOrderID, order.ID).
			Code(pkgErrors.CodeSubmitFailed).Wrapf(err, "treasury could not send USDC to fonbnk")
	}

	confirmed := a.confirm(ctx, order.ID, req.LoanID, txHash)

	return &offramp.Result{
		RequestID:        order.ID,
		SequenceID:       orderParams,
		Status:           order.Status,
		AmountUSD:        cryptoAmount,
		AmountLocal:      order.Payout.Cashout.AmountAfterFees,
		LocalCurrency:    order.Payout.CurrencyCode,
		ExchangeRate:     order.Payout.Cashout.Rate,
		Fee:              order.Deposit.Cashout.TotalChargedFeesUSD,
		FeeLocal:         order.Payout.Cashout.TotalChargedFees,
		CreatedAt:        time.Now(),
		SettlementMethod: "direct",
		Provider: fonbnk.OffRampPayload{
			OrderID:        order.ID,
			OrderParams:    orderParams,
			StellarAddress: address,
			StellarMemo:    memo,
			StellarTxHash:  txHash,
			Confirmed:      confirmed,
		},
	}, nil
}

// createOrder opens the order, reusing a locked quote when the caller supplied
// one so the borrower is disbursed at the rate the relay compared.
func (a *FonbnkOffRampAdapter) createOrder(
	ctx context.Context,
	req offramp.Request,
	opts fonbnk.Options,
	cryptoAmount float64,
	orderParams string,
) (*fonbnk.Order, error) {
	fields := map[string]string{
		fonbnk.FieldPhoneNumber:             phoneutil.E164(req.DestinationPhone),
		fonbnk.FieldBlockchainWalletAddress: a.treasuryPubkey,
	}
	if opts.SandboxForcedFlow != "" {
		fields[fonbnk.FieldDepositSandboxForcedFlow] = opts.SandboxForcedFlow
	}

	amount := cryptoAmount
	orderReq := fonbnk.CreateOrderRequest{
		UserEmail:          opts.UserEmail,
		UserCountryIsoCode: req.CountryCode,
		UserIP:             opts.UserIP,
		Deposit: fonbnk.LegSpec{
			PaymentChannel: fonbnk.ChannelCrypto,
			CurrencyType:   fonbnk.CurrencyTypeCrypto,
			CurrencyCode:   a.cryptoCode,
			Amount:         &amount,
		},
		Payout: fonbnk.LegSpec{
			PaymentChannel: a.channel,
			CurrencyType:   fonbnk.CurrencyTypeFiat,
			CurrencyCode:   localCurrencyFor(req),
			CountryIsoCode: req.CountryCode,
			CarrierCode:    opts.CarrierCode,
		},
		FieldsToCreateOrder: fields,
		OrderParams:         orderParams,
	}

	var (
		resp *fonbnk.CreateOrderResponse
		err  error
	)
	if opts.Quote != nil {
		resp, err = a.client.CreateOrderFromQuote(ctx, opts.Quote, orderReq)
	} else {
		resp, err = a.client.CreateOrder(ctx, orderReq)
	}
	if err != nil {
		return nil, err
	}
	return &resp.Order, nil
}

// confirm tells Fonbnk the deposit landed, reporting whether it was accepted.
//
// A failure here is logged loudly rather than returned: the USDC has already
// left the treasury, and Fonbnk detects incoming payments on its own, so
// failing the disbursement would discard the order reference for a step that
// only accelerates settlement.
func (a *FonbnkOffRampAdapter) confirm(ctx context.Context, orderID, loanID, txHash string) bool {
	_, err := a.client.ConfirmOrder(ctx, orderID, map[string]string{
		fonbnk.FieldBlockchainTxHash: txHash,
	})
	if err == nil {
		return true
	}
	a.logger.Error("CRITICAL: USDC sent to fonbnk but the deposit could not be confirmed",
		pkgErrors.AttrLoanID, loanID,
		pkgErrors.AttrOrderID, orderID,
		pkgErrors.AttrTxHash, txHash,
		"error", err)
	return false
}

// cancelQuietly releases an order no funds were sent against.
func (a *FonbnkOffRampAdapter) cancelQuietly(ctx context.Context, orderID, loanID string) {
	if _, err := a.client.CancelOrder(ctx, orderID); err != nil {
		a.logger.Warn("could not cancel the unfunded fonbnk order",
			pkgErrors.AttrLoanID, loanID, pkgErrors.AttrOrderID, orderID, "error", err)
	}
}

// Status looks up an in-flight order. ref.ID is Fonbnk's order ID.
func (a *FonbnkOffRampAdapter) Status(ctx context.Context, ref offramp.ProviderRef) (*offramp.Status, error) {
	errb := fonbnkAdapterErr("status").With(pkgErrors.AttrOrderID, ref.ID)

	if ref.ID == "" {
		return nil, errb.Code(pkgErrors.CodeMissingAccount).Errorf("order id is required")
	}

	order, err := a.client.GetOrder(ctx, ref.ID)
	if err != nil {
		return nil, errb.Code(pkgErrors.CodeHTTPError).Wrapf(err, "could not read the order")
	}

	status := &offramp.Status{
		RequestID:     order.ID,
		SequenceID:    order.MerchantOrderParams,
		Status:        order.Status,
		AmountLocal:   order.Payout.Cashout.AmountAfterFees,
		LocalCurrency: order.Payout.CurrencyCode,
	}
	if fonbnk.IsTerminal(order.Status) {
		completed := order.UpdatedAt
		status.CompletedAt = &completed
	}
	if order.Status == fonbnk.StatusPayoutFailed || order.Status == fonbnk.StatusDepositInvalid {
		reason := order.Status
		status.FailureReason = &reason
	}
	return status, nil
}

// depositTarget reads the address the treasury must pay, and the memo Stellar
// needs to attribute it.
func depositTarget(order *fonbnk.Order) (address, memo string, err error) {
	instructions := order.Deposit.TransferInstructions
	if instructions == nil {
		return "", "", fonbnkAdapterErr("deposit_target").
			Code(pkgErrors.CodeIncompleteResponse).Errorf("order carries no transfer instructions")
	}

	address = transferDetail(instructions.TransferDetails, fonbnk.DetailRecipientWalletAddress)
	memo = transferDetail(instructions.TransferDetails, detailCryptoAdditionalData)
	if address == "" {
		return "", "", fonbnkAdapterErr("deposit_target").
			With("transfer_type", instructions.Type).
			Code(pkgErrors.CodeIncompleteResponse).Errorf("order names no recipient wallet address")
	}
	return address, memo, nil
}

// transferDetail reads one instruction line by id.
func transferDetail(details []fonbnk.TransferDetail, id string) string {
	detail, _ := lo.Find(details, func(d fonbnk.TransferDetail) bool { return d.ID == id })
	return detail.Value
}

// detailCryptoAdditionalData carries a Stellar memo when the payout wallet
// needs one.
const detailCryptoAdditionalData = "cryptoTransactionRequestAdditionalData"

// localCurrencyFor picks the payout currency for a request.
func localCurrencyFor(req offramp.Request) string {
	if req.NetworkCode != "" && len(req.NetworkCode) == 3 {
		return req.NetworkCode
	}
	return offramp.LocalCurrency(req.CountryCode)
}

// readFonbnkOptions extracts the typed fonbnk.Options from a Request.
func readFonbnkOptions(opts offramp.ProviderOptions) (fonbnk.Options, error) {
	if opts == nil {
		return fonbnk.Options{}, fonbnkAdapterErr("parse_options").
			Code(pkgErrors.CodeMissingAccount).Errorf("request options are required")
	}
	if v, ok := opts.(fonbnk.Options); ok {
		return v, nil
	}
	return fonbnk.Options{}, fonbnkAdapterErr("parse_options").
		With("got_type", fmt.Sprintf("%T", opts)).
		Code(pkgErrors.CodeDecodeFailed).Errorf("request options are the wrong type")
}
