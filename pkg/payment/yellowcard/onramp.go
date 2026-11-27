package yellowcard

import (
	"context"
	"strconv"
	"strings"

	"github.com/samber/oops"
	"github.com/stellar/go-stellar-sdk/strkey"

	pkgErrors "github.com/Shamba-Records-Limited/microvault/pkg/errors"
)

// StellarReceive collects local currency from a mobile money payer and
// settles it as USDC on Stellar to Destination with Memo, in one receive.
type StellarReceive struct {
	SequenceID  string
	CustomerUID string
	ChannelID   string
	// NetworkID is the payer's mobile money network, from GetNetworks.
	NetworkID string
	// PayerPhone is the mobile money number in international form (+254…),
	// as YellowCard requires for momo.
	PayerPhone string
	// Payer is the retail KYC block. Every field in RetailPayerFields is
	// required by YellowCard.
	Payer       Party
	LocalAmount float64
	Currency    string
	Country     string
	Reason      string
	Destination string
	Memo        string
}

// RetailPayerFields are the payer fields YellowCard requires on a retail
// receive.
var RetailPayerFields = []string{"name", "country", "phone", "email", "address", "dob", "idType", "idNumber"}

// onRampErr starts an error builder for the pay-in direction.
func onRampErr(op string) oops.OopsErrorBuilder {
	return oops.In(pkgErrors.DomainOnRamp).Tags("yellowcard").
		With(pkgErrors.AttrProvider, "yellowcard").
		With(pkgErrors.AttrOperation, op)
}

// MissingRetailFields lists the required retail payer fields that are empty.
func MissingRetailFields(p Party) []string {
	values := map[string]string{
		"name": p.Name, "country": p.Country, "phone": p.Phone, "email": p.Email,
		"address": p.Address, "dob": p.DOB, "idType": p.IDType, "idNumber": p.IDNumber,
	}
	var missing []string
	for _, f := range RetailPayerFields {
		if strings.TrimSpace(values[f]) == "" {
			missing = append(missing, f)
		}
	}
	return missing
}

// Request validates the receive and builds the direct-settlement request.
func (r StellarReceive) Request() (CollectionRequest, error) {
	errb := onRampErr("stellar_receive").With(pkgErrors.AttrSequenceID, r.SequenceID)

	switch {
	case r.SequenceID == "" || r.CustomerUID == "" || r.ChannelID == "" || r.NetworkID == "":
		return CollectionRequest{}, errb.Code(pkgErrors.CodeMissingDependency).
			Errorf("sequence id, customer uid, channel id and network id are required")
	case r.PayerPhone == "":
		return CollectionRequest{}, errb.Code(pkgErrors.CodeMissingPhoneNumber).Errorf("payer phone is required")
	case !isInternationalPhone(r.PayerPhone):
		return CollectionRequest{}, errb.Code(pkgErrors.CodeInvalidPhoneNumber).
			Errorf("payer phone must be in international form with a leading +")
	case r.LocalAmount <= 0:
		return CollectionRequest{}, errb.Code(pkgErrors.CodeInvalidAmount).With("local_amount", r.LocalAmount).
			Errorf("local amount must be positive")
	case r.Currency == "" || r.Country == "":
		return CollectionRequest{}, errb.Code(pkgErrors.CodeMissingDependency).Errorf("currency and country are required")
	case !strkey.IsValidEd25519PublicKey(r.Destination):
		return CollectionRequest{}, errb.Code(pkgErrors.CodeInvalidAddress).With(pkgErrors.AttrAddress, r.Destination).
			Errorf("destination is not a stellar account address")
	}
	if _, err := strconv.ParseUint(r.Memo, 10, 64); err != nil {
		return CollectionRequest{}, errb.Code(pkgErrors.CodeInvalidMemo).With("memo", r.Memo).
			Errorf("memo must be a stellar memo id")
	}
	if missing := MissingRetailFields(r.Payer); len(missing) > 0 {
		return CollectionRequest{}, errb.Code(pkgErrors.CodeMissingPayerKYC).With("missing_fields", missing).
			Errorf("retail payer kyc is incomplete")
	}

	reason := r.Reason
	if reason == "" {
		reason = "other"
	}
	return CollectionRequest{
		ChannelID:    r.ChannelID,
		SequenceID:   r.SequenceID,
		CustomerUID:  r.CustomerUID,
		CustomerType: CustomerTypeRetail,
		ForceAccept:  true,
		ChannelType:  ChannelTypeMomo,
		Source: CollectionSource{
			AccountType:   ChannelTypeMomo,
			AccountNumber: r.PayerPhone,
			NetworkID:     r.NetworkID,
		},
		Recipient:        r.Payer,
		LocalAmount:      r.LocalAmount,
		Currency:         r.Currency,
		Country:          r.Country,
		Reason:           reason,
		DirectSettlement: true,
		SettlementInfo: &SettlementInfo{
			WalletAddress:  r.Destination,
			WalletTag:      r.Memo,
			CryptoCurrency: CryptoCurrencyUSDC,
			CryptoNetwork:  CryptoNetworkXLM,
		},
	}, nil
}

// isInternationalPhone reports a leading + followed by 8 to 15 digits.
func isInternationalPhone(s string) bool {
	digits, ok := strings.CutPrefix(s, "+")
	if !ok || len(digits) < 8 || len(digits) > 15 {
		return false
	}
	for _, c := range digits {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

// SubmitStellarReceive validates and submits a direct-settlement receive.
func (y *YellowcardAdapter) SubmitStellarReceive(ctx context.Context, r StellarReceive) (*Collection, error) {
	req, err := r.Request()
	if err != nil {
		return nil, err
	}
	return y.SubmitCollection(ctx, req)
}

// IsReceiveSettled reports whether a direct-settlement receive has paid out
// its crypto.
func IsReceiveSettled(status string) bool {
	return status == StatusSettlementComplete
}
