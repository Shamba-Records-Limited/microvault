package chipper

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"time"

	"github.com/govalues/decimal"

	pkgErrors "github.com/Shamba-Records-Limited/microvault/pkg/errors"
)

// Order statuses. Completed, failed and expired are terminal.
const (
	OrderCreated               = "created"
	OrderAwaitingFunds         = "awaiting_funds"
	OrderAwaitingConfirmations = "awaiting_confirmations"
	OrderFundsReceived         = "funds_received"
	OrderOverpaid              = "overpaid"
	OrderUnderpaid             = "underpaid"
	OrderProcessingPayout      = "processing_payout"
	OrderCompleted             = "completed"
	OrderFailed                = "failed"
	OrderExpired               = "expired"
)

// MethodUSDCStellar is the order and payout code for USDC on Stellar.
const MethodUSDCStellar = "usdc_stellar"

const maxOrderNarration = 200

// OrderRequest collects on one rail and pays out on another. Exactly one of
// From.Amount and To.Amount is set. Crypto destinations use To.AccountNumber.
type OrderRequest struct {
	From              OrderLeg `json:"from"`
	To                OrderLeg `json:"to"`
	ExternalReference string   `json:"externalReference"`
	Narration         string   `json:"narration,omitempty"`
}

// OrderLeg is one side of an order request.
type OrderLeg struct {
	Code          string  `json:"code"`
	AccountNumber string  `json:"accountNumber,omitempty"`
	Tag           string  `json:"tag,omitempty"`
	Amount        *Amount `json:"amount,omitempty"`
	Currency      string  `json:"currency"`
}

// OrderInstructions tell the payer how to pay; Message can be shown as is.
type OrderInstructions struct {
	Type     string           `json:"type"`
	Message  string           `json:"message"`
	Amount   *decimal.Decimal `json:"amount"`
	Currency string           `json:"currency"`
	Address  *string          `json:"address,omitempty"`
	Tag      *string          `json:"tag,omitempty"`
}

// OrderParty is one side of an order as returned.
type OrderParty struct {
	Code          string           `json:"code"`
	AccountNumber *string          `json:"accountNumber"`
	Tag           *string          `json:"tag,omitempty"`
	Amount        *decimal.Decimal `json:"amount"`
	Currency      string           `json:"currency"`
}

// Order is an order resource.
type Order struct {
	ID                string             `json:"id"`
	Status            string             `json:"status"`
	From              OrderParty         `json:"from"`
	To                OrderParty         `json:"to"`
	ExternalReference string             `json:"externalReference"`
	Instructions      *OrderInstructions `json:"instructions"`
	ExpectedAmount    *decimal.Decimal   `json:"expectedAmount"`
	ReceivedAmount    *decimal.Decimal   `json:"receivedAmount"`
	Fee               *Money             `json:"fee"`
	Rate              *decimal.Decimal   `json:"rate"`
	CollectionID      *string            `json:"collectionId"`
	PayoutID          *string            `json:"payoutId"`
	StatusMessage     *string            `json:"statusMessage"`
	ExpiresAt         *time.Time         `json:"expiresAt"`
	CreatedAt         time.Time          `json:"createdAt"`
	UpdatedAt         time.Time          `json:"updatedAt"`
	Timeline          []TimelineEvent    `json:"timeline,omitempty"`
}

// IsTerminal reports whether the order has reached a final status.
func (o Order) IsTerminal() bool {
	return o.Status == OrderCompleted || o.Status == OrderFailed || o.Status == OrderExpired
}

type orderEnvelope struct {
	Order Order `json:"order"`
}

// CreateOrder submits an order. replayed is true when Chipper returned an
// existing order for the same externalReference.
func (c *Client) CreateOrder(ctx context.Context, req OrderRequest) (order *Order, replayed bool, err error) {
	errb := chipperErr("create_order").With(pkgErrors.AttrSequenceID, req.ExternalReference)
	if err := validateOrder(req); err != nil {
		return nil, false, err
	}
	out, status, err := call[orderEnvelope](ctx, c, errb, http.MethodPost, "/v1/orders", nil, req)
	if err != nil {
		return nil, false, err
	}
	return &out.Order, status == http.StatusOK, nil
}

// GetOrder reads an order with its merged timeline.
func (c *Client) GetOrder(ctx context.Context, id string) (*Order, error) {
	errb := chipperErr("get_order").With(pkgErrors.AttrOrderID, id)
	if id == "" {
		return nil, errb.Code(pkgErrors.CodeMissingAccount).Errorf("order id is required")
	}
	out, _, err := call[orderEnvelope](ctx, c, errb, http.MethodGet, "/v1/orders/"+url.PathEscape(id), nil, nil)
	if err != nil {
		return nil, err
	}
	return &out.Order, nil
}

// GetOrderByReference reads an order by externalReference.
func (c *Client) GetOrderByReference(ctx context.Context, reference string) (*Order, error) {
	errb := chipperErr("get_order_by_reference").With(pkgErrors.AttrSequenceID, reference)
	if reference == "" {
		return nil, errb.Code(pkgErrors.CodeMissingAccount).Errorf("external reference is required")
	}
	out, _, err := call[orderEnvelope](ctx, c, errb, http.MethodGet, "/v1/orders/by-reference/"+url.PathEscape(reference), nil, nil)
	if err != nil {
		return nil, err
	}
	return &out.Order, nil
}

// Order decodes an event's Data as an order.
func (e Event) Order() (*Order, error) {
	var o Order
	if err := json.Unmarshal(e.Data, &o); err != nil {
		return nil, chipperErr("webhook_order").With("event", e.Event).
			Code(pkgErrors.CodeDecodeFailed).Wrapf(err, "could not decode the order")
	}
	return &o, nil
}

func validateOrder(req OrderRequest) error {
	errb := chipperErr("validate_order").With(pkgErrors.AttrSequenceID, req.ExternalReference)

	switch {
	case req.ExternalReference == "" || len(req.ExternalReference) > maxReferenceLen:
		return errb.Code(pkgErrors.CodeInvalidConfig).
			With("max_length", maxReferenceLen).Errorf("external reference length is out of range")
	case req.From.Code == "" || req.From.Currency == "" || req.To.Code == "" || req.To.Currency == "":
		return errb.Code(pkgErrors.CodeMissingAccount).Errorf("both legs need a code and currency")
	case req.To.AccountNumber == "":
		return errb.Code(pkgErrors.CodeMissingAccount).Errorf("destination account number is required")
	case len(req.To.Tag) > 100:
		return errb.Code(pkgErrors.CodeInvalidMemo).Errorf("destination tag exceeds 100 characters")
	case len(req.Narration) > maxOrderNarration:
		return errb.Code(pkgErrors.CodeInvalidConfig).Errorf("narration exceeds 200 characters")
	case req.From.Amount == nil && req.To.Amount == nil:
		return errb.Code(pkgErrors.CodeMissingAmount).Errorf("one of from.amount and to.amount is required")
	case req.From.Amount != nil && req.To.Amount != nil:
		return errb.Code(pkgErrors.CodeInvalidAmount).Errorf("only one of from.amount and to.amount may be set")
	}
	if err := checkAmount(errb, "from.amount", req.From.Amount, req.From.Currency); err != nil {
		return err
	}
	return checkAmount(errb, "to.amount", req.To.Amount, req.To.Currency)
}
