package chipper

import (
	"context"
	"net/http"
	"net/url"
	"time"

	pkgErrors "github.com/Shamba-Records-Limited/microvault/pkg/errors"
)

// ChainStellar is the chain name crypto addresses use for Stellar.
const ChainStellar = "Stellar"

// CryptoAddress is a deposit address. There is one per currency and chain
// per organization; Tag is the memo a deposit must carry when set.
type CryptoAddress struct {
	ID                string         `json:"id"`
	Currency          string         `json:"currency"`
	Chain             string         `json:"chain"`
	Address           *string        `json:"address"`
	Tag               *string        `json:"tag"`
	ExternalReference *string        `json:"externalReference"`
	Metadata          map[string]any `json:"metadata,omitempty"`
	CreatedAt         time.Time      `json:"createdAt"`
	UpdatedAt         *time.Time     `json:"updatedAt,omitempty"`
}

// CryptoAddressRequest provisions a deposit address. Re-requesting the same
// currency and chain returns the existing address.
type CryptoAddressRequest struct {
	Currency          string         `json:"currency"`
	Chain             string         `json:"chain"`
	ExternalReference string         `json:"externalReference,omitempty"`
	Metadata          map[string]any `json:"metadata,omitempty"`
}

// CryptoAddressFilter narrows an address list.
type CryptoAddressFilter struct {
	Currency          string
	Chain             string
	ExternalReference string
	Limit             int
	Cursor            string
}

type cryptoAddressEnvelope struct {
	Address CryptoAddress `json:"address"`
}

// CreateCryptoAddress provisions, or returns the existing, deposit address.
func (c *Client) CreateCryptoAddress(ctx context.Context, req CryptoAddressRequest) (*CryptoAddress, error) {
	errb := chipperErr("create_crypto_address").With(pkgErrors.AttrCurrency, req.Currency).With("chain", req.Chain)
	switch {
	case req.Currency == "" || req.Chain == "":
		return nil, errb.Code(pkgErrors.CodeMissingAccount).Errorf("currency and chain are required")
	case len(req.ExternalReference) > 255:
		return nil, errb.Code(pkgErrors.CodeInvalidConfig).Errorf("external reference exceeds 255 characters")
	}
	out, _, err := call[cryptoAddressEnvelope](ctx, c, errb, http.MethodPost, "/v1/crypto-addresses", nil, req)
	if err != nil {
		return nil, err
	}
	if out.Address.Address == nil || *out.Address.Address == "" {
		return nil, errb.Code(pkgErrors.CodeIncompleteResponse).With("address_id", out.Address.ID).
			Errorf("chipper returned a crypto address with no address")
	}
	return &out.Address, nil
}

// GetCryptoAddress reads one deposit address.
func (c *Client) GetCryptoAddress(ctx context.Context, id string) (*CryptoAddress, error) {
	errb := chipperErr("get_crypto_address").With("address_id", id)
	if id == "" {
		return nil, errb.Code(pkgErrors.CodeMissingAccount).Errorf("address id is required")
	}
	out, _, err := call[cryptoAddressEnvelope](ctx, c, errb, http.MethodGet, "/v1/crypto-addresses/"+url.PathEscape(id), nil, nil)
	if err != nil {
		return nil, err
	}
	return &out.Address, nil
}

// ListCryptoAddresses reads one page of deposit addresses.
func (c *Client) ListCryptoAddresses(ctx context.Context, f CryptoAddressFilter) (*Page[CryptoAddress], error) {
	q := url.Values{}
	setIf(q, "currency", f.Currency)
	setIf(q, "chain", f.Chain)
	setIf(q, "externalReference", f.ExternalReference)
	page, _, err := call[Page[CryptoAddress]](ctx, c, chipperErr("list_crypto_addresses"), http.MethodGet, "/v1/crypto-addresses", pageQuery(q, f.Limit, f.Cursor), nil)
	return page, err
}
