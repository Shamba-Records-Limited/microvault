package adapters

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/govalues/decimal"
	"github.com/samber/oops"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	pkgErrors "github.com/Shamba-Records-Limited/microvault/pkg/errors"
	"github.com/Shamba-Records-Limited/microvault/pkg/payment/chipper"
	"github.com/Shamba-Records-Limited/microvault/pkg/payment/offramp"
)

const (
	chipperTestAddress = "GBBD47IF6LWK7P7MDEVSCWR7DPUWV3NY3DTQEVFL4NAT4AQH3ZLLFLA5"
	chipperTestMemo    = "2802699348"
)

func chipperCodeOf(t *testing.T, err error) string {
	t.Helper()
	var oopsErr oops.OopsError
	require.True(t, errors.As(err, &oopsErr), "not an oops error: %v", err)
	code, _ := oopsErr.Code().(string)
	return code
}

type fakeChipperClient struct {
	address, tag  string
	addressCalls  int
	caps          *chipper.Capabilities
	capsCalls     int
	existing      *chipper.Payout
	payout        *chipper.Payout
	createErr     error
	created       []chipper.PayoutRequest
	usdcAvailable string
	deposit       *chipper.Collection
	depositAfter  int
	depositCalls  int
	rates         map[string]string
}

func (f *fakeChipperClient) CreatePayout(_ context.Context, req chipper.PayoutRequest) (*chipper.Payout, bool, error) {
	f.created = append(f.created, req)
	if f.createErr != nil {
		return nil, false, f.createErr
	}
	return f.payout, false, nil
}

func (f *fakeChipperClient) GetPayout(_ context.Context, id string) (*chipper.Payout, error) {
	if f.payout != nil && f.payout.ID == id {
		return f.payout, nil
	}
	return nil, &chipper.APIError{StatusCode: 404, Code: chipper.CodeNotFound}
}

func (f *fakeChipperClient) GetPayoutByReference(_ context.Context, _ string) (*chipper.Payout, error) {
	if f.existing != nil {
		return f.existing, nil
	}
	return nil, &chipper.APIError{StatusCode: 404, Code: chipper.CodeNotFound}
}

func (f *fakeChipperClient) GetRate(_ context.Context, origin, destination string) (*chipper.Rate, error) {
	r, ok := f.rates[origin+"/"+destination]
	if !ok {
		return nil, &chipper.APIError{StatusCode: 503, Code: chipper.CodeRateUnavailable}
	}
	return &chipper.Rate{From: origin, To: destination, Rate: decimal.MustParse(r)}, nil
}

func (f *fakeChipperClient) GetCountryCapabilities(context.Context, string) (*chipper.Capabilities, error) {
	f.capsCalls++
	return f.caps, nil
}

func (f *fakeChipperClient) ListAccounts(context.Context) ([]chipper.Account, error) {
	return []chipper.Account{{Currency: "USDC", AvailableBalance: decimal.MustParse(f.usdcAvailable)}}, nil
}

func (f *fakeChipperClient) CreateCryptoAddress(context.Context, chipper.CryptoAddressRequest) (*chipper.CryptoAddress, error) {
	f.addressCalls++
	return &chipper.CryptoAddress{ID: "addr_1", Address: &f.address, Tag: &f.tag}, nil
}

func (f *fakeChipperClient) FindCryptoDeposit(_ context.Context, addressID, _ string, _ time.Time) (*chipper.Collection, bool, error) {
	f.depositCalls++
	if addressID != "addr_1" {
		return nil, false, errors.New("wrong address id")
	}
	if f.deposit == nil || f.depositCalls <= f.depositAfter {
		return nil, false, nil
	}
	return f.deposit, true, nil
}

func kenyaCaps(status string) *chipper.Capabilities {
	cc := chipper.CountryCapability{Methods: []chipper.PaymentMethod{
		{Code: "ke_mpesa", Name: "M-PESA", Type: "mobile_money", Status: status},
		{Code: "ke_kcb", Name: "KCB", Type: "bank_transfer", Status: chipper.MethodOperational},
	}}
	cc.Country.Code, cc.Currency.Code = "KE", "KES"
	return &chipper.Capabilities{Payouts: []chipper.CountryCapability{cc}}
}

func completedPayout() *chipper.Payout {
	rate := decimal.MustParse("130.226805")
	return &chipper.Payout{
		ID:                "pay_kes_1",
		Status:            chipper.PayoutPending,
		From:              chipper.PayoutParty{Amount: decimal.MustParse("7.678911"), Currency: "USDC"},
		To:                chipper.PayoutParty{Amount: decimal.MustParse("1000.00"), Currency: "KES"},
		Fee:               &chipper.Money{Amount: decimal.MustParse("0.115184"), Currency: "USDC"},
		Rate:              &rate,
		ExternalReference: "loan-1",
	}
}

func newFakeChipper() *fakeChipperClient {
	return &fakeChipperClient{
		address:       chipperTestAddress,
		tag:           chipperTestMemo,
		caps:          kenyaCaps(chipper.MethodOperational),
		payout:        completedPayout(),
		usdcAvailable: "100",
		deposit:       &chipper.Collection{ID: "col_1", Type: chipper.CollectionTypeCrypto, Status: chipper.CollectionCompleted},
		rates:         map[string]string{"USDC/KES": "130.226805", "KES/USDC": "0.0075"},
	}
}

func chipperAdapter(t *testing.T, client ChipperClient, treasury offramp.TreasuryTransfer, alertSvc *fakeAlertService) *ChipperOffRampAdapter {
	t.Helper()
	cfg := ChipperOffRampConfig{
		Client:         client,
		Treasury:       treasury,
		DepositAddress: chipperTestAddress,
		DepositMemo:    chipperTestMemo,
		MethodCodes:    map[string]string{"63902": "ke_mpesa"},
		CreditTimeout:  50 * time.Millisecond,
		PollInterval:   time.Millisecond,
	}
	if alertSvc != nil {
		cfg.Alerts = alertSvc
	}
	a, err := NewChipperOffRampAdapter(cfg)
	require.NoError(t, err)
	return a
}

func chipperRequest() offramp.Request {
	return offramp.Request{
		LoanID:           "loan-1",
		RecipientName:    "Jane Doe",
		AmountStroops:    76_789_115,
		DestinationPhone: "+254 712 345 678",
		CountryCode:      "KE",
		NetworkCode:      "63902",
		IdempotencyKey:   "loan-1",
		PayoutMethod:     offramp.PayoutMethodMobileMoney,
	}
}

func TestNewChipperOffRampAdapterValidation(t *testing.T) {
	base := ChipperOffRampConfig{
		Client: newFakeChipper(), Treasury: &fakeTreasury{},
		DepositAddress: chipperTestAddress, DepositMemo: chipperTestMemo,
		MethodCodes: map[string]string{"63902": "ke_mpesa"},
	}
	tests := []struct {
		name   string
		mutate func(*ChipperOffRampConfig)
		code   string
	}{
		{"bad address", func(c *ChipperOffRampConfig) { c.DepositAddress = "GNOTANADDRESS" }, pkgErrors.CodeInvalidAddress},
		{"text memo", func(c *ChipperOffRampConfig) { c.DepositMemo = "loan-memo" }, pkgErrors.CodeInvalidMemo},
		{"empty memo", func(c *ChipperOffRampConfig) { c.DepositMemo = "" }, pkgErrors.CodeInvalidMemo},
		{"no method codes", func(c *ChipperOffRampConfig) { c.MethodCodes = nil }, pkgErrors.CodeMissingDependency},
		{"negative headroom", func(c *ChipperOffRampConfig) { c.FeeHeadroomPct = -1 }, pkgErrors.CodeInvalidConfig},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := base
			tt.mutate(&cfg)
			_, err := NewChipperOffRampAdapter(cfg)
			require.Error(t, err)
			assert.Equal(t, tt.code, chipperCodeOf(t, err))
		})
	}
}

func TestChipperVerifyDepositTarget(t *testing.T) {
	client := newFakeChipper()
	a := chipperAdapter(t, client, &fakeTreasury{}, nil)

	require.NoError(t, a.VerifyDepositTarget(context.Background()))
	require.NoError(t, a.VerifyDepositTarget(context.Background()))
	assert.Equal(t, 1, client.addressCalls, "a verified target is cached")

	client = newFakeChipper()
	client.tag = "999"
	err := chipperAdapter(t, client, &fakeTreasury{}, nil).VerifyDepositTarget(context.Background())
	require.Error(t, err)
	assert.Equal(t, pkgErrors.CodeInvalidAddress, chipperCodeOf(t, err))
}

func TestChipperInitiateDirect(t *testing.T) {
	client := newFakeChipper()
	client.depositAfter = 2
	treasury := &fakeTreasury{txHash: "stellar-tx"}

	res, err := chipperAdapter(t, client, treasury, nil).Initiate(context.Background(), chipperRequest())
	require.NoError(t, err)

	assert.Equal(t, chipperTestAddress, treasury.sentTo)
	assert.Equal(t, offramp.Memo{Value: chipperTestMemo, Type: offramp.MemoTypeID}, treasury.sentMemo)
	assert.Equal(t, int64(76_789_115), treasury.sentAmt, "the treasury sends the full net payout")
	assert.Equal(t, 3, client.depositCalls, "polls until the deposit is credited")

	require.Len(t, client.created, 1)
	req := client.created[0]
	assert.Equal(t, "7.678911", req.From.Amount.String(), "stroops are truncated to usdc's 6 decimals")
	assert.Equal(t, "USDC", req.From.Currency)
	assert.Nil(t, req.To.Amount)
	assert.Equal(t, "ke_mpesa", req.To.Code)
	assert.Equal(t, "254712345678", req.To.AccountNumber)
	assert.Equal(t, "KES", req.To.Currency)
	assert.Equal(t, "Jane Doe", req.To.Recipient.Name)
	assert.Equal(t, "loan-1", req.ExternalReference)

	assert.Equal(t, "pay_kes_1", res.RequestID)
	assert.Equal(t, "loan-1", res.SequenceID)
	assert.Equal(t, chipper.SettlementDirect, res.SettlementMethod)
	assert.InDelta(t, 1000.0, res.AmountLocal, 1e-9)
	assert.InDelta(t, 0.115184, res.Fee, 1e-9)
	assert.InDelta(t, 15.0, res.FeeLocal, 0.01)
	payload := res.Provider.(chipper.OffRampPayload)
	assert.Equal(t, "stellar-tx", payload.DepositTxHash)
	assert.Equal(t, "col_1", payload.DepositCollectionID)
	assert.False(t, payload.PayoutPending)
}

func TestChipperInitiateReturnsExistingPayout(t *testing.T) {
	client := newFakeChipper()
	client.existing = completedPayout()
	treasury := &fakeTreasury{txHash: "stellar-tx"}

	res, err := chipperAdapter(t, client, treasury, nil).Initiate(context.Background(), chipperRequest())
	require.NoError(t, err)
	assert.Equal(t, "pay_kes_1", res.RequestID)
	assert.Zero(t, treasury.sendCalls, "no second deposit for a reference that already paid out")
	assert.Empty(t, client.created)
}

func TestChipperInitiateSendFailure(t *testing.T) {
	t.Run("falls back to the balance", func(t *testing.T) {
		client := newFakeChipper()
		res, err := chipperAdapter(t, client, &fakeTreasury{err: errors.New("horizon down")}, nil).
			Initiate(context.Background(), chipperRequest())
		require.NoError(t, err)
		assert.Equal(t, chipper.SettlementFiat, res.SettlementMethod, "fiat tells the caller the usdc is still in treasury")
		require.Len(t, client.created, 1)
	})

	t.Run("fails when the balance is short", func(t *testing.T) {
		client := newFakeChipper()
		client.usdcAvailable = "7.7"
		_, err := chipperAdapter(t, client, &fakeTreasury{err: errors.New("horizon down")}, nil).
			Initiate(context.Background(), chipperRequest())
		require.Error(t, err)
		assert.Equal(t, pkgErrors.CodeSubmitFailed, chipperCodeOf(t, err))
		assert.Empty(t, client.created)
	})
}

func TestChipperInitiateDepositNotCredited(t *testing.T) {
	client := newFakeChipper()
	client.deposit = nil
	alertSvc := &fakeAlertService{}

	res, err := chipperAdapter(t, client, &fakeTreasury{txHash: "stellar-tx"}, alertSvc).
		Initiate(context.Background(), chipperRequest())
	require.NoError(t, err, "usdc left the treasury, so the result must not be an error")
	assert.Equal(t, chipperStatusPending, res.Status)
	assert.Equal(t, "stellar-tx", res.RequestID)
	payload := res.Provider.(chipper.OffRampPayload)
	assert.True(t, payload.PayoutPending)
	assert.Equal(t, "deposit_not_credited", payload.PendingReason)
	assert.Empty(t, client.created)
	assert.Equal(t, []string{"Chipper deposit not credited"}, alertSvc.subjects)
}

func TestChipperInitiateDepositFailed(t *testing.T) {
	client := newFakeChipper()
	client.deposit.Status = chipper.CollectionFailed

	res, err := chipperAdapter(t, client, &fakeTreasury{txHash: "stellar-tx"}, &fakeAlertService{}).
		Initiate(context.Background(), chipperRequest())
	require.NoError(t, err)
	assert.Equal(t, "deposit_failed", res.Provider.(chipper.OffRampPayload).PendingReason)
	assert.Empty(t, client.created)
}

func TestChipperInitiatePayoutRejectedAfterCredit(t *testing.T) {
	client := newFakeChipper()
	client.createErr = &chipper.APIError{StatusCode: 400, Code: chipper.CodeInsufficientFunds}
	alertSvc := &fakeAlertService{}

	res, err := chipperAdapter(t, client, &fakeTreasury{txHash: "stellar-tx"}, alertSvc).
		Initiate(context.Background(), chipperRequest())
	require.NoError(t, err)
	payload := res.Provider.(chipper.OffRampPayload)
	assert.True(t, payload.PayoutPending)
	assert.Equal(t, chipper.CodeInsufficientFunds, payload.PendingReason)
	assert.Equal(t, "col_1", payload.DepositCollectionID)
	assert.Equal(t, []string{"Chipper payout not created"}, alertSvc.subjects)
}

func TestChipperCreatePayoutAmbiguousFailure(t *testing.T) {
	client := newFakeChipper()
	client.createErr = &chipper.APIError{StatusCode: http.StatusBadGateway}
	a := chipperAdapter(t, client, &fakeTreasury{txHash: "stellar-tx"}, nil)
	d, err := a.prepare(context.Background(), chipperRequest())
	require.NoError(t, err)

	client.existing = completedPayout()
	p, err := a.createPayout(context.Background(), d)
	require.NoError(t, err, "a payout found by reference after a 5xx counts as created")
	assert.Equal(t, "pay_kes_1", p.ID)

	client.existing = nil
	client.createErr = &chipper.APIError{StatusCode: 422, Code: chipper.CodeInvalidRecipient}
	_, err = a.createPayout(context.Background(), d)
	require.Error(t, err)
}

func TestChipperInitiateFiat(t *testing.T) {
	req := chipperRequest()
	req.Options = chipper.Options{SettlementMethod: chipper.SettlementFiat}

	client := newFakeChipper()
	treasury := &fakeTreasury{}
	res, err := chipperAdapter(t, client, treasury, nil).Initiate(context.Background(), req)
	require.NoError(t, err)
	assert.Equal(t, chipper.SettlementFiat, res.SettlementMethod)
	assert.Zero(t, treasury.sendCalls)

	client = newFakeChipper()
	client.usdcAvailable = "7.8"
	_, err = chipperAdapter(t, client, &fakeTreasury{}, nil).Initiate(context.Background(), req)
	require.Error(t, err)
	assert.Equal(t, pkgErrors.CodeInsufficientLiquidity, chipperCodeOf(t, err), "7.8 is below 7.678911 plus 2% headroom")
}

func TestChipperInitiateRejects(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*offramp.Request, *fakeChipperClient)
		code   string
	}{
		{"unmapped network", func(r *offramp.Request, _ *fakeChipperClient) { r.NetworkCode = "63903" }, pkgErrors.CodeCorridorUnavailable},
		{"method in outage", func(_ *offramp.Request, c *fakeChipperClient) { c.caps = kenyaCaps(chipper.MethodOutage) }, pkgErrors.CodeCorridorUnavailable},
		{"unsupported country", func(r *offramp.Request, _ *fakeChipperClient) { r.CountryCode = "ZA" }, pkgErrors.CodeCorridorUnavailable},
		{"national phone", func(r *offramp.Request, _ *fakeChipperClient) { r.DestinationPhone = "0712345678" }, pkgErrors.CodeMissingPhoneNumber},
		{"zero amount", func(r *offramp.Request, _ *fakeChipperClient) { r.AmountStroops = 0 }, pkgErrors.CodeInvalidAmount},
		{"dust amount", func(r *offramp.Request, _ *fakeChipperClient) { r.AmountStroops = 9 }, pkgErrors.CodeInvalidAmount},
		{"foreign options", func(r *offramp.Request, _ *fakeChipperClient) {
			r.Options = chipper.OffRampPayload{}
		}, pkgErrors.CodeInvalidConfig},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := newFakeChipper()
			req := chipperRequest()
			tt.mutate(&req, client)
			treasury := &fakeTreasury{txHash: "stellar-tx"}
			_, err := chipperAdapter(t, client, treasury, nil).Initiate(context.Background(), req)
			require.Error(t, err)
			assert.Equal(t, tt.code, chipperCodeOf(t, err))
			assert.Zero(t, treasury.sendCalls)
		})
	}
}

func chipperRelease() chipper.Release {
	return chipper.Release{
		LoanID: "loan-1", Reference: "loan-1", RecipientName: "Jane Doe",
		DestinationPhone: "+254712345678", CountryCode: "KE", NetworkCode: "63902",
		DepositTxHash: "stellar-tx",
	}
}

func TestChipperReleasePayout(t *testing.T) {
	client := newFakeChipper()
	client.deposit.From = chipper.CollectionParty{Amount: decimal.MustParse("7.678911"), Currency: "USDC"}
	res, err := chipperAdapter(t, client, &fakeTreasury{}, nil).ReleasePayout(context.Background(), chipperRelease())
	require.NoError(t, err)
	assert.Equal(t, "pay_kes_1", res.RequestID)
	require.Len(t, client.created, 1)
	assert.Equal(t, "7.678911", client.created[0].From.Amount.String(), "the payout uses the deposited amount")

	client = newFakeChipper()
	client.deposit.Status = chipper.CollectionProcessing
	_, err = chipperAdapter(t, client, &fakeTreasury{}, nil).ReleasePayout(context.Background(), chipperRelease())
	require.Error(t, err)
	assert.Equal(t, pkgErrors.CodeIncompleteResponse, chipperCodeOf(t, err))
	assert.Empty(t, client.created)
}

func TestChipperReissuePayout(t *testing.T) {
	failed := completedPayout()
	failed.Status = chipper.PayoutFailed
	failed.To.Code, failed.To.AccountNumber = new("ke_mpesa"), new("254712345678")
	failed.Metadata = map[string]any{"loan_id": "loan-1"}

	client := newFakeChipper()
	p, err := chipperAdapter(t, client, &fakeTreasury{}, nil).ReissuePayout(context.Background(), failed, "loan-1_fiat")
	require.NoError(t, err)
	assert.Equal(t, "pay_kes_1", p.ID)
	require.Len(t, client.created, 1)
	req := client.created[0]
	assert.Equal(t, "loan-1_fiat", req.ExternalReference)
	assert.Equal(t, "7.678911", req.From.Amount.String())
	assert.Equal(t, "ke_mpesa", req.To.Code)
	assert.Equal(t, "loan-1", req.Metadata["loan_id"])

	failed.To.AccountNumber = nil
	_, err = chipperAdapter(t, newFakeChipper(), &fakeTreasury{}, nil).ReissuePayout(context.Background(), failed, "loan-1_fiat")
	require.Error(t, err)
	assert.Equal(t, pkgErrors.CodeIncompleteResponse, chipperCodeOf(t, err))
}

func TestChipperStatus(t *testing.T) {
	client := newFakeChipper()
	client.payout.Status = chipper.PayoutFailed
	client.payout.StatusMessage = new("wallet closed")
	a := chipperAdapter(t, client, &fakeTreasury{}, nil)

	st, err := a.Status(context.Background(), offramp.ProviderRef{ID: "pay_kes_1"})
	require.NoError(t, err)
	assert.Equal(t, chipper.PayoutFailed, st.Status)
	assert.Equal(t, "wallet closed", *st.FailureReason)

	client.existing = completedPayout()
	st, err = a.Status(context.Background(), offramp.ProviderRef{ID: "loan-1"})
	require.NoError(t, err)
	assert.Equal(t, "loan-1", st.SequenceID)
	assert.Nil(t, st.FailureReason)
}

func TestChipperQuoteAndDirectory(t *testing.T) {
	client := newFakeChipper()
	a := chipperAdapter(t, client, &fakeTreasury{}, nil)

	rate, err := a.Quote(context.Background(), offramp.QuoteRequest{Currency: "KES"})
	require.NoError(t, err)
	assert.InDelta(t, 130.226805, rate.SellRate, 1e-9)
	assert.InDelta(t, 133.333333, rate.BuyRate, 1e-5)

	delete(client.rates, "USDC/KES")
	_, err = a.Quote(context.Background(), offramp.QuoteRequest{Currency: "KES"})
	require.Error(t, err)
	assert.Equal(t, pkgErrors.CodeRateUnavailable, chipperCodeOf(t, err))

	networks, err := a.Networks(context.Background(), "KE")
	require.NoError(t, err)
	require.Len(t, networks, 1, "only usable mobile money methods")
	assert.Equal(t, "ke_mpesa", networks[0].Code)

	providers, err := a.SupportedProviders(context.Background(), "KE")
	require.NoError(t, err)
	require.Len(t, providers, 1)
	assert.Equal(t, "KES", providers[0].Currency)
	assert.Equal(t, 1, client.capsCalls, "capabilities are cached per country")

	balance, err := a.AvailableBalance(context.Background())
	require.NoError(t, err)
	assert.InDelta(t, 100.0, balance, 1e-9)
}
