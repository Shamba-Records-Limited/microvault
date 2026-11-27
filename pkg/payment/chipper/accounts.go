package chipper

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/govalues/decimal"

	pkgErrors "github.com/Shamba-Records-Limited/microvault/pkg/errors"
)

// Ledger entry types used for reconciliation.
const (
	LedgerCryptoDeposit = "crypto_deposit"
	LedgerPayoutDebit   = "payout_debit"
	LedgerPayoutRefund  = "payout_refund"
	LedgerFeeDebit      = "fee_debit"
	LedgerFeeRefund     = "fee_refund"
	LedgerCollectionFee = "collection_fee_debit"
)

const maxAccountPages = 10

// Account is one balance. Spend from AvailableBalance only.
type Account struct {
	ID               string           `json:"id"`
	Currency         string           `json:"currency"`
	AvailableBalance decimal.Decimal  `json:"availableBalance"`
	PendingBalance   decimal.Decimal  `json:"pendingBalance"`
	TotalBalance     decimal.Decimal  `json:"totalBalance"`
	EstimatedUSD     *decimal.Decimal `json:"estimatedUsd"`
	CreatedAt        time.Time        `json:"createdAt"`
}

// LedgerEntry is one booked movement. Types outside the known set pass
// through unchanged.
type LedgerEntry struct {
	ID                string          `json:"id"`
	Sequence          int64           `json:"sequence"`
	Type              string          `json:"type"`
	Amount            decimal.Decimal `json:"amount"`
	Currency          string          `json:"currency"`
	AccountNumber     string          `json:"accountNumber"`
	BalanceAfter      decimal.Decimal `json:"balanceAfter"`
	ReferenceType     *string         `json:"referenceType"`
	ReferenceID       *string         `json:"referenceId"`
	ExternalReference *string         `json:"externalReference"`
	CreatedAt         time.Time       `json:"createdAt"`
}

// LedgerFilter narrows a ledger read.
type LedgerFilter struct {
	AccountNumber string
	Currency      string
	Type          string
	ReferenceID   string
	From, To      time.Time
	Limit         int
	Cursor        string
}

// Transaction is one row of the unified statement.
type Transaction struct {
	ID             string          `json:"id"`
	Type           string          `json:"type"`
	Reference      *string         `json:"reference"`
	Amount         decimal.Decimal `json:"amount"`
	RunningBalance decimal.Decimal `json:"runningBalance"`
	Currency       string          `json:"currency"`
	Description    string          `json:"description"`
	Status         string          `json:"status"`
	RelatedID      *string         `json:"relatedId"`
	CreatedAt      time.Time       `json:"createdAt"`
}

// TransactionFilter narrows a statement read.
type TransactionFilter struct {
	Currency string
	From, To time.Time
	Limit    int
	Cursor   string
}

// ListAccounts reads every balance.
func (c *Client) ListAccounts(ctx context.Context) ([]Account, error) {
	errb := chipperErr("list_accounts")
	var (
		accounts []Account
		cursor   string
	)
	for range maxAccountPages {
		page, _, err := call[Page[Account]](ctx, c, errb, http.MethodGet, "/v1/accounts", pageQuery(url.Values{}, 100, cursor), nil)
		if err != nil {
			return nil, err
		}
		accounts = append(accounts, page.Data...)
		if !page.HasMore || page.NextCursor == nil {
			return accounts, nil
		}
		cursor = *page.NextCursor
	}
	return nil, errb.Code(pkgErrors.CodeIncompleteResponse).With("max_pages", maxAccountPages).Errorf("account list did not end within the page limit")
}

// AccountFor reads the balance for currency. ok is false when it has never
// been used.
func (c *Client) AccountFor(ctx context.Context, currency string) (account *Account, ok bool, err error) {
	accounts, err := c.ListAccounts(ctx)
	if err != nil {
		return nil, false, err
	}
	for i := range accounts {
		if strings.EqualFold(accounts[i].Currency, currency) {
			return &accounts[i], true, nil
		}
	}
	return nil, false, nil
}

// ListLedger reads one page of ledger entries, newest first.
func (c *Client) ListLedger(ctx context.Context, f LedgerFilter) (*Page[LedgerEntry], error) {
	q := url.Values{}
	setIf(q, "accountNumber", f.AccountNumber)
	setIf(q, "currency", f.Currency)
	setIf(q, "type", f.Type)
	setIf(q, "referenceId", f.ReferenceID)
	setTime(q, "from", f.From)
	setTime(q, "to", f.To)
	page, _, err := call[Page[LedgerEntry]](ctx, c, chipperErr("list_ledger"), http.MethodGet, "/v1/ledger", pageQuery(q, f.Limit, f.Cursor), nil)
	return page, err
}

// ListTransactions reads one page of the unified statement, newest first.
func (c *Client) ListTransactions(ctx context.Context, f TransactionFilter) (*Page[Transaction], error) {
	q := url.Values{}
	setIf(q, "currency", f.Currency)
	setTime(q, "from", f.From)
	setTime(q, "to", f.To)
	page, _, err := call[Page[Transaction]](ctx, c, chipperErr("list_transactions"), http.MethodGet, "/v1/transactions", pageQuery(q, f.Limit, f.Cursor), nil)
	return page, err
}

func setIf(q url.Values, key, value string) {
	if value != "" {
		q.Set(key, value)
	}
}

func setTime(q url.Values, key string, t time.Time) {
	if !t.IsZero() {
		q.Set(key, t.UTC().Format(time.RFC3339))
	}
}
