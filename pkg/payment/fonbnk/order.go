package fonbnk

import (
	"context"
	"net/http"
	"net/url"
	"time"

	pkgErrors "github.com/Shamba-Records-Limited/microvault/pkg/errors"
)

// CreateOrder opens an order. Requires the create-users permission on the
// merchant account; without it every call returns CodeMerchantNotPermitted.
func (a *FonbnkAdapter) CreateOrder(ctx context.Context, req CreateOrderRequest) (*CreateOrderResponse, error) {
	errb := withDirection(
		fonbnkErr("create_order").
			With(pkgErrors.AttrQuoteID, req.QuoteID).
			With("order_params", req.OrderParams),
		req.Deposit.CurrencyType, req.Payout.CurrencyType)

	if err := exactlyOneAmount(errb, req.Deposit.Amount, req.Payout.Amount); err != nil {
		return nil, err
	}
	return call[CreateOrderResponse](ctx, a, errb, http.MethodPost, pathOrder, req)
}

// CreateOrderFromQuote opens an order at a quoted price, copying the legs from
// the quote so they cannot drift from what was priced.
//
// Returns CodeQuoteExpired without calling Fonbnk when the quote window has
// already closed.
func (a *FonbnkAdapter) CreateOrderFromQuote(ctx context.Context, quote *Quote, req CreateOrderRequest) (*CreateOrderResponse, error) {
	if quote == nil {
		return nil, fonbnkErr("create_order_from_quote").
			Code(pkgErrors.CodeMissingDependency).
			Errorf("quote is nil")
	}
	if !quote.QuoteExpiresAt.IsZero() && a.now().After(quote.QuoteExpiresAt) {
		return nil, quoteExpiredErr(quote.QuoteID)
	}

	req.QuoteID = quote.QuoteID
	req.Deposit = legSpecFrom(quote.Deposit, req.Deposit.Amount)
	req.Payout = legSpecFrom(quote.Payout, req.Payout.Amount)
	return a.CreateOrder(ctx, req)
}

// legSpecFrom rebuilds a request leg from a quoted one.
func legSpecFrom(leg QuoteLeg, amount *float64) LegSpec {
	spec := LegSpec{
		PaymentChannel: leg.PaymentChannel,
		CurrencyType:   leg.CurrencyType,
		CurrencyCode:   leg.CurrencyCode,
		CountryIsoCode: leg.CurrencyDetails.CountryIsoCode,
		Amount:         amount,
	}
	if leg.CurrencyDetails.Carrier != nil {
		spec.CarrierCode = leg.CurrencyDetails.Carrier.Code
	}
	return spec
}

// ConfirmOrder reports that the user has paid. Only an order in
// deposit_awaiting can be confirmed; confirming twice is an error.
func (a *FonbnkAdapter) ConfirmOrder(ctx context.Context, orderID string, fields map[string]string) (*Order, error) {
	body := struct {
		OrderID              string            `json:"orderId"`
		FieldsToConfirmOrder map[string]string `json:"fieldsToConfirmOrder,omitempty"`
	}{OrderID: orderID, FieldsToConfirmOrder: fields}

	return call[Order](ctx, a, orderErr("confirm_order", orderID), http.MethodPost, pathOrderConfirm, body)
}

// CancelOrder cancels an order that has not been paid. Only reachable from
// deposit_awaiting, and a late payment can still revive the order.
func (a *FonbnkAdapter) CancelOrder(ctx context.Context, orderID string) (*Order, error) {
	body := struct {
		OrderID string `json:"orderId"`
	}{OrderID: orderID}

	return call[Order](ctx, a, orderErr("cancel_order", orderID), http.MethodPost, pathOrderCancel, body)
}

// TriggerIntermediateAction sends a new STK push, or submits the OTP that
// unlocks one.
func (a *FonbnkAdapter) TriggerIntermediateAction(ctx context.Context, orderID string, fields map[string]string) (*Order, error) {
	body := struct {
		OrderID                     string            `json:"orderId"`
		FieldsForIntermediateAction map[string]string `json:"fieldsForIntermediateAction,omitempty"`
	}{OrderID: orderID, FieldsForIntermediateAction: fields}

	return call[Order](ctx, a, orderErr("trigger_intermediate_action", orderID), http.MethodPost, pathOrderIntermediate, body)
}

// SubmitOTP submits the code that unlocks an otp_stk_push prompt.
func (a *FonbnkAdapter) SubmitOTP(ctx context.Context, orderID, code string) (*Order, error) {
	return a.TriggerIntermediateAction(ctx, orderID, map[string]string{FieldOTPCode: code})
}

// RetrySTKPush sends the user a fresh USSD prompt.
func (a *FonbnkAdapter) RetrySTKPush(ctx context.Context, orderID string) (*Order, error) {
	return a.TriggerIntermediateAction(ctx, orderID, nil)
}

// GetOrder reads one order by Fonbnk's ID.
func (a *FonbnkAdapter) GetOrder(ctx context.Context, orderID string) (*Order, error) {
	endpoint := withQuery(pathOrder, url.Values{"orderId": {orderID}})
	return call[Order](ctx, a, orderErr("get_order", orderID), http.MethodGet, endpoint, nil)
}

// GetOrderByParams reads one order by the orderParams reference set at
// creation. This is the lookup that works before Fonbnk's own ID is persisted.
func (a *FonbnkAdapter) GetOrderByParams(ctx context.Context, orderParams string) (*Order, error) {
	endpoint := withQuery(pathOrder, url.Values{"orderParams": {orderParams}})
	errb := fonbnkErr("get_order_by_params").With("order_params", orderParams)
	return call[Order](ctx, a, errb, http.MethodGet, endpoint, nil)
}

// now is overridable in tests.
func (a *FonbnkAdapter) now() time.Time {
	if a.clock != nil {
		return a.clock()
	}
	return time.Now()
}
