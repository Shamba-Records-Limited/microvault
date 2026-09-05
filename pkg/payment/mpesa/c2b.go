package mpesa

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	pkgErrors "github.com/Shamba-Records-Limited/microvault/pkg/errors"
)

// C2B v2 endpoints. v1 is not implemented: it hashes the MSISDN with SHA-256
// where v2 masks it, and neither the hash nor the mask is a usable number, so
// there is nothing to gain from supporting both.
const (
	pathC2BRegisterURL = "/mpesa/c2b/v2/registerurl"
	pathC2BSimulate    = "/mpesa/c2b/v2/simulate"
)

// ResponseType is what M-Pesa does when our validation URL is unreachable.
type ResponseType string

// The two fallback behaviours.
//
// Completed accepts payments we never saw and therefore cannot attribute.
// Cancelled refuses payments during any outage of ours. For a lender the second
// is the safer default — an unattributable payment is worse than a payment that
// did not happen — but it trades collection availability for reconciliation
// safety and is a business decision, not an engineering one.
//
// Safaricom's own documentation spells the second value both "Cancelled" and
// "Canceled" on different pages while warning that it must be well-spelled.
// Registration is a one-time production call, so confirm the spelling with
// Safaricom before making it.
const (
	ResponseTypeCompleted ResponseType = "Completed"
	ResponseTypeCancelled ResponseType = "Cancelled"
)

// RegisterURLRequest points a shortcode at our callback URLs.
type RegisterURLRequest struct {
	// Shortcode defaults to the configured collection shortcode.
	Shortcode uint

	ResponseType ResponseType

	// ValidationURL is called only when external validation is enabled on the
	// shortcode, which it is not by default. Enabling it is a request to
	// Safaricom, not a setting.
	ValidationURL string

	ConfirmationURL string
}

// RegisterURLResponse acknowledges a registration.
type RegisterURLResponse struct {
	OriginatorConversationID string `json:"OriginatorCoversationID"`
	ResponseCode             string `json:"ResponseCode"`
	ResponseDescription      string `json:"ResponseDescription"`
}

// RegisterURL binds callback URLs to a shortcode.
//
// In production this is effectively one-time: re-registering requires Safaricom
// to delete the existing URLs first. Treat it as a deliberate operator action,
// not something a service does at boot.
func (c *Client) RegisterURL(ctx context.Context, req RegisterURLRequest) (*RegisterURLResponse, error) {
	errb := mpesaErr("c2b_register_url")

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
	if req.ResponseType != ResponseTypeCompleted && req.ResponseType != ResponseTypeCancelled {
		return nil, errb.
			Code(pkgErrors.CodeBuildFailed).
			With("response_type", string(req.ResponseType)).
			Errorf("response type must be Completed or Cancelled")
	}
	if req.ConfirmationURL == "" {
		return nil, errb.Code(pkgErrors.CodeMissingDependency).Errorf("confirmation URL is required")
	}
	for _, url := range []string{req.ConfirmationURL, req.ValidationURL} {
		if url == "" {
			continue
		}
		if err := AssertCallbackURL(url); err != nil {
			return nil, err
		}
	}

	body := map[string]any{
		"ShortCode":       strconv.FormatUint(uint64(shortcode), 10),
		"ResponseType":    string(req.ResponseType),
		"ConfirmationURL": req.ConfirmationURL,
		"ValidationURL":   req.ValidationURL,
	}
	return call[RegisterURLResponse](ctx, c, errb.With("shortcode", shortcode), http.MethodPost, pathC2BRegisterURL, body)
}

// SimulateRequest drives a sandbox payment. It is rejected outright in
// production, where the only way to move money is for a customer to move it.
type SimulateRequest struct {
	Shortcode     uint
	CommandID     TransactionType
	AmountKES     int64
	Payer         string
	BillRefNumber string
}

// SimulateResponse acknowledges a simulated payment.
type SimulateResponse struct {
	OriginatorConversationID string `json:"OriginatorCoversationID"`
	ConversationID           string `json:"ConversationID"`
	ResponseCode             string `json:"ResponseCode"`
	ResponseDescription      string `json:"ResponseDescription"`
}

// Simulate triggers a C2B payment in sandbox.
func (c *Client) Simulate(ctx context.Context, req SimulateRequest) (*SimulateResponse, error) {
	errb := mpesaErr("c2b_simulate")

	if c.env.IsProduction() {
		return nil, errb.
			Code(pkgErrors.CodePermissionDenied).
			Errorf("C2B simulation is not available in production")
	}

	shortcode := req.Shortcode
	if shortcode == 0 {
		shortcode = c.collectionShortcode
	}
	if req.CommandID == "" {
		req.CommandID = CustomerPayBillOnline
	}
	if req.AmountKES <= 0 {
		return nil, errb.Code(pkgErrors.CodeInvalidAmount).Errorf("amount must be positive")
	}
	payer, err := NormalizeMSISDN(req.Payer)
	if err != nil {
		return nil, err
	}

	body := map[string]any{
		"ShortCode":     strconv.FormatUint(uint64(shortcode), 10),
		"CommandID":     string(req.CommandID),
		"Amount":        strconv.FormatInt(req.AmountKES, 10),
		"Msisdn":        payer,
		"BillRefNumber": req.BillRefNumber,
	}
	return call[SimulateResponse](ctx, c, errb, http.MethodPost, pathC2BSimulate, body)
}

// C2BNotification is a validation or confirmation payload.
//
// It is not evidence. Daraja signs nothing, so a well-formed notification
// proves only that something posted to a URL. Confirm independently before it
// moves anything.
type C2BNotification struct {
	TransactionType   string
	TransID           string
	TransTime         string
	TransAmountMinor  int64
	BusinessShortCode string

	// BillRefNumber is the account number the payer typed, and on a shared
	// paybill it is the only thing binding a payment to a loan. It is null for
	// till payments.
	BillRefNumber string

	InvoiceNumber string

	// OrgAccountBalanceMinor is blank on validation and the post-payment
	// balance on confirmation.
	OrgAccountBalanceMinor int64

	// ThirdPartyTransID is ours to set: a validation response may return one,
	// and Daraja echoes it on the matching confirmation.
	ThirdPartyTransID string

	// MSISDN is masked on C2B v2. It cannot be used as a phone number; the
	// unmasked value comes from Pull Transaction.
	MSISDN MaskedMSISDN

	FirstName  string
	MiddleName string
	LastName   string
}

type c2bNotificationWire struct {
	TransactionType   string `json:"TransactionType"`
	TransID           string `json:"TransID"`
	TransTime         string `json:"TransTime"`
	TransAmount       string `json:"TransAmount"`
	BusinessShortCode string `json:"BusinessShortCode"`
	BillRefNumber     string `json:"BillRefNumber"`
	InvoiceNumber     string `json:"InvoiceNumber"`
	OrgAccountBalance string `json:"OrgAccountBalance"`
	ThirdPartyTransID string `json:"ThirdPartyTransID"`
	MSISDN            string `json:"MSISDN"`
	FirstName         string `json:"FirstName"`
	MiddleName        string `json:"MiddleName"`
	LastName          string `json:"LastName"`
}

// ParseC2BNotification decodes a validation or confirmation payload.
func ParseC2BNotification(raw []byte) (*C2BNotification, error) {
	var wire c2bNotificationWire
	if err := json.Unmarshal(raw, &wire); err != nil {
		return nil, mpesaErr("parse_c2b_notification").
			Code(pkgErrors.CodeDecodeFailed).
			Wrapf(err, "could not decode the C2B notification")
	}

	notification := &C2BNotification{
		TransactionType:   wire.TransactionType,
		TransID:           wire.TransID,
		TransTime:         wire.TransTime,
		BusinessShortCode: wire.BusinessShortCode,
		BillRefNumber:     strings.TrimSpace(wire.BillRefNumber),
		InvoiceNumber:     wire.InvoiceNumber,
		ThirdPartyTransID: wire.ThirdPartyTransID,
		MSISDN:            MaskedMSISDN(wire.MSISDN),
		FirstName:         wire.FirstName,
		MiddleName:        wire.MiddleName,
		LastName:          wire.LastName,
	}
	if amount, err := parseMinor(wire.TransAmount); err == nil {
		notification.TransAmountMinor = amount
	}
	if balance, err := parseMinor(wire.OrgAccountBalance); err == nil {
		notification.OrgAccountBalanceMinor = balance
	}
	return notification, nil
}

// PaidAt decodes TransTime.
func (n C2BNotification) PaidAt() (parsed string, ok bool) {
	value, found := ParseTimestamp(n.TransTime)
	if !found {
		return "", false
	}
	return value.Format("2006-01-02T15:04:05Z"), true
}

// ValidationResultCode is what we answer a validation request with.
type ValidationResultCode string

// Accept, and the documented rejection codes. Rejecting with the right code
// gives the customer a message that matches what went wrong, which is the
// difference between "wrong account number" and a generic failure on a feature
// phone.
const (
	ValidationAccepted             ValidationResultCode = "0"
	ValidationInvalidMSISDN        ValidationResultCode = "C2B00011"
	ValidationInvalidAccountNumber ValidationResultCode = "C2B00012"
	ValidationInvalidAmount        ValidationResultCode = "C2B00013"
	ValidationInvalidKYC           ValidationResultCode = "C2B00014"
	ValidationInvalidShortcode     ValidationResultCode = "C2B00015"
	ValidationOtherError           ValidationResultCode = "C2B00016"
)

// ValidationResponse is the body we return to a validation request.
type ValidationResponse struct {
	ResultCode ValidationResultCode `json:"ResultCode"`
	ResultDesc string               `json:"ResultDesc"`

	// ThirdPartyTransID is echoed back on the matching confirmation, which is
	// the only way to correlate the two callbacks.
	ThirdPartyTransID string `json:"ThirdPartyTransID,omitempty"`
}

// AcceptPayment builds an accepting validation response.
func AcceptPayment(thirdPartyTransID string) ValidationResponse {
	return ValidationResponse{
		ResultCode:        ValidationAccepted,
		ResultDesc:        "Accepted",
		ThirdPartyTransID: thirdPartyTransID,
	}
}

// RejectPayment builds a rejecting validation response.
//
// Rejecting with code 0 accepts the payment, so a caller that forgets to set a
// code accepts money it meant to refuse. This exists so the code is never
// implicit.
func RejectPayment(code ValidationResultCode) ValidationResponse {
	if code == ValidationAccepted {
		code = ValidationOtherError
	}
	return ValidationResponse{ResultCode: code, ResultDesc: "Rejected"}
}
