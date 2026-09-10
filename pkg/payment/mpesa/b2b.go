package mpesa

import (
	"context"
	"net/http"
	"strconv"

	pkgErrors "github.com/Shamba-Records-Limited/microvault/pkg/errors"
)

// Envelope B: pays a shortcode, debits the paying shortcode's MMF/Working
// account. Package capability only — nothing in this repo calls any of these
// yet. An automated collection sweep (B2B-paying an on-ramp's own paybill —
// BusinessPayBill or BusinessBuyGoods, not BusinessPayment, which is
// envelope A/B2C and pays an MSISDN) stays deferred pending a provider
// choice and a fee comparison against the OTC desk; see
// config.MpesaConfig.SettlementMode.

const (
	pathB2B          = "/mpesa/b2b/v1/paymentrequest"
	pathB2BAcctTopUp = "/mpesa/b2bacctopup/v1/paymentrequest"
	pathB2BRemitTax  = "/mpesa/b2b/v1/remittax"
)

// kraPRN is the fixed PartyB for tax remittance. Not a shortcode Daraja
// resolves for us — Safaricom's own documentation names this exact value.
const kraPRN = "572572"

// B2BRequest pays a shortcode from another shortcode's MMF/Working account.
type B2BRequest struct {
	// PartyA defaults to the configured collection shortcode — the float this
	// package's collections land in is the one every envelope-B command here
	// is expected to move money out of.
	PartyA uint

	// PartyB is the receiving paybill or till. Ignored by PayTaxToKRA (always
	// 572572) and by BusinessPayToBulk when zero (defaults to the configured
	// disbursement shortcode).
	PartyB uint

	AmountKES int64

	// AccountReference is at most 13 characters — one more than STK's 12, per
	// Daraja's own example (ACC#03929/4yu).
	AccountReference string

	// Requester is the consumer's MSISDN this payment is made on behalf of.
	// Optional; populate it whenever the B2B payment exists because of a
	// specific borrower, e.g. paying a supplier directly for purpose-bound
	// lending.
	Requester string

	Remarks string

	URLs AsyncURLs
}

// BusinessPayBill pays a paybill.
func (c *Client) BusinessPayBill(ctx context.Context, req B2BRequest) (*AsyncAck, error) {
	return c.sendEnvelopeB(ctx, "business_pay_bill", pathB2B, "BusinessPayBill", req)
}

// BusinessBuyGoods pays a till.
func (c *Client) BusinessBuyGoods(ctx context.Context, req B2BRequest) (*AsyncAck, error) {
	return c.sendEnvelopeB(ctx, "business_buy_goods", pathB2B, "BusinessBuyGoods", req)
}

// BusinessPayToBulk moves float from the collection shortcode to the
// disbursement shortcode — the bridge that makes the closed loop of §9
// possible (collections fund disbursements without a manual sweep). This is
// the one envelope-B command where both parties are ours, which makes it the
// safest to exercise first: a double-send is recoverable because the money
// never leaves our own accounts.
func (c *Client) BusinessPayToBulk(ctx context.Context, req B2BRequest) (*AsyncAck, error) {
	if req.PartyB == 0 {
		req.PartyB = c.disbursementShortcode
	}
	return c.sendEnvelopeB(ctx, "business_pay_to_bulk", pathB2BAcctTopUp, "BusinessPayToBulk", req)
}

// PayTaxToKRA remits to KRA. PartyB is fixed at 572572 and is not a
// parameter — a caller cannot express a different destination.
// AccountReference should be the KRA-issued Payment Registration Number, not
// a loan reference.
func (c *Client) PayTaxToKRA(ctx context.Context, req B2BRequest) (*AsyncAck, error) {
	errb := mpesaErr("pay_tax_to_kra")
	req.PartyB = 0 // set on the wire directly; never resolvable from the caller.
	if req.PartyA == 0 {
		req.PartyA = c.collectionShortcode
	}
	if req.PartyA == 0 {
		return nil, errb.
			Code(pkgErrors.CodeMissingDependency).
			With(pkgErrors.AttrDependency, "collection shortcode").
			Errorf("no paying shortcode was supplied or configured")
	}
	if req.AmountKES <= 0 {
		return nil, errb.Code(pkgErrors.CodeInvalidAmount).Errorf("amount must be positive")
	}
	if err := validateRemarks(errb, req.Remarks); err != nil {
		return nil, err
	}
	if err := req.URLs.validate(errb); err != nil {
		return nil, err
	}
	credential, err := c.SecurityCredential()
	if err != nil {
		return nil, err
	}
	body := b2bBody(c, "PayTaxToKRA", credential, req.PartyA, 0, req.AmountKES, req.AccountReference, req.Requester, req.Remarks, req.URLs)
	body["PartyB"] = kraPRN
	return call[AsyncAck](ctx, c, errb, http.MethodPost, pathB2BRemitTax, body)
}

func (c *Client) sendEnvelopeB(ctx context.Context, op, path, command string, req B2BRequest) (*AsyncAck, error) {
	errb := mpesaErr(op)

	if req.PartyA == 0 {
		req.PartyA = c.collectionShortcode
	}
	if req.PartyA == 0 {
		return nil, errb.
			Code(pkgErrors.CodeMissingDependency).
			With(pkgErrors.AttrDependency, "collection shortcode").
			Errorf("no paying shortcode was supplied or configured")
	}
	if req.PartyB == 0 {
		return nil, errb.Code(pkgErrors.CodeMissingDependency).Errorf("party b is required")
	}
	if req.AmountKES <= 0 {
		return nil, errb.Code(pkgErrors.CodeInvalidAmount).Errorf("amount must be positive")
	}
	if len(req.AccountReference) > 13 {
		return nil, errb.
			Code(pkgErrors.CodeBuildFailed).
			With("length", len(req.AccountReference)).
			Errorf("account reference must be at most 13 characters")
	}
	if err := validateRemarks(errb, req.Remarks); err != nil {
		return nil, err
	}
	if err := req.URLs.validate(errb); err != nil {
		return nil, err
	}

	credential, err := c.SecurityCredential()
	if err != nil {
		return nil, err
	}

	body := b2bBody(c, command, credential, req.PartyA, req.PartyB, req.AmountKES, req.AccountReference, req.Requester, req.Remarks, req.URLs)
	return call[AsyncAck](ctx, c, errb, http.MethodPost, path, body)
}

// b2bBody builds the envelope-B wire body. SenderIdentifierType and
// RecieverIdentifierType (Safaricom's misspelling, reproduced exactly) are
// "4" for every envelope-B command regardless of paybill vs till — set here
// so a caller cannot express anything else.
func b2bBody(c *Client, command, credential string, partyA, partyB uint, amountKES int64, accountRef, requester, remarks string, urls AsyncURLs) map[string]string {
	body := map[string]string{
		"Initiator":              c.initiatorName,
		"SecurityCredential":     credential,
		"CommandID":              command,
		"SenderIdentifierType":   string(IdentifierShortcode),
		"RecieverIdentifierType": string(IdentifierShortcode),
		"Amount":                 strconv.FormatInt(amountKES, 10),
		"PartyA":                 strconv.FormatUint(uint64(partyA), 10),
		"AccountReference":       accountRef,
		"Requester":              requester,
		"Remarks":                remarks,
		"QueueTimeOutURL":        urls.QueueTimeOutURL,
		"ResultURL":              urls.ResultURL,
	}
	if partyB != 0 {
		body["PartyB"] = strconv.FormatUint(uint64(partyB), 10)
	}
	return body
}
