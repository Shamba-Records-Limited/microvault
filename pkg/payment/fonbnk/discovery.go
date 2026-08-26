package fonbnk

import (
	"context"
	"net/http"
	"net/url"

	"github.com/samber/lo"

	pkgErrors "github.com/Shamba-Records-Limited/microvault/pkg/errors"
)

// GetAvailableCurrencies lists everything tradable. Cached 60s server-side, so
// the allowed flags can be a minute stale — treat them as a display filter,
// not a guarantee.
func (a *FonbnkAdapter) GetAvailableCurrencies(ctx context.Context) ([]Currency, error) {
	out, err := call[[]Currency](ctx, a, fonbnkErr("get_currencies"), http.MethodGet, pathCurrencies, nil)
	if err != nil {
		return nil, err
	}
	return *out, nil
}

// GetOrderLimits returns the tradable window for one corridor.
//
// Fonbnk answers 200 with every field zero when the pair cannot be traded, so
// that case is turned into CodeCorridorUnavailable rather than returned as a
// limit of zero.
func (a *FonbnkAdapter) GetOrderLimits(ctx context.Context, q OrderLimitsQuery) (*OrderLimits, error) {
	errb := fonbnkErr("get_order_limits").
		With("deposit_currency", q.DepositCurrencyCode).
		With("payout_currency", q.PayoutCurrencyCode)

	endpoint := withQuery(pathOrderLimits, q.values())
	limits, err := call[OrderLimits](ctx, a, errb, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	if !limits.IsTradable() {
		return nil, errb.Code(pkgErrors.CodeCorridorUnavailable).
			Errorf("Fonbnk cannot trade this corridor right now")
	}
	return limits, nil
}

// values renders the query, omitting unset optional params.
func (q OrderLimitsQuery) values() url.Values {
	v := url.Values{}
	for key, value := range map[string]string{
		"depositPaymentChannel": q.DepositPaymentChannel,
		"depositCurrencyType":   q.DepositCurrencyType,
		"depositCurrencyCode":   q.DepositCurrencyCode,
		"depositCarrierCode":    q.DepositCarrierCode,
		"depositCountryIsoCode": q.DepositCountryIsoCode,
		"payoutPaymentChannel":  q.PayoutPaymentChannel,
		"payoutCurrencyType":    q.PayoutCurrencyType,
		"payoutCurrencyCode":    q.PayoutCurrencyCode,
		"payoutCarrierCode":     q.PayoutCarrierCode,
		"payoutCountryIsoCode":  q.PayoutCountryIsoCode,
	} {
		if value != "" {
			v.Set(key, value)
		}
	}
	return v
}

// FindCurrency returns the entry for a currency, narrowed by country when the
// currency spans several.
func FindCurrency(currencies []Currency, currencyCode, countryIsoCode string) (Currency, bool) {
	return lo.Find(currencies, func(c Currency) bool {
		if c.CurrencyCode != currencyCode {
			return false
		}
		return countryIsoCode == "" || c.CurrencyDetails.CountryIsoCode == countryIsoCode
	})
}

// DepositChannels returns the channels a currency can currently be paid in on.
func (c Currency) DepositChannels() []PaymentChannelInfo {
	return lo.Filter(c.PaymentChannels, func(p PaymentChannelInfo, _ int) bool {
		return p.IsDepositAllowed
	})
}

// PayoutChannels returns the channels a currency can currently be paid out on.
func (c Currency) PayoutChannels() []PaymentChannelInfo {
	return lo.Filter(c.PaymentChannels, func(p PaymentChannelInfo, _ int) bool {
		return p.IsPayoutAllowed
	})
}

// Channel returns the entry for a channel type.
func (c Currency) Channel(channelType string) (PaymentChannelInfo, bool) {
	return lo.Find(c.PaymentChannels, func(p PaymentChannelInfo) bool {
		return p.Type == channelType
	})
}
