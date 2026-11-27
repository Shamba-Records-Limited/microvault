package ussd

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type routingRateSvc struct {
	quote    *DisbursementQuote
	quoteErr error
	got      DisbursementQuoteRequest
	calls    int
}

func (r *routingRateSvc) GetExchangeRate(context.Context, string) (float64, error) { return 128, nil }

func (r *routingRateSvc) QuoteDisbursement(_ context.Context, req DisbursementQuoteRequest) (*DisbursementQuote, error) {
	r.calls++
	r.got = req
	return r.quote, r.quoteErr
}

func TestQuoteDisbursement(t *testing.T) {
	tests := []struct {
		name         string
		svc          RateService
		payoutMethod string
		wantRate     float64
		wantProvider string
	}{
		{"mobile money uses the routed quote", &routingRateSvc{quote: &DisbursementQuote{Provider: "chipper", SellRate: 130.2}}, "mobile_money", 130.2, "chipper"},
		{"empty payout method is mobile money", &routingRateSvc{quote: &DisbursementQuote{Provider: "chipper", SellRate: 130.2}}, "", 130.2, "chipper"},
		{"cash pickup keeps the shared rate", &routingRateSvc{quote: &DisbursementQuote{Provider: "chipper", SellRate: 130.2}}, "cash_pickup", 128, ""},
		{"routed quote failure falls back unpinned", &routingRateSvc{quoteErr: errors.New("relay down")}, "mobile_money", 128, ""},
		{"rate service without routing", fakeRateSvc{}, "mobile_money", 130, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := &USSDHandler{rateService: tt.svc}
			rate, provider, err := h.quoteDisbursement(context.Background(), tt.payoutMethod, 1_000_000, "KES", "KE")
			require.NoError(t, err)
			assert.InDelta(t, tt.wantRate, rate, 1e-9)
			assert.Equal(t, tt.wantProvider, provider)
		})
	}

	svc := &routingRateSvc{quote: &DisbursementQuote{Provider: "chipper", SellRate: 130.2}}
	_, _, err := (&USSDHandler{rateService: svc}).quoteDisbursement(context.Background(), "", 1_000_000, "KES", "KE")
	require.NoError(t, err)
	assert.Equal(t, DisbursementQuoteRequest{LocalAmount: 1_000_000, LocalCurrency: "KES", CountryCode: "KE"}, svc.got)
}
