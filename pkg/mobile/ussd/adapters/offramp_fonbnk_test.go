package adapters

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"

	"github.com/samber/oops"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	pkgErrors "github.com/Shamba-Records-Limited/microvault/pkg/errors"
	"github.com/Shamba-Records-Limited/microvault/pkg/payment/fonbnk"
	"github.com/Shamba-Records-Limited/microvault/pkg/payment/offramp"
)

func quietAdapterLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func fbCodeOf(t *testing.T, err error) string {
	t.Helper()
	var oopsErr oops.OopsError
	require.True(t, errors.As(err, &oopsErr), "not an oops error: %v", err)
	code, _ := oopsErr.Code().(string)
	return code
}

type fakeFonbnkClient struct {
	order       *fonbnk.Order
	createErr   error
	confirmErr  error
	createCalls int
	fromQuote   int
	confirmed   map[string]string
	cancelled   []string
	getOrder    *fonbnk.Order
	getErr      error
}

func (f *fakeFonbnkClient) CreateOrder(_ context.Context, _ fonbnk.CreateOrderRequest) (*fonbnk.CreateOrderResponse, error) {
	f.createCalls++
	if f.createErr != nil {
		return nil, f.createErr
	}
	return &fonbnk.CreateOrderResponse{Order: *f.order}, nil
}

func (f *fakeFonbnkClient) CreateOrderFromQuote(ctx context.Context, _ *fonbnk.Quote, req fonbnk.CreateOrderRequest) (*fonbnk.CreateOrderResponse, error) {
	f.fromQuote++
	return f.CreateOrder(ctx, req)
}

func (f *fakeFonbnkClient) ConfirmOrder(_ context.Context, orderID string, fields map[string]string) (*fonbnk.Order, error) {
	if f.confirmErr != nil {
		return nil, f.confirmErr
	}
	if f.confirmed == nil {
		f.confirmed = map[string]string{}
	}
	f.confirmed[orderID] = fields[fonbnk.FieldBlockchainTxHash]
	return f.order, nil
}

func (f *fakeFonbnkClient) CancelOrder(_ context.Context, orderID string) (*fonbnk.Order, error) {
	f.cancelled = append(f.cancelled, orderID)
	return f.order, nil
}

func (f *fakeFonbnkClient) GetOrder(_ context.Context, _ string) (*fonbnk.Order, error) {
	return f.getOrder, f.getErr
}

func (f *fakeFonbnkClient) QuoteOffRamp(context.Context, fonbnk.CryptoLeg, fonbnk.FiatLeg, float64) (*fonbnk.Quote, error) {
	return nil, errors.New("not used")
}

type fakeTreasury struct {
	txHash    string
	err       error
	sentTo    string
	sentMemo  string
	sentAmt   int64
	sendCalls int
}

func (f *fakeTreasury) SendUSDC(_ context.Context, destination, memo string, amount int64) (string, error) {
	f.sendCalls++
	if f.err != nil {
		return "", f.err
	}
	f.sentTo, f.sentMemo, f.sentAmt = destination, memo, amount
	return f.txHash, nil
}

func (f *fakeTreasury) CheckUSDCTrustline(context.Context, string) (bool, error) { return true, nil }

func fundedOrder() *fonbnk.Order {
	return &fonbnk.Order{
		ID:     "ord-1",
		Status: fonbnk.StatusDepositAwaiting,
		Deposit: fonbnk.OrderLeg{
			CurrencyCode: "STELLAR_USDC",
			Cashout:      fonbnk.Cashout{TotalChargedFeesUSD: 0.01},
			TransferInstructions: &fonbnk.TransferInstructions{
				Type: fonbnk.TransferTypeManual,
				TransferDetails: []fonbnk.TransferDetail{
					{ID: fonbnk.DetailRecipientWalletAddress, Value: "GFONBNK"},
					{ID: "cryptoTransactionRequestAdditionalData", Value: "memo-42"},
					{ID: fonbnk.DetailAmountToSend, Value: "20"},
				},
			},
		},
		Payout: fonbnk.OrderLeg{
			CurrencyCode: "KES",
			Cashout: fonbnk.Cashout{
				AmountAfterFees: 2468, Rate: 124.9098, TotalChargedFees: 30,
			},
		},
	}
}

func fonbnkAdapter(t *testing.T, client FonbnkClient, treasury offramp.TreasuryTransfer) *FonbnkOffRampAdapter {
	t.Helper()
	a, err := NewFonbnkOffRampAdapter(FonbnkOffRampConfig{
		Client:             client,
		Treasury:           treasury,
		CryptoCurrencyCode: "STELLAR_USDC",
		TreasuryAddress:    "GTREASURY",
		Logger:             quietAdapterLogger(),
	})
	require.NoError(t, err)
	return a
}

func fonbnkRequest() offramp.Request {
	return offramp.Request{
		LoanID:           "loan-1",
		UserID:           "user-1",
		AmountStroops:    200_000_000,
		DestinationPhone: "0712345678",
		CountryCode:      "KE",
		IdempotencyKey:   "loan-1",
		Options: fonbnk.Options{
			UserEmail:   "u@example.com",
			UserIP:      "197.232.1.1",
			CarrierCode: "ke_safaricom",
		},
	}
}

func TestFonbnkInitiate_HappyPath(t *testing.T) {
	client := &fakeFonbnkClient{order: fundedOrder()}
	treasury := &fakeTreasury{txHash: "stellar-tx"}

	got, err := fonbnkAdapter(t, client, treasury).Initiate(context.Background(), fonbnkRequest())
	require.NoError(t, err)

	assert.Equal(t, "GFONBNK", treasury.sentTo, "USDC goes to the address on the order")
	assert.Equal(t, "memo-42", treasury.sentMemo)
	assert.Equal(t, int64(200_000_000), treasury.sentAmt)
	assert.Equal(t, "stellar-tx", client.confirmed["ord-1"], "the confirm carries the on-chain hash")

	assert.Equal(t, "ord-1", got.RequestID)
	assert.Equal(t, 20.0, got.AmountUSD)
	assert.Equal(t, 2468.0, got.AmountLocal)
	assert.Equal(t, "KES", got.LocalCurrency)

	payload, ok := got.Provider.(fonbnk.OffRampPayload)
	require.True(t, ok)
	assert.True(t, payload.Confirmed)
	assert.Equal(t, "stellar-tx", payload.StellarTxHash)
}

// The order is opened before any funds move, so a refused order must leave the
// treasury untouched.
func TestFonbnkInitiate_OrderRefusedSendsNothing(t *testing.T) {
	client := &fakeFonbnkClient{order: fundedOrder(), createErr: errors.New("403")}
	treasury := &fakeTreasury{txHash: "stellar-tx"}

	_, err := fonbnkAdapter(t, client, treasury).Initiate(context.Background(), fonbnkRequest())
	require.Error(t, err)
	assert.Zero(t, treasury.sendCalls, "no USDC may leave before an order exists")
}

// A failed send leaves an unfunded order, which is cancelled — but the send
// failure is what the caller must see.
func TestFonbnkInitiate_SendFailureCancelsTheOrder(t *testing.T) {
	client := &fakeFonbnkClient{order: fundedOrder()}
	treasury := &fakeTreasury{err: errors.New("underfunded")}

	_, err := fonbnkAdapter(t, client, treasury).Initiate(context.Background(), fonbnkRequest())
	require.Error(t, err)
	assert.Equal(t, pkgErrors.CodeSubmitFailed, fbCodeOf(t, err))
	assert.Equal(t, []string{"ord-1"}, client.cancelled)
}

// Once the USDC is gone the disbursement has happened. Failing here would
// discard the order reference for a step Fonbnk performs on its own anyway.
func TestFonbnkInitiate_ConfirmFailureStillSucceeds(t *testing.T) {
	client := &fakeFonbnkClient{order: fundedOrder(), confirmErr: errors.New("timeout")}
	treasury := &fakeTreasury{txHash: "stellar-tx"}

	got, err := fonbnkAdapter(t, client, treasury).Initiate(context.Background(), fonbnkRequest())
	require.NoError(t, err)

	payload := got.Provider.(fonbnk.OffRampPayload)
	assert.False(t, payload.Confirmed, "the unconfirmed deposit is flagged, not hidden")
	assert.Equal(t, "stellar-tx", payload.StellarTxHash)
	assert.Empty(t, client.cancelled, "a funded order must never be cancelled")
}

// Without an address there is nowhere to send, so nothing may be sent.
func TestFonbnkInitiate_NoDepositAddress(t *testing.T) {
	order := fundedOrder()
	order.Deposit.TransferInstructions.TransferDetails = []fonbnk.TransferDetail{
		{ID: fonbnk.DetailAmountToSend, Value: "20"},
	}
	client := &fakeFonbnkClient{order: order}
	treasury := &fakeTreasury{txHash: "stellar-tx"}

	_, err := fonbnkAdapter(t, client, treasury).Initiate(context.Background(), fonbnkRequest())
	require.Error(t, err)
	assert.Equal(t, pkgErrors.CodeIncompleteResponse, fbCodeOf(t, err))
	assert.Zero(t, treasury.sendCalls)
	assert.Equal(t, []string{"ord-1"}, client.cancelled)
}

func TestFonbnkInitiate_UsesLockedQuoteWhenSupplied(t *testing.T) {
	client := &fakeFonbnkClient{order: fundedOrder()}
	req := fonbnkRequest()
	opts := req.Options.(fonbnk.Options)
	opts.Quote = &fonbnk.Quote{QuoteID: "q1"}
	req.Options = opts

	_, err := fonbnkAdapter(t, client, &fakeTreasury{txHash: "tx"}).Initiate(context.Background(), req)
	require.NoError(t, err)
	assert.Equal(t, 1, client.fromQuote, "a relay-supplied quote must be honoured")
}

func TestFonbnkInitiate_Validation(t *testing.T) {
	tests := map[string]func(*offramp.Request){
		"no options":       func(r *offramp.Request) { r.Options = nil },
		"wrong options":    func(r *offramp.Request) { r.Options = wrongOptions{} },
		"zero amount":      func(r *offramp.Request) { r.AmountStroops = 0 },
		"negative amount":  func(r *offramp.Request) { r.AmountStroops = -1 },
		"no user identity": func(r *offramp.Request) { r.Options = fonbnk.Options{UserEmail: "u@example.com"} },
	}

	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			client := &fakeFonbnkClient{order: fundedOrder()}
			treasury := &fakeTreasury{txHash: "tx"}
			req := fonbnkRequest()
			mutate(&req)

			_, err := fonbnkAdapter(t, client, treasury).Initiate(context.Background(), req)
			require.Error(t, err)
			assert.Zero(t, client.createCalls)
			assert.Zero(t, treasury.sendCalls)
		})
	}
}

type wrongOptions struct{}

func (wrongOptions) ProviderID() offramp.ProviderID { return offramp.ProviderYellowCard }

func TestFonbnkStatus(t *testing.T) {
	order := fundedOrder()
	order.Status = fonbnk.StatusPayoutSuccessful
	order.MerchantOrderParams = "loan-1"

	client := &fakeFonbnkClient{order: order, getOrder: order}
	got, err := fonbnkAdapter(t, client, &fakeTreasury{}).
		Status(context.Background(), offramp.ProviderRef{ID: "ord-1"})
	require.NoError(t, err)

	assert.Equal(t, fonbnk.StatusPayoutSuccessful, got.Status)
	assert.Equal(t, "loan-1", got.SequenceID)
	assert.Equal(t, 2468.0, got.AmountLocal)
	require.NotNil(t, got.CompletedAt, "a terminal order carries a completion time")
	assert.Nil(t, got.FailureReason)
}

// deposit_expired is not terminal at Fonbnk, so it must not be reported as
// completed.
func TestFonbnkStatus_NonTerminalHasNoCompletion(t *testing.T) {
	order := fundedOrder()
	order.Status = fonbnk.StatusDepositExpired

	client := &fakeFonbnkClient{order: order, getOrder: order}
	got, err := fonbnkAdapter(t, client, &fakeTreasury{}).
		Status(context.Background(), offramp.ProviderRef{ID: "ord-1"})
	require.NoError(t, err)
	assert.Nil(t, got.CompletedAt)
}

func TestFonbnkStatus_FailureReason(t *testing.T) {
	order := fundedOrder()
	order.Status = fonbnk.StatusPayoutFailed

	client := &fakeFonbnkClient{order: order, getOrder: order}
	got, err := fonbnkAdapter(t, client, &fakeTreasury{}).
		Status(context.Background(), offramp.ProviderRef{ID: "ord-1"})
	require.NoError(t, err)
	require.NotNil(t, got.FailureReason)
	assert.Equal(t, fonbnk.StatusPayoutFailed, *got.FailureReason)
}

func TestNewFonbnkOffRampAdapter_Validation(t *testing.T) {
	base := FonbnkOffRampConfig{
		Client:             &fakeFonbnkClient{},
		Treasury:           &fakeTreasury{},
		CryptoCurrencyCode: "STELLAR_USDC",
	}

	tests := map[string]func(*FonbnkOffRampConfig){
		"no client":   func(c *FonbnkOffRampConfig) { c.Client = nil },
		"no treasury": func(c *FonbnkOffRampConfig) { c.Treasury = nil },
		"no crypto":   func(c *FonbnkOffRampConfig) { c.CryptoCurrencyCode = "" },
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			cfg := base
			mutate(&cfg)
			_, err := NewFonbnkOffRampAdapter(cfg)
			require.Error(t, err)
		})
	}
}
