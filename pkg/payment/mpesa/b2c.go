package mpesa

import (
	"context"
	"net/http"
	"regexp"
	"strconv"

	pkgErrors "github.com/Shamba-Records-Limited/microvault/pkg/errors"
)

// Envelope A: pays an MSISDN, debits the disbursement shortcode's Utility
// account. Package capability only — nothing in this repo calls B2C or
// B2Pochi yet. Disbursement stays on the OTC desk / cmd/mpesa-settle path
// until a provider and a fee comparison settle §17 of the plan.

const (
	pathB2C     = "/mpesa/b2c/v3/paymentrequest"
	pathB2Pochi = "/mpesa/b2pochi/v1/paymentrequest"
)

// PayoutCommand is an envelope-A command ID. Typed so a caller cannot pass a
// B2B command where a B2C one belongs.
type PayoutCommand string

// The envelope-A commands. CommandBusinessPayment is the one this package
// recommends. SalaryPayment no longer reaches unregistered numbers despite
// its name — Safaricom's own table says so — and PromotionPayment's
// congratulatory SMS is the wrong tone for a loan; both are exposed because
// Daraja documents them, not because anything here sends them.
const (
	CommandBusinessPayment  PayoutCommand = "BusinessPayment"
	CommandSalaryPayment    PayoutCommand = "SalaryPayment"
	CommandPromotionPayment PayoutCommand = "PromotionPayment"

	// commandBusinessPayToPochi is pinned inside B2Pochi, never caller-set —
	// the B2Pochi page's own CommandID table lists the three above as "other
	// supported values", which is copy-paste from the B2C page. Whether they
	// are genuinely routable there is not worth discovering in production.
	commandBusinessPayToPochi PayoutCommand = "BusinessPayToPochi"
)

// partyBMSISDN matches a 12-digit MSISDN with no leading '+', e.g.
// 254712345678.
var partyBMSISDN = regexp.MustCompile(`^2\d{11}$`)

// B2CRequest pays an MSISDN.
type B2CRequest struct {
	// Command defaults to CommandBusinessPayment.
	Command PayoutCommand

	// OriginatorConversationID is caller-supplied and is B2C's native
	// idempotency key — Daraja rejects reuse with 500.002.1001, which means
	// "this request already reached us", never a reason to retry with a
	// fresh ID. See §7 of the plan.
	OriginatorConversationID string

	// PartyA defaults to the configured disbursement shortcode.
	PartyA uint

	// PartyB is the recipient MSISDN: 12 digits, no '+'.
	PartyB string

	AmountKES int64

	// Remarks is 2-100 characters.
	Remarks string

	// Occasion is 1-100 characters when set; optional.
	Occasion string

	URLs AsyncURLs
}

// B2C pays a registered M-PESA customer from the disbursement shortcode.
func (c *Client) B2C(ctx context.Context, req B2CRequest) (*AsyncAck, error) {
	if req.Command == "" {
		req.Command = CommandBusinessPayment
	}
	return c.sendEnvelopeA(ctx, "b2c", pathB2C, req)
}

// B2Pochi pays a business's Pochi la Biashara wallet. Envelope A despite
// living under Daraja's B2B navigation — see §3 of the plan — because it
// pays an MSISDN, not a shortcode.
func (c *Client) B2Pochi(ctx context.Context, req B2CRequest) (*AsyncAck, error) {
	req.Command = commandBusinessPayToPochi
	return c.sendEnvelopeA(ctx, "b2pochi", pathB2Pochi, req)
}

func (c *Client) sendEnvelopeA(ctx context.Context, op, path string, req B2CRequest) (*AsyncAck, error) {
	errb := mpesaErr(op).With(pkgErrors.AttrRecipient, req.PartyB)

	if req.OriginatorConversationID == "" {
		return nil, errb.Code(pkgErrors.CodeMissingDependency).Errorf("originator conversation id is required")
	}
	if req.PartyA == 0 {
		req.PartyA = c.disbursementShortcode
	}
	if req.PartyA == 0 {
		return nil, errb.
			Code(pkgErrors.CodeMissingDependency).
			With(pkgErrors.AttrDependency, "disbursement shortcode").
			Errorf("no disbursement shortcode was supplied or configured")
	}
	if !partyBMSISDN.MatchString(req.PartyB) {
		return nil, errb.Code(pkgErrors.CodeBuildFailed).Errorf("party b must be a 12-digit MSISDN with no leading +")
	}
	if req.AmountKES <= 0 {
		return nil, errb.Code(pkgErrors.CodeInvalidAmount).Errorf("amount must be positive")
	}
	if err := validateRemarks(errb, req.Remarks); err != nil {
		return nil, err
	}
	if req.Occasion != "" && len(req.Occasion) > 100 {
		return nil, errb.Code(pkgErrors.CodeBuildFailed).Errorf("occasion must be at most 100 characters")
	}
	if err := req.URLs.validate(errb); err != nil {
		return nil, err
	}

	credential, err := c.SecurityCredential()
	if err != nil {
		return nil, err
	}

	body := map[string]string{
		"OriginatorConversationID": req.OriginatorConversationID,
		"InitiatorName":            c.initiatorName,
		"SecurityCredential":       credential,
		"CommandID":                string(req.Command),
		"Amount":                   strconv.FormatInt(req.AmountKES, 10),
		"PartyA":                   strconv.FormatUint(uint64(req.PartyA), 10),
		"PartyB":                   req.PartyB,
		"Remarks":                  req.Remarks,
		"QueueTimeOutURL":          req.URLs.QueueTimeOutURL,
		"ResultURL":                req.URLs.ResultURL,
		//nolint:misspell // Safaricom's spelling, preserved on the wire.
		"Occassion": req.Occasion,
	}
	return call[AsyncAck](ctx, c, errb, http.MethodPost, path, body)
}
