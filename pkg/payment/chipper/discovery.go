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

// Payment method statuses, derived from the last 30 minutes of traffic.
const (
	MethodOperational = "operational"
	MethodDegraded    = "degraded"
	MethodOutage      = "outage"
)

// Organization is the account a key controls.
type Organization struct {
	ID              string    `json:"id"`
	Name            string    `json:"name"`
	Country         string    `json:"country"`
	Timezone        string    `json:"timezone"`
	DefaultCurrency string    `json:"defaultCurrency"`
	Status          string    `json:"status"`
	CreatedAt       time.Time `json:"createdAt"`
	UpdatedAt       time.Time `json:"updatedAt"`
}

// MethodLimits bounds one transaction.
type MethodLimits struct {
	Min      decimal.Decimal `json:"min"`
	Max      decimal.Decimal `json:"max"`
	Currency string          `json:"currency"`
}

// MethodField describes an input a method needs.
type MethodField struct {
	Key         string        `json:"key"`
	Label       string        `json:"label"`
	Type        string        `json:"type"`
	Placeholder string        `json:"placeholder,omitempty"`
	Description string        `json:"description,omitempty"`
	Group       string        `json:"group,omitempty"`
	Options     []FieldOption `json:"options,omitempty"`
}

// FieldOption is one choice of a select field.
type FieldOption struct {
	Label string `json:"label"`
	Value string `json:"value"`
}

// PaymentMethod is one rail, e.g. ke_mpesa.
type PaymentMethod struct {
	Code                string        `json:"code"`
	Name                string        `json:"name"`
	Type                string        `json:"type"`
	Status              string        `json:"status"`
	Limits              *MethodLimits `json:"limits"`
	EstimatedSettlement string        `json:"estimatedSettlement"`
	Fields              []MethodField `json:"fields"`
}

// IsUsable reports whether the method is not in outage.
func (m PaymentMethod) IsUsable() bool { return m.Status != MethodOutage }

// CountryCapability lists the methods for one country and currency.
type CountryCapability struct {
	Country struct {
		Code string `json:"code"`
		Name string `json:"name"`
		Flag string `json:"flag"`
	} `json:"country"`
	Currency struct {
		Code string `json:"code"`
		Name string `json:"name"`
	} `json:"currency"`
	Methods []PaymentMethod `json:"methods"`
}

// DepositChain is one chain an asset can be deposited on.
type DepositChain struct {
	Chain        string `json:"chain"`
	Network      string `json:"network"`
	Code         string `json:"code"`
	RequiresMemo bool   `json:"requiresMemo"`
}

// DepositAsset is a crypto asset accepted for deposit.
type DepositAsset struct {
	Currency string         `json:"currency"`
	Name     string         `json:"name"`
	Chains   []DepositChain `json:"chains"`
}

// Capabilities is the catalog of methods. Deposits is empty for a
// per-country read.
type Capabilities struct {
	Payouts     []CountryCapability `json:"payouts"`
	Collections []CountryCapability `json:"collections"`
	Deposits    []DepositAsset      `json:"deposits,omitempty"`
}

// PayoutMethod finds a payout method by country and code.
func (c Capabilities) PayoutMethod(country, code string) (PaymentMethod, bool) {
	for _, cc := range c.Payouts {
		if !strings.EqualFold(cc.Country.Code, country) {
			continue
		}
		for _, m := range cc.Methods {
			if m.Code == code {
				return m, true
			}
		}
	}
	return PaymentMethod{}, false
}

// DepositChain finds the deposit chain for an asset.
func (c Capabilities) DepositChain(currency, chain string) (DepositChain, bool) {
	for _, a := range c.Deposits {
		if !strings.EqualFold(a.Currency, currency) {
			continue
		}
		for _, ch := range a.Chains {
			if strings.EqualFold(ch.Chain, chain) {
				return ch, true
			}
		}
	}
	return DepositChain{}, false
}

// Rate is an all-in rate: destination units per one origin unit, spread
// included, the same figure used at execution.
type Rate struct {
	From string          `json:"from"`
	To   string          `json:"to"`
	Rate decimal.Decimal `json:"rate"`
}

// ValidateRequest checks a destination.
type ValidateRequest struct {
	Code          string `json:"code"`
	AccountNumber string `json:"accountNumber,omitempty"`
	Address       string `json:"address,omitempty"`
	RoutingNumber string `json:"routingNumber,omitempty"`
}

// ValidationResult is the outcome of a destination check. An invalid
// destination is a 200 with Valid false.
type ValidationResult struct {
	Valid   bool   `json:"valid"`
	Enabled bool   `json:"enabled"`
	Success bool   `json:"success"`
	Name    string `json:"name,omitempty"`
	Error   string `json:"error,omitempty"`
	Status  string `json:"status,omitempty"`
}

// GetOrganization confirms which organization the key controls.
func (c *Client) GetOrganization(ctx context.Context) (*Organization, error) {
	out, _, err := call[struct {
		Organization Organization `json:"organization"`
	}](ctx, c, chipperErr("get_organization"), http.MethodGet, "/v1/organization", nil, nil)
	if err != nil {
		return nil, err
	}
	return &out.Organization, nil
}

// GetCapabilities reads the full catalog, including deposits.
func (c *Client) GetCapabilities(ctx context.Context) (*Capabilities, error) {
	return c.capabilities(ctx, chipperErr("get_capabilities"), "/v1/capabilities")
}

// GetCountryCapabilities reads one country's payouts and collections.
func (c *Client) GetCountryCapabilities(ctx context.Context, country string) (*Capabilities, error) {
	errb := chipperErr("get_country_capabilities").With("country", country)
	if country == "" {
		return nil, errb.Code(pkgErrors.CodeMissingAccount).Errorf("country is required")
	}
	return c.capabilities(ctx, errb, "/v1/capabilities/"+url.PathEscape(strings.ToUpper(country)))
}

func (c *Client) capabilities(ctx context.Context, errb errBuilder, path string) (*Capabilities, error) {
	out, _, err := call[struct {
		Capabilities Capabilities `json:"capabilities"`
	}](ctx, c, errb, http.MethodGet, path, nil, nil)
	if err != nil {
		return nil, err
	}
	return &out.Capabilities, nil
}

// GetRate reads the live all-in rate from origin to destination.
func (c *Client) GetRate(ctx context.Context, origin, destination string) (*Rate, error) {
	errb := chipperErr("get_rate").With("origin", origin).With("destination", destination)
	if origin == "" || destination == "" {
		return nil, errb.Code(pkgErrors.CodeMissingAccount).Errorf("origin and destination are required")
	}
	path := "/v1/rates/" + url.PathEscape(strings.ToUpper(origin)) + "/" + url.PathEscape(strings.ToUpper(destination))
	out, _, err := call[struct {
		Rate Rate `json:"rate"`
	}](ctx, c, errb, http.MethodGet, path, nil, nil)
	if err != nil {
		return nil, err
	}
	if !out.Rate.Rate.IsPos() {
		return nil, errb.Code(pkgErrors.CodeRateUnavailable).With("rate", out.Rate.Rate.String()).
			Errorf("chipper returned a non-positive rate")
	}
	return &out.Rate, nil
}

// Validate checks a destination. It is charged in production.
func (c *Client) Validate(ctx context.Context, req ValidateRequest) (*ValidationResult, error) {
	errb := chipperErr("validate").With("code", req.Code)
	if req.Code == "" || (req.AccountNumber == "" && req.Address == "") {
		return nil, errb.Code(pkgErrors.CodeMissingAccount).Errorf("code and an account number or address are required")
	}
	out, _, err := call[struct {
		Validation ValidationResult `json:"validation"`
	}](ctx, c, errb, http.MethodPost, "/v1/validate", nil, req)
	if err != nil {
		return nil, err
	}
	return &out.Validation, nil
}
