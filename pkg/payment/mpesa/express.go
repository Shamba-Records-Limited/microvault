package mpesa

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	pkgErrors "github.com/Shamba-Records-Limited/microvault/pkg/errors"
	"github.com/Shamba-Records-Limited/microvault/pkg/phone"
)

// M-Pesa Express endpoints.
const (
	pathExpressSimulate = "/mpesa/stkpush/v1/processrequest"
	pathExpressQuery    = "/mpesa/stkpushquery/v1/query"
)

// Field limits Safaricom documents. Checked locally because exceeding them
// turns into an opaque 400 or, worse, result code 1025 after the prompt has
// already been built.
const (
	maxAccountReference = 12
	maxTransactionDesc  = 13
)

// TransactionType selects the paybill or till flavour of the prompt. Sending
// the wrong one for the shortcode yields result code 2028.
type TransactionType string

// The two prompt flavours.
const (
	CustomerPayBillOnline  TransactionType = "CustomerPayBillOnline"
	CustomerBuyGoodsOnline TransactionType = "CustomerBuyGoodsOnline"
)

// ExpressRequest asks Daraja to push a payment prompt to a handset.
type ExpressRequest struct {
	// Shortcode defaults to the configured collection shortcode.
	Shortcode uint

	TransactionType TransactionType

	// AmountKES is whole shillings. M-Pesa Express does not accept cents.
	AmountKES int64

	// Payer receives the prompt and is debited. Accepted in any Kenyan format
	// and normalised to 2547XXXXXXXX.
	Payer string

	// CallbackURL receives the result. It must not contain a word Daraja
	// blocks; see AssertCallbackURL.
	CallbackURL string

	// AccountReference is shown to the customer in the prompt. Twelve
	// characters at most.
	AccountReference string

	// TransactionDesc is optional. Thirteen characters at most.
	TransactionDesc string
}

type expressWireRequest struct {
	BusinessShortCode string `json:"BusinessShortCode"`
	Password          string `json:"Password"`
	Timestamp         string `json:"Timestamp"`
	TransactionType   string `json:"TransactionType"`
	Amount            string `json:"Amount"`
	PartyA            string `json:"PartyA"`
	PartyB            string `json:"PartyB"`
	PhoneNumber       string `json:"PhoneNumber"`
	CallBackURL       string `json:"CallBackURL"`
	AccountReference  string `json:"AccountReference"`
	TransactionDesc   string `json:"TransactionDesc"`
}

// ExpressResponse acknowledges that the prompt was accepted for processing. It
// does not mean the customer paid — that arrives on the callback.
type ExpressResponse struct {
	MerchantRequestID   string `json:"MerchantRequestID"`
	CheckoutRequestID   string `json:"CheckoutRequestID"`
	ResponseCode        string `json:"ResponseCode"`
	ResponseDescription string `json:"ResponseDescription"`
	CustomerMessage     string `json:"CustomerMessage"`
}

// Express pushes a payment prompt to the payer's handset.
func (c *Client) Express(ctx context.Context, req ExpressRequest) (*ExpressResponse, error) {
	errb := mpesaErr("express").With("account_reference", req.AccountReference)

	shortcode := req.Shortcode
	if shortcode == 0 {
		shortcode = c.collectionShortcode
	}
	if shortcode == 0 {
		return nil, errb.
			Code(pkgErrors.CodeMissingDependency).
			With(pkgErrors.AttrDependency, "collection shortcode").
			Errorf("no shortcode was supplied or configured")
	}
	if req.TransactionType == "" {
		req.TransactionType = CustomerPayBillOnline
	}
	if err := validateExpress(errb, req); err != nil {
		return nil, err
	}

	payer, err := NormalizeMSISDN(req.Payer)
	if err != nil {
		return nil, err
	}

	// The password is derived from the timestamp, so both must come from one
	// instant. Reading the clock twice yields a password Daraja rejects as
	// 500.001.1001 "Wrong credentials" roughly once a second.
	timestamp, password := c.expressPassword(shortcode)

	body := expressWireRequest{
		BusinessShortCode: strconv.FormatUint(uint64(shortcode), 10),
		Password:          password,
		Timestamp:         timestamp,
		TransactionType:   string(req.TransactionType),
		Amount:            strconv.FormatInt(req.AmountKES, 10),
		PartyA:            payer,
		PartyB:            strconv.FormatUint(uint64(shortcode), 10),
		PhoneNumber:       payer,
		CallBackURL:       req.CallbackURL,
		AccountReference:  req.AccountReference,
		TransactionDesc:   req.TransactionDesc,
	}
	return call[ExpressResponse](ctx, c, errb.With("payer", phone.Redact(payer)), http.MethodPost, pathExpressSimulate, body)
}

// expressPassword renders the timestamp and the base64 password from a single
// reading of the clock.
func (c *Client) expressPassword(shortcode uint) (timestamp, password string) {
	timestamp = c.now().Format("20060102150405")
	raw := strconv.FormatUint(uint64(shortcode), 10) + c.passkey + timestamp
	return timestamp, base64.StdEncoding.EncodeToString([]byte(raw))
}

func validateExpress(errb oopsBuilder, req ExpressRequest) error {
	switch {
	case req.AmountKES <= 0:
		return errb.Code(pkgErrors.CodeInvalidAmount).
			With(pkgErrors.AttrAmountLocal, req.AmountKES).
			Errorf("amount must be a positive whole number of shillings")
	case req.AccountReference == "":
		return errb.Code(pkgErrors.CodeMissingDependency).
			Errorf("account reference is required")
	case len(req.AccountReference) > maxAccountReference:
		return errb.Code(pkgErrors.CodeBuildFailed).
			With("length", len(req.AccountReference)).
			Hint("Daraja caps AccountReference at 12 characters and an overlong prompt fails with result code 1025.").
			Errorf("account reference is too long")
	case len(req.TransactionDesc) > maxTransactionDesc:
		return errb.Code(pkgErrors.CodeBuildFailed).
			With("length", len(req.TransactionDesc)).
			Errorf("transaction description is too long")
	case req.CallbackURL == "":
		return errb.Code(pkgErrors.CodeMissingDependency).
			Errorf("callback URL is required")
	}
	return AssertCallbackURL(req.CallbackURL)
}

// ExpressQueryResponse reports the state of a checkout.
type ExpressQueryResponse struct {
	ResponseCode        string        `json:"ResponseCode"`
	ResponseDescription string        `json:"ResponseDescription"`
	MerchantRequestID   string        `json:"MerchantRequestID"`
	CheckoutRequestID   string        `json:"CheckoutRequestID"`
	ResultCode          FlexibleInt64 `json:"ResultCode"`
	ResultDesc          string        `json:"ResultDesc"`
}

// ExpressQuery asks Daraja for the state of a checkout.
//
// Daraja is widely reported to error rather than report "pending" while a
// prompt is still on the handset, and documents neither behaviour. Callers must
// treat an error as "not yet resolved" and retry, not as a terminal failure.
func (c *Client) ExpressQuery(ctx context.Context, checkoutRequestID string, shortcode uint) (*ExpressQueryResponse, error) {
	errb := mpesaErr("express_query").With("checkout_request_id", checkoutRequestID)

	if checkoutRequestID == "" {
		return nil, errb.Code(pkgErrors.CodeMissingDependency).Errorf("checkout request ID is required")
	}
	if shortcode == 0 {
		shortcode = c.collectionShortcode
	}
	timestamp, password := c.expressPassword(shortcode)

	body := map[string]string{
		"BusinessShortCode": strconv.FormatUint(uint64(shortcode), 10),
		"Password":          password,
		"Timestamp":         timestamp,
		"CheckoutRequestID": checkoutRequestID,
	}
	return call[ExpressQueryResponse](ctx, c, errb, http.MethodPost, pathExpressQuery, body)
}

// ExpressCallback is the decoded stkCallback body.
//
// Note the items use Name rather than the Key used by every Result envelope, so
// this does not share a decoder with resultparams.go.
type ExpressCallback struct {
	MerchantRequestID string
	CheckoutRequestID string
	ResultCode        int64
	ResultDesc        string

	// The following are present only on success; CallbackMetadata is absent
	// entirely when the customer did not pay.
	AmountKES     int64
	AmountMinor   int64
	ReceiptNumber string
	Payer         string
	CompletedAt   time.Time
}

// Succeeded reports whether the customer paid.
func (e ExpressCallback) Succeeded() bool { return e.ResultCode == 0 }

type expressCallbackEnvelope struct {
	Body struct {
		STKCallback struct {
			MerchantRequestID string        `json:"MerchantRequestID"`
			CheckoutRequestID string        `json:"CheckoutRequestID"`
			ResultCode        FlexibleInt64 `json:"ResultCode"`
			ResultDesc        string        `json:"ResultDesc"`
			CallbackMetadata  *struct {
				Item []struct {
					Name  string          `json:"Name"`
					Value json.RawMessage `json:"Value"`
				} `json:"Item"`
			} `json:"CallbackMetadata"`
		} `json:"stkCallback"`
	} `json:"Body"`
}

// ParseExpressCallback decodes an M-Pesa Express callback.
func ParseExpressCallback(raw []byte) (*ExpressCallback, error) {
	var envelope expressCallbackEnvelope
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return nil, mpesaErr("parse_express_callback").
			Code(pkgErrors.CodeDecodeFailed).
			Wrapf(err, "could not decode the express callback")
	}

	inner := envelope.Body.STKCallback
	callback := &ExpressCallback{
		MerchantRequestID: inner.MerchantRequestID,
		CheckoutRequestID: inner.CheckoutRequestID,
		ResultCode:        inner.ResultCode.Int64(),
		ResultDesc:        inner.ResultDesc,
	}
	if inner.CallbackMetadata == nil {
		return callback, nil
	}

	for _, item := range inner.CallbackMetadata.Item {
		value := scalarString(item.Value)
		switch item.Name {
		case "Amount":
			if minor, err := parseMinor(value); err == nil {
				callback.AmountMinor = minor
				callback.AmountKES = minor / 100
			}
		case "MpesaReceiptNumber":
			callback.ReceiptNumber = value
		case "PhoneNumber":
			callback.Payer = value
		case "TransactionDate":
			if parsed, ok := ParseTimestamp(value); ok {
				callback.CompletedAt = parsed
			}
		}
	}
	return callback, nil
}

// ExpressOutcome is what a result code means to the borrower.
type ExpressOutcome struct {
	// Retryable reports whether trying again could succeed without anything
	// else changing.
	Retryable bool

	// Operational reports whether this is our problem rather than the payer's.
	// An operational failure must never be surfaced to a borrower as if they
	// did something wrong.
	Operational bool

	// Message is safe to show a borrower. GSM 03.38 characters only, because
	// it may be rendered on a feature phone over USSD or SMS.
	Message string
}

// expressOutcomes maps every result code Safaricom documents for M-Pesa
// Express. The codes are not shared with other endpoints: 2001 is a wrong
// customer PIN here and an invalid initiator on Reversal.
var expressOutcomes = map[int64]ExpressOutcome{
	0:    {Message: "Payment received."},
	1:    {Retryable: true, Message: "Not enough money in your M-PESA account. Top up and try again."},
	2:    {Message: "That amount is below the minimum M-PESA allows."},
	3:    {Message: "That amount is above the maximum M-PESA allows."},
	4:    {Retryable: true, Message: "This would pass your M-PESA daily limit. Try again tomorrow."},
	8:    {Retryable: true, Message: "This would pass your M-PESA balance limit."},
	17:   {Retryable: true, Message: "Too many similar payments. Wait two minutes and try again."},
	1019: {Retryable: true, Message: "The request took too long. Please try again."},
	1025: {Operational: true, Message: "We could not send the payment request. Please try again."},
	1032: {Retryable: true, Message: "You cancelled the payment request."},
	1037: {Retryable: true, Message: "We could not reach your phone. Check it is on and try again."},
	2001: {Retryable: true, Message: "Wrong M-PESA PIN. Please try again."},
	2028: {Operational: true, Message: "We could not process the payment. Please try again later."},
	8006: {Operational: true, Message: "We could not process the payment. Please try again later."},
}

// ExpressOutcomeFor reports what a result code means. An undocumented code is
// treated as operational and not retryable, so an unknown failure is neither
// blamed on the borrower nor retried blindly.
func ExpressOutcomeFor(resultCode int64) ExpressOutcome {
	if outcome, ok := expressOutcomes[resultCode]; ok {
		return outcome
	}
	return ExpressOutcome{Operational: true, Message: "We could not process the payment. Please try again later."}
}

// Words Daraja rejects anywhere in a callback URL. The obvious route for this
// integration — /api/v1/webhooks/mpesa/confirmation — contains one of them.
var blockedURLWords = []string{"mpesa", "safaricom", "exe", "exec", "cmd", "sql", "query"}

// AssertCallbackURL rejects a URL Daraja will not accept, at the point it can
// still be changed rather than at a one-time registration call.
func AssertCallbackURL(value string) error {
	errb := mpesaErr("callback_url").With("callback_url", value)

	if !strings.HasPrefix(value, "https://") && !strings.HasPrefix(value, "http://") {
		return errb.Code(pkgErrors.CodeBuildFailed).Errorf("callback URL must be absolute")
	}
	lower := strings.ToLower(value)
	for _, word := range blockedURLWords {
		if strings.Contains(lower, word) {
			return errb.
				Code(pkgErrors.CodeBuildFailed).
				With("blocked_word", word).
				Hint("Daraja rejects URLs containing mpesa, safaricom, exe, exec, cmd, sql or query. Use a neutral path segment such as daraja.").
				Errorf("callback URL contains a word Daraja blocks")
		}
	}
	return nil
}
