package chipper

import (
	"context"
	"net/http"
	"net/url"
	"time"

	"github.com/govalues/decimal"

	pkgErrors "github.com/Shamba-Records-Limited/microvault/pkg/errors"
)

// Payout statuses.
const (
	PayoutPending    = "pending"
	PayoutProcessing = "processing"
	PayoutCompleted  = "completed"
	PayoutFailed     = "failed"
)

const maxReferenceLen = 100

// PayoutRequest creates a payout. Exactly one of From.Amount and To.Amount
// is set.
type PayoutRequest struct {
	From              *PayoutSource     `json:"from,omitempty"`
	To                PayoutDestination `json:"to"`
	Narration         string            `json:"narration,omitempty"`
	ExternalReference string            `json:"externalReference"`
	Metadata          map[string]string `json:"metadata,omitempty"`
}

// PayoutSource selects the balance to debit; Amount means "debit exactly".
type PayoutSource struct {
	Amount   *Amount        `json:"amount,omitempty"`
	Currency string         `json:"currency,omitempty"`
	KYC      map[string]any `json:"kyc,omitempty"`
}

// PayoutDestination is where the money lands; Amount means "deliver exactly".
type PayoutDestination struct {
	Code          string         `json:"code"`
	AccountNumber string         `json:"accountNumber,omitempty"`
	Address       string         `json:"address,omitempty"`
	Tag           string         `json:"tag,omitempty"`
	Amount        *Amount        `json:"amount,omitempty"`
	Currency      string         `json:"currency"`
	Recipient     *Recipient     `json:"recipient,omitempty"`
	KYC           map[string]any `json:"kyc,omitempty"`
}

// Recipient names the payee.
type Recipient struct {
	Name string `json:"name"`
}

// Money is an amount in a currency.
type Money struct {
	Amount   decimal.Decimal `json:"amount"`
	Currency string          `json:"currency"`
}

// PayoutParty is one side of a payout as returned.
type PayoutParty struct {
	AccountNumber *string         `json:"accountNumber"`
	Address       *string         `json:"address,omitempty"`
	Tag           string          `json:"tag,omitempty"`
	Code          *string         `json:"code,omitempty"`
	Amount        decimal.Decimal `json:"amount"`
	Currency      string          `json:"currency"`
	KYC           map[string]any  `json:"kyc,omitempty"`
}

// TimelineEvent is one audit entry on a payout.
type TimelineEvent struct {
	At     time.Time `json:"at"`
	Kind   string    `json:"kind"`
	Title  string    `json:"title"`
	Detail *string   `json:"detail"`
}

// Payout is a payout resource.
type Payout struct {
	ID                string           `json:"id"`
	Status            string           `json:"status"`
	From              PayoutParty      `json:"from"`
	To                PayoutParty      `json:"to"`
	Fee               *Money           `json:"fee"`
	Rate              *decimal.Decimal `json:"rate"`
	Narration         *string          `json:"narration"`
	ExternalReference string           `json:"externalReference"`
	Metadata          map[string]any   `json:"metadata"`
	StatusMessage     *string          `json:"statusMessage"`
	CompletedAt       *time.Time       `json:"completedAt"`
	CreatedAt         time.Time        `json:"createdAt"`
	UpdatedAt         time.Time        `json:"updatedAt"`
	Timeline          []TimelineEvent  `json:"timeline,omitempty"`
}

// IsTerminal reports whether the payout has reached a final status.
func (p Payout) IsTerminal() bool { return p.Status == PayoutCompleted || p.Status == PayoutFailed }

type payoutEnvelope struct {
	Payout Payout `json:"payout"`
}

// CreatePayout submits a payout. replayed is true when Chipper returned an
// existing payout for the same externalReference.
func (c *Client) CreatePayout(ctx context.Context, req PayoutRequest) (payout *Payout, replayed bool, err error) {
	errb := chipperErr("create_payout").With(pkgErrors.AttrSequenceID, req.ExternalReference)

	if err := validatePayout(req); err != nil {
		return nil, false, err
	}

	out, status, err := call[payoutEnvelope](ctx, c, errb, http.MethodPost, "/v1/payouts", nil, req)
	if err != nil {
		return nil, false, err
	}
	return &out.Payout, status == http.StatusOK, nil
}

// GetPayout reads a payout with its timeline.
func (c *Client) GetPayout(ctx context.Context, id string) (*Payout, error) {
	errb := chipperErr("get_payout").With("payout_id", id)
	if id == "" {
		return nil, errb.Code(pkgErrors.CodeMissingAccount).Errorf("payout id is required")
	}
	out, _, err := call[payoutEnvelope](ctx, c, errb, http.MethodGet, "/v1/payouts/"+url.PathEscape(id), nil, nil)
	if err != nil {
		return nil, err
	}
	return &out.Payout, nil
}

// GetPayoutByReference reads a payout by externalReference, without timeline.
func (c *Client) GetPayoutByReference(ctx context.Context, reference string) (*Payout, error) {
	errb := chipperErr("get_payout_by_reference").With(pkgErrors.AttrSequenceID, reference)
	if reference == "" {
		return nil, errb.Code(pkgErrors.CodeMissingAccount).Errorf("external reference is required")
	}
	out, _, err := call[payoutEnvelope](ctx, c, errb, http.MethodGet, "/v1/payouts/by-reference/"+url.PathEscape(reference), nil, nil)
	if err != nil {
		return nil, err
	}
	return &out.Payout, nil
}

func validatePayout(req PayoutRequest) error {
	errb := chipperErr("validate_payout").With(pkgErrors.AttrSequenceID, req.ExternalReference)

	switch {
	case req.ExternalReference == "" || len(req.ExternalReference) > maxReferenceLen:
		return errb.Code(pkgErrors.CodeInvalidConfig).
			With("max_length", maxReferenceLen).Errorf("external reference length is out of range")
	case req.To.Code == "" || req.To.Currency == "":
		return errb.Code(pkgErrors.CodeMissingAccount).Errorf("destination code and currency are required")
	case req.To.AccountNumber == "" && req.To.Address == "":
		return errb.Code(pkgErrors.CodeMissingAccount).Errorf("destination account number or address is required")
	case req.To.Recipient != nil && (req.To.Recipient.Name == "" || len(req.To.Recipient.Name) > 200):
		return errb.Code(pkgErrors.CodeMissingAccount).Errorf("recipient name must be 1 to 200 characters")
	case len(req.Narration) > 500:
		return errb.Code(pkgErrors.CodeInvalidConfig).Errorf("narration exceeds 500 characters")
	}
	if kyc := req.To.KYC; kyc != nil {
		if name, _ := kyc["accountName"].(string); name == "" {
			return errb.Code(pkgErrors.CodeMissingAccount).Errorf("destination kyc requires accountName")
		}
	}

	var fromAmount *Amount
	fromCurrency := ""
	if req.From != nil {
		fromAmount, fromCurrency = req.From.Amount, req.From.Currency
	}
	switch {
	case fromAmount == nil && req.To.Amount == nil:
		return errb.Code(pkgErrors.CodeMissingAmount).Errorf("one of from.amount and to.amount is required")
	case fromAmount != nil && req.To.Amount != nil:
		return errb.Code(pkgErrors.CodeInvalidAmount).Errorf("only one of from.amount and to.amount may be set")
	case fromAmount != nil && fromCurrency == "":
		return errb.Code(pkgErrors.CodeMissingAccount).Errorf("from.currency is required with from.amount")
	}
	if err := checkAmount(errb, "from.amount", fromAmount, fromCurrency); err != nil {
		return err
	}
	return checkAmount(errb, "to.amount", req.To.Amount, req.To.Currency)
}
