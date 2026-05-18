package adapters

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Shamba-Records-Limited/microvault/pkg/payment/offramp"
)

// fakeOffRamp is a minimal offramp.Service stub that records which methods
// were called and lets each test pre-set return values per call.
type fakeOffRamp struct {
	id string

	initiateCalls int
	initiateResp  *offramp.Result
	initiateErr   error

	statusCalls int
	statusResp  *offramp.Status
	statusErr   error

	providersResp []offramp.ProviderInfo
	providersErr  error

	exchangeRateResp *offramp.ExchangeRate
	exchangeRateErr  error

	mobileMoneyNetworksResp []offramp.MobileMoneyNetwork
	mobileMoneyNetworksErr  error

	balanceResp float64
	balanceErr  error
}

func (f *fakeOffRamp) InitiateOffRamp(_ context.Context, _ offramp.Request) (*offramp.Result, error) {
	f.initiateCalls++
	if f.initiateResp == nil && f.initiateErr == nil {
		return &offramp.Result{RequestID: f.id}, nil
	}
	return f.initiateResp, f.initiateErr
}

func (f *fakeOffRamp) GetOffRampStatus(_ context.Context, _ string) (*offramp.Status, error) {
	f.statusCalls++
	return f.statusResp, f.statusErr
}

func (f *fakeOffRamp) GetSupportedProviders(_ context.Context, _ string) ([]offramp.ProviderInfo, error) {
	return f.providersResp, f.providersErr
}

func (f *fakeOffRamp) GetExchangeRate(_ context.Context, _ string) (*offramp.ExchangeRate, error) {
	return f.exchangeRateResp, f.exchangeRateErr
}

func (f *fakeOffRamp) GetMobileMoneyNetworks(_ context.Context, _ string) ([]offramp.MobileMoneyNetwork, error) {
	return f.mobileMoneyNetworksResp, f.mobileMoneyNetworksErr
}

func (f *fakeOffRamp) GetAvailableBalance(_ context.Context) (float64, error) {
	return f.balanceResp, f.balanceErr
}

func TestRouter_RejectsBothNil(t *testing.T) {
	_, err := NewRoutingOffRampService(nil, nil)
	require.Error(t, err)
}

func TestRouter_DispatchesByPayoutMethod(t *testing.T) {
	mm := &fakeOffRamp{id: "yc-1"}
	cp := &fakeOffRamp{id: "mg-1"}

	r, err := NewRoutingOffRampService(mm, cp)
	require.NoError(t, err)

	tests := []struct {
		name           string
		method         string
		wantMobileHits int
		wantCashHits   int
		wantID         string
	}{
		{"empty defaults to mobile money", "", 1, 0, "yc-1"},
		{"explicit mobile money", offramp.PayoutMethodMobileMoney, 1, 0, "yc-1"},
		{"cash pickup", offramp.PayoutMethodCashPickup, 0, 1, "mg-1"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			mm.initiateCalls = 0
			cp.initiateCalls = 0

			res, err := r.InitiateOffRamp(context.Background(), offramp.Request{
				PayoutMethod: tc.method,
				LoanID:       "L1",
				AmountUSD:    50,
			})
			require.NoError(t, err)
			assert.Equal(t, tc.wantID, res.RequestID)
			assert.Equal(t, tc.wantMobileHits, mm.initiateCalls)
			assert.Equal(t, tc.wantCashHits, cp.initiateCalls)
		})
	}
}

func TestRouter_RejectsUnknownPayoutMethod(t *testing.T) {
	r, err := NewRoutingOffRampService(&fakeOffRamp{}, &fakeOffRamp{})
	require.NoError(t, err)

	_, err = r.InitiateOffRamp(context.Background(), offramp.Request{
		PayoutMethod: "skywriting",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown payout method")
}

func TestRouter_RejectsMethodWhenProviderMissing(t *testing.T) {
	// Cash pickup configured, no mobile money — empty/MM requests should fail clearly.
	r, err := NewRoutingOffRampService(nil, &fakeOffRamp{})
	require.NoError(t, err)

	_, err = r.InitiateOffRamp(context.Background(), offramp.Request{
		PayoutMethod: offramp.PayoutMethodMobileMoney,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "mobile money provider not configured")

	_, err = r.InitiateOffRamp(context.Background(), offramp.Request{}) // empty defaults to MM
	require.Error(t, err)
}

func TestRouter_GetSupportedProviders_ConcatenatesBoth(t *testing.T) {
	mm := &fakeOffRamp{
		providersResp: []offramp.ProviderInfo{{ID: "yc-momo"}},
	}
	cp := &fakeOffRamp{
		providersResp: []offramp.ProviderInfo{{ID: "mg-cash"}},
	}
	r, err := NewRoutingOffRampService(mm, cp)
	require.NoError(t, err)

	got, err := r.GetSupportedProviders(context.Background(), "KE")
	require.NoError(t, err)
	require.Len(t, got, 2)
	assert.Equal(t, "yc-momo", got[0].ID)
	assert.Equal(t, "mg-cash", got[1].ID)
}

func TestRouter_GetSupportedProviders_PropagatesError(t *testing.T) {
	mm := &fakeOffRamp{providersErr: errors.New("boom")}
	cp := &fakeOffRamp{providersResp: []offramp.ProviderInfo{{ID: "mg-cash"}}}
	r, err := NewRoutingOffRampService(mm, cp)
	require.NoError(t, err)

	_, err = r.GetSupportedProviders(context.Background(), "KE")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "mobile money providers")
}

func TestRouter_MoMoNetworks_DelegatesToMobileMoney(t *testing.T) {
	mm := &fakeOffRamp{
		mobileMoneyNetworksResp: []offramp.MobileMoneyNetwork{{ID: "n1"}, {ID: "n2"}},
	}
	cp := &fakeOffRamp{} // never consulted
	r, err := NewRoutingOffRampService(mm, cp)
	require.NoError(t, err)

	got, err := r.GetMobileMoneyNetworks(context.Background(), "KE")
	require.NoError(t, err)
	assert.Len(t, got, 2)
}

func TestRouter_MoMoNetworks_EmptyWhenNoMobileMoney(t *testing.T) {
	r, err := NewRoutingOffRampService(nil, &fakeOffRamp{})
	require.NoError(t, err)

	got, err := r.GetMobileMoneyNetworks(context.Background(), "KE")
	require.NoError(t, err)
	assert.Empty(t, got)
}

func TestRouter_GetAvailableBalance_ReturnsZeroWhenNoMobileMoney(t *testing.T) {
	r, err := NewRoutingOffRampService(nil, &fakeOffRamp{balanceResp: 99}) // cash pickup balance ignored
	require.NoError(t, err)

	got, err := r.GetAvailableBalance(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 0.0, got)
}
