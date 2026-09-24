package airtel

import (
	"context"
	"net/http"
	"net/url"
	"time"

	"github.com/samber/lo"

	pkgErrors "github.com/Shamba-Records-Limited/microvault/pkg/errors"
)

// Collection endpoints.
const (
	pathCollectionPayment = "/merchant/v1/payments/"
	pathCollectionRefund  = "/standard/v1/payments/refund"
	pathCollectionEnquiry = "/standard/v1/payments/"
)

// EnquiryFloor is the wait Airtel documents between a payment and the first
// enquiry about it. Asking sooner is not an error, it is uninformative.
const EnquiryFloor = 3 * time.Minute

// The endpoint contracts. Payment and Refund participate in v2 message
// signing; Enquiry does not.
var (
	epCollectionPayment = endpoint{method: http.MethodPost, path: pathCollectionPayment, headers: headerUpper, signed: true}
	epCollectionRefund  = endpoint{method: http.MethodPost, path: pathCollectionRefund, headers: headerUpper, signed: true}
	epCollectionEnquiry = endpoint{method: http.MethodGet, path: pathCollectionEnquiry, headers: headerUpper, outcomeBearing: true}
)

// PaymentRequest asks Airtel to push a USSD payment prompt to a subscriber,
// who authorises it on their handset.
type PaymentRequest struct {
	// Reference describes the goods or service purchased. It is shown in the
	// subscriber's records.
	Reference string

	// Payer is the subscriber to debit, in any Kenyan format. It is
	// normalised to the national form Airtel requires.
	Payer string

	// AmountKES is whole shillings.
	//
	// Airtel documents transaction.amount as a number and nowhere states
	// whether minor units are accepted. Whole shillings is the conservative
	// reading and matches the M-Pesa rail; the staging suite settles it.
	AmountKES int64

	// TransactionID is our own id and must be unique per partner. It is the
	// idempotency key, the handle for the enquiry, and the only thing that
	// makes a duplicate submission a duplicate-transaction error rather than
	// a second payment.
	TransactionID string
}

type paymentSubscriber struct {
	MSISDN string `json:"msisdn"`
}

type paymentTransaction struct {
	Amount int64  `json:"amount"`
	ID     string `json:"id"`
}

type paymentWireRequest struct {
	Reference   string             `json:"reference"`
	Subscriber  paymentSubscriber  `json:"subscriber"`
	Transaction paymentTransaction `json:"transaction"`
}

// PaymentResponse acknowledges that the prompt was accepted. It does not mean
// the subscriber paid — that arrives on the callback or an enquiry.
type PaymentResponse struct {
	Data struct {
		Transaction struct {
			ID     string `json:"id"`
			Status string `json:"status"`
		} `json:"transaction"`
	} `json:"data"`
	Status Status `json:"status"`
}

func (r *PaymentResponse) envelope() Status { return r.Status }

// ResponseCode reports the product code, falling back to the legacy result
// code for products that have not been migrated off it.
func (r PaymentResponse) ResponseCode() string {
	return lo.CoalesceOrEmpty(r.Status.ResponseCode, r.Status.ResultCode)
}

// Pending reports whether Airtel took the payment but has not resolved it.
// A pending payment is neither a success nor a failure: enquire, never retry.
func (r PaymentResponse) Pending() bool { return IsPending(r.ResponseCode()) }

// Accepted reports whether Airtel took the prompt for delivery. An accepted
// prompt is not a payment.
func (r PaymentResponse) Accepted() bool {
	return r.Status.Success || r.ResponseCode() == CodeCollectionSuccess || r.Pending()
}

// Payment pushes a USSD payment prompt to the payer's handset.
func (c *Client) Payment(ctx context.Context, req PaymentRequest) (*PaymentResponse, error) {
	errb := airtelErr("collection_payment").With("transaction_id", req.TransactionID)

	if err := validatePayment(errb, req); err != nil {
		return nil, err
	}
	msisdn, err := NormalizeMSISDN(req.Payer)
	if err != nil {
		return nil, err
	}

	body := paymentWireRequest{
		Reference:   req.Reference,
		Subscriber:  paymentSubscriber{MSISDN: msisdn.String()},
		Transaction: paymentTransaction{Amount: req.AmountKES, ID: req.TransactionID},
	}
	return call[PaymentResponse](ctx, c, errb, epCollectionPayment, epCollectionPayment.path, body)
}

// EnquiryResponse is the current state of a collection.
type EnquiryResponse struct {
	Data struct {
		Transaction struct {
			// AirtelMoneyID is populated only on TS. On TIP and TF it is
			// absent, and reading it unconditionally is the single most
			// common way to break on the non-success paths.
			AirtelMoneyID string `json:"airtel_money_id"`

			ID      string `json:"id"`
			Message string `json:"message"`
			Status  string `json:"status"`
		} `json:"transaction"`
	} `json:"data"`
	Status Status `json:"status"`
}

func (r *EnquiryResponse) envelope() Status { return r.Status }

// TransactionStatus reports the outcome as a typed value.
func (r EnquiryResponse) TransactionStatus() TransactionStatus {
	return ParseTransactionStatus(r.Data.Transaction.Status)
}

// Receipt returns Airtel's own transaction id and whether it was present.
// It is the only key a refund accepts, and it exists only on success.
func (r EnquiryResponse) Receipt() (string, bool) {
	id := r.Data.Transaction.AirtelMoneyID
	return id, id != "" && r.TransactionStatus().Succeeded()
}

// Enquiry reads the state of a collection, keyed by our own transaction id.
//
// Airtel documents a three-minute wait after Payment; see EnquiryFloor. The
// floor is not enforced here because a caller resolving a lost callback hours
// later is also a legitimate enquiry, and a client that refused would leave
// them no way to ask.
//
// An enquiry reporting TF is not an error. The question was answered; read
// TransactionStatus for the answer. Only a failure to ask — a rejected
// request, a missing transaction — comes back as one.
func (c *Client) Enquiry(ctx context.Context, transactionID string) (*EnquiryResponse, error) {
	errb := airtelErr("collection_enquiry").With("transaction_id", transactionID)

	if transactionID == "" {
		return nil, errb.
			Code(pkgErrors.CodeMissingDependency).
			Errorf("no transaction id was supplied")
	}

	path := pathCollectionEnquiry + url.PathEscape(transactionID)
	return call[EnquiryResponse](ctx, c, errb, epCollectionEnquiry, path, nil)
}

type refundTransaction struct {
	AirtelMoneyID string `json:"airtel_money_id"`
}

type refundWireRequest struct {
	Transaction refundTransaction `json:"transaction"`
}

// RefundResponse reports the outcome of a refund.
type RefundResponse struct {
	Data struct {
		Transaction struct {
			AirtelMoneyID string `json:"airtel_money_id"`
			Status        string `json:"status"`
		} `json:"transaction"`
	} `json:"data"`
	Status Status `json:"status"`
}

func (r *RefundResponse) envelope() Status { return r.Status }

// Refund returns a collection to the payer in full. Airtel supports no
// partial refunds.
//
// It is keyed by airtelMoneyID — Airtel's own id, not ours — while Enquiry is
// keyed by ours. That asymmetry means a refund is impossible until an enquiry
// or a callback has disclosed the id, and it is the reverse of the rule on
// Airtel's own ATM Withdrawal product, which refunds by the partner id.
func (c *Client) Refund(ctx context.Context, airtelMoneyID string) (*RefundResponse, error) {
	errb := airtelErr("collection_refund").With("airtel_money_id", airtelMoneyID)

	if airtelMoneyID == "" {
		return nil, errb.
			Code(pkgErrors.CodeMissingDependency).
			Hint("A refund needs the id Airtel minted, which only an enquiry or a callback discloses.").
			Errorf("no airtel money id was supplied")
	}

	body := refundWireRequest{Transaction: refundTransaction{AirtelMoneyID: airtelMoneyID}}
	return call[RefundResponse](ctx, c, errb, epCollectionRefund, epCollectionRefund.path, body)
}
