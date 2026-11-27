package chipper

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/govalues/decimal"

	pkgErrors "github.com/Shamba-Records-Limited/microvault/pkg/errors"
)

// Collection statuses.
const (
	CollectionPending    = "pending"
	CollectionProcessing = "processing"
	CollectionCompleted  = "completed"
	CollectionFailed     = "failed"
	CollectionReversed   = "reversed"
	CollectionDisputed   = "disputed"
)

// CollectionTypeCrypto marks an inbound crypto deposit.
const CollectionTypeCrypto = "crypto"

const maxDepositPages = 5

// CollectionParty is one side of a collection.
type CollectionParty struct {
	AccountNumber *string         `json:"accountNumber"`
	Code          *string         `json:"code,omitempty"`
	Amount        decimal.Decimal `json:"amount"`
	Currency      string          `json:"currency"`
}

// Collection is an inflow: a charge we initiated or a deposit we received.
// For crypto deposits Details carries hash, chain and tag.
type Collection struct {
	ID                string          `json:"id"`
	Type              string          `json:"type"`
	Status            string          `json:"status"`
	From              CollectionParty `json:"from"`
	To                CollectionParty `json:"to"`
	Fee               *Money          `json:"fee"`
	Details           json.RawMessage `json:"details"`
	ExternalReference *string         `json:"externalReference"`
	CreatedAt         time.Time       `json:"createdAt"`
}

// CryptoDetails is Details for a crypto deposit.
type CryptoDetails struct {
	Hash  string `json:"hash"`
	Chain string `json:"chain"`
	Tag   string `json:"tag"`
}

// Crypto decodes Details of a crypto deposit. ok is false for other types.
func (c Collection) Crypto() (details CryptoDetails, ok bool) {
	if c.Type != CollectionTypeCrypto || len(c.Details) == 0 {
		return CryptoDetails{}, false
	}
	if err := json.Unmarshal(c.Details, &details); err != nil {
		return CryptoDetails{}, false
	}
	return details, true
}

// CollectionFilter narrows a collection list.
type CollectionFilter struct {
	Type            string
	Status          string
	Currency        string
	CryptoAddressID string
	From, To        time.Time
	Limit           int
	Cursor          string
}

// ListCollections reads one page of collections, newest first.
func (c *Client) ListCollections(ctx context.Context, f CollectionFilter) (*Page[Collection], error) {
	q := url.Values{}
	setIf(q, "type", f.Type)
	setIf(q, "status", f.Status)
	setIf(q, "currency", f.Currency)
	setIf(q, "cryptoAddressId", f.CryptoAddressID)
	setTime(q, "from", f.From)
	setTime(q, "to", f.To)
	page, _, err := call[Page[Collection]](ctx, c, chipperErr("list_collections"), http.MethodGet, "/v1/collections", pageQuery(q, f.Limit, f.Cursor), nil)
	return page, err
}

// FindCryptoDeposit looks for the deposit made by an on-chain transaction to
// one of our addresses since a point in time. ok is false when Chipper has
// not recorded it yet.
func (c *Client) FindCryptoDeposit(ctx context.Context, addressID, txHash string, since time.Time) (deposit *Collection, ok bool, err error) {
	errb := chipperErr("find_crypto_deposit").With(pkgErrors.AttrTxHash, txHash)
	if addressID == "" || txHash == "" {
		return nil, false, errb.Code(pkgErrors.CodeMissingAccount).Errorf("address id and tx hash are required")
	}

	filter := CollectionFilter{Type: CollectionTypeCrypto, CryptoAddressID: addressID, From: since, Limit: 100}
	for range maxDepositPages {
		page, err := c.ListCollections(ctx, filter)
		if err != nil {
			return nil, false, err
		}
		for i := range page.Data {
			if d, ok := page.Data[i].Crypto(); ok && strings.EqualFold(d.Hash, txHash) {
				return &page.Data[i], true, nil
			}
		}
		if !page.HasMore || page.NextCursor == nil {
			return nil, false, nil
		}
		filter.Cursor = *page.NextCursor
	}
	return nil, false, nil
}
