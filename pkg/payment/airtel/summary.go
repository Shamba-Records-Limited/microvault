package airtel

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/samber/lo"

	pkgErrors "github.com/Shamba-Records-Limited/microvault/pkg/errors"
)

const pathTransactionsSummary = "/merchant/v1/transactions"

// Transactions Summary is one of the products taking the lowercase country
// and currency headers, against the same host and the same account as
// Collection, which takes the uppercase pair.
var epTransactionsSummary = endpoint{method: http.MethodGet, path: pathTransactionsSummary, headers: headerLower}

// Default and maximum page sizes. Airtel documents neither, so the default is
// conservative and the ceiling exists to stop a caller asking for a window
// the gateway will reject in a way that looks like an outage.
const (
	defaultSummaryLimit = 100
	maxSummaryLimit     = 500
)

// SummaryServiceType distinguishes the ledgers that land in one summary.
type SummaryServiceType string

// The documented service types.
const (
	ServiceMerchantPayment SummaryServiceType = "MERCHPAY"
	ServiceCashIn          SummaryServiceType = "CASHIN"
)

// SummaryTransaction is one settled transaction.
//
// This is the only endpoint in the catalogue that returns charges, and the
// only one that returns both parties — everywhere else one side is implied.
// That is what makes it the reconciliation source rather than a convenience.
type SummaryTransaction struct {
	Charges struct {
		Service float64 `json:"service"`
	} `json:"charges"`

	Payee struct {
		Currency string `json:"currency"`
		MSISDN   string `json:"msisdn"`
		Name     string `json:"name"`
	} `json:"payee"`

	Payer struct {
		Currency string `json:"currency"`
		MSISDN   string `json:"msisdn"`
		Name     string `json:"name"`
	} `json:"payer"`

	Service struct {
		Type SummaryServiceType `json:"type"`
	} `json:"service"`

	Transaction struct {
		AirtelMoneyID   string `json:"airtel_money_id"`
		Amount          string `json:"amount"`
		CreatedAt       string `json:"created_at"`
		ID              string `json:"id"`
		ReferenceNumber string `json:"reference_number"`
		Status          string `json:"status"`
	} `json:"transaction"`
}

// TransactionStatus reports the entry's outcome as a typed value.
func (t SummaryTransaction) TransactionStatus() TransactionStatus {
	return ParseTransactionStatus(t.Transaction.Status)
}

// AmountMinor parses the entry's amount into minor units.
func (t SummaryTransaction) AmountMinor() (int64, error) {
	return ParseFormattedAmount(t.Transaction.Amount)
}

// SummaryResponse is a page of settled transactions.
type SummaryResponse struct {
	Data struct {
		Count        int                  `json:"count"`
		Transactions []SummaryTransaction `json:"transactions"`
	} `json:"data"`
	Status Status `json:"status"`
}

func (r *SummaryResponse) envelope() Status { return r.Status }

// Settled returns only the entries that completed, which are the only ones
// carrying a receipt.
func (r SummaryResponse) Settled() []SummaryTransaction {
	return lo.Filter(r.Data.Transactions, func(t SummaryTransaction, _ int) bool {
		return t.TransactionStatus().Succeeded()
	})
}

// SummaryRequest is one page of the settled window.
type SummaryRequest struct {
	From time.Time
	To   time.Time

	// Limit defaults to defaultSummaryLimit and is capped at maxSummaryLimit.
	Limit int

	// Offset is the zero-based page start. This is the only paginated
	// endpoint in the catalogue.
	Offset int
}

// TransactionsSummary walks the settled window.
//
// The window bounds are EPOCH integers, not the formatted dates every other
// endpoint takes. Airtel's own sample response for this endpoint is
// structurally malformed — the four objects appear both nested and at the top
// level, and status is rendered as a character-indexed object — so the shapes
// above follow the attribute table instead.
func (c *Client) TransactionsSummary(ctx context.Context, req SummaryRequest) (*SummaryResponse, error) {
	errb := airtelErr("transactions_summary")

	if req.From.IsZero() || req.To.IsZero() {
		return nil, errb.
			Code(pkgErrors.CodeMissingDependency).
			Errorf("both window bounds are required")
	}
	if !req.From.Before(req.To) {
		return nil, errb.
			Code(pkgErrors.CodeBuildFailed).
			With("from", req.From).
			With("to", req.To).
			Errorf("the window start is not before its end")
	}

	limit := lo.Clamp(lo.Ternary(req.Limit > 0, req.Limit, defaultSummaryLimit), 1, maxSummaryLimit)
	offset := max(req.Offset, 0)

	query := url.Values{}
	query.Set("from", strconv.FormatInt(req.From.Unix(), 10))
	query.Set("to", strconv.FormatInt(req.To.Unix(), 10))
	query.Set("limit", strconv.Itoa(limit))
	query.Set("offset", strconv.Itoa(offset))

	path := pathTransactionsSummary + "?" + query.Encode()
	return call[SummaryResponse](ctx, c, errb, epTransactionsSummary, path, nil)
}
