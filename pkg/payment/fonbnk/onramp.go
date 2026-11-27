package fonbnk

import (
	"context"
	"net"
	"strconv"
	"strings"

	"github.com/samber/oops"
	"github.com/stellar/go-stellar-sdk/strkey"

	pkgErrors "github.com/Shamba-Records-Limited/microvault/pkg/errors"
)

// CryptoStellarUSDC is Fonbnk's code for USDC on Stellar.
const CryptoStellarUSDC = "STELLAR_USDC"

// StellarOnRamp buys USDCAmount of USDC on Stellar for Destination with Memo,
// collected from a mobile money payer by STK push.
//
// UserEmail and UserIP identify the end user to Fonbnk and are required on
// every order; the user must also satisfy on-ramp KYC (GetUserKYCState).
type StellarOnRamp struct {
	UserEmail      string
	UserIP         string
	CountryIsoCode string
	Currency       string
	CarrierCode    string
	PayerPhone     string
	USDCAmount     float64
	Destination    string
	Memo           string
	// OrderParams is our reference, echoed back and searchable.
	OrderParams string
	WebhookURL  string
}

func onRampErr(op string) oops.OopsErrorBuilder {
	return oops.In(pkgErrors.DomainOnRamp).Tags("fonbnk").
		With(pkgErrors.AttrProvider, ProviderName).
		With(pkgErrors.AttrOperation, op)
}

func (r StellarOnRamp) fiatLeg() FiatLeg {
	return FiatLeg{
		CurrencyCode:   r.Currency,
		CountryIsoCode: r.CountryIsoCode,
		PaymentChannel: ChannelMobileMoney,
		CarrierCode:    r.CarrierCode,
		TransferType:   TransferTypeSTKPush,
	}
}

// Validate checks everything Fonbnk would otherwise refuse, without calling it.
func (r StellarOnRamp) Validate() error {
	errb := onRampErr("stellar_on_ramp").With("order_params", r.OrderParams)
	switch {
	case strings.TrimSpace(r.UserEmail) == "":
		return errb.Code(pkgErrors.CodeMissingPayerEmail).Errorf("user email is required on every order")
	case net.ParseIP(r.UserIP) == nil:
		return errb.Code(pkgErrors.CodeMissingPayerIP).With("user_ip", r.UserIP).
			Errorf("user ip is required on every order and must be a valid address")
	case r.PayerPhone == "":
		return errb.Code(pkgErrors.CodeMissingPhoneNumber).Errorf("payer phone is required")
	case r.CountryIsoCode == "" || r.Currency == "" || r.CarrierCode == "":
		return errb.Code(pkgErrors.CodeMissingDependency).Errorf("country, currency and carrier are required")
	case r.USDCAmount <= 0:
		return errb.Code(pkgErrors.CodeInvalidAmount).With("usdc_amount", r.USDCAmount).
			Errorf("usdc amount must be positive")
	case !strkey.IsValidEd25519PublicKey(r.Destination):
		return errb.Code(pkgErrors.CodeInvalidAddress).With(pkgErrors.AttrAddress, r.Destination).
			Errorf("destination is not a stellar account address")
	}
	if _, err := strconv.ParseUint(r.Memo, 10, 64); err != nil {
		return errb.Code(pkgErrors.CodeInvalidMemo).With("memo", r.Memo).Errorf("memo must be a stellar memo id")
	}
	return nil
}

// QuoteStellarOnRamp prices the on-ramp with the USDC amount on the payout
// leg, pinned to an STK push deposit.
func (a *FonbnkAdapter) QuoteStellarOnRamp(ctx context.Context, r StellarOnRamp) (*Quote, error) {
	if err := r.Validate(); err != nil {
		return nil, err
	}
	return a.QuoteOnRamp(ctx, r.fiatLeg(), CryptoLeg{CurrencyCode: CryptoStellarUSDC}, r.USDCAmount)
}

// CreateStellarOnRamp opens the order at the quoted price. The deposit is an
// STK push: send it with RetrySTKPush once the order's transfer instructions
// allow, then ConfirmOrder. Needs the create-users permission.
func (a *FonbnkAdapter) CreateStellarOnRamp(ctx context.Context, quote *Quote, r StellarOnRamp) (*Order, error) {
	if err := r.Validate(); err != nil {
		return nil, err
	}
	amount := r.USDCAmount
	resp, err := a.CreateOrderFromQuote(ctx, quote, CreateOrderRequest{
		UserEmail:          r.UserEmail,
		UserCountryIsoCode: r.CountryIsoCode,
		UserIP:             r.UserIP,
		Payout:             LegSpec{Amount: &amount},
		FieldsToCreateOrder: map[string]string{
			FieldPhoneNumber:             r.PayerPhone,
			FieldBlockchainWalletAddress: r.Destination,
			FieldBlockchainMemo:          r.Memo,
		},
		OrderParams: r.OrderParams,
		WebhookURL:  r.WebhookURL,
	})
	if err != nil {
		return nil, err
	}
	return &resp.Order, nil
}

// IsOnRampSettled reports whether the USDC has been paid out.
func IsOnRampSettled(status string) bool { return status == StatusPayoutSuccessful }
