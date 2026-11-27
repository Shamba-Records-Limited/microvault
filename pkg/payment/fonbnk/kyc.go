package fonbnk

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	pkgErrors "github.com/Shamba-Records-Limited/microvault/pkg/errors"
)

// KYC tiers.
const (
	KYCBasic    = "basic"
	KYCAdvanced = "advanced"
)

// KYCStateQuery asks where a user stands for an order. The four amount fields
// are sent together or not at all; with them the answer accounts for the
// order, without them only for the user's history.
type KYCStateQuery struct {
	UserEmail         string
	CountryIsoCode    string
	KYCCountryIsoCode string
	Order             *KYCOrderAmounts
	CallbackURL       string
	CallbackBtnText   string
}

// KYCOrderAmounts are the pre-check amounts: the deposit before fees and the
// payout after fees, both in USD, as a quote reports them.
type KYCOrderAmounts struct {
	DepositCurrencyType string
	PayoutCurrencyType  string
	DepositAmountUSD    float64
	PayoutAmountUSD     float64
}

// KYCDocument is one document a user may submit for a tier.
type KYCDocument struct {
	ID             string          `json:"_id"`
	Type           string          `json:"type"`
	Title          string          `json:"title"`
	Value          string          `json:"value"`
	RequiredFields []RequiredField `json:"requiredFields"`
}

// KYCSetting is one of the country's KYC rules.
type KYCSetting struct {
	OperationType  string   `json:"operationType"`
	CurrencyType   string   `json:"currencyType"`
	Type           string   `json:"type"`
	Min            *float64 `json:"min,omitempty"`
	Max            MaxBound `json:"max,omitzero"`
	MaxAmountUSD   *float64 `json:"maxAmountUsd,omitempty"`
	MaxOrdersCount *int     `json:"maxOrdersCount,omitempty"`
}

// KYCURLs are hosted verification links, one per tier the country can
// satisfy today.
type KYCURLs struct {
	Basic    string `json:"basic,omitempty"`
	Advanced string `json:"advanced,omitempty"`
}

// KYCState is a user's KYC standing. Branch on RequiredType: empty means
// proceed.
type KYCState struct {
	PassedType               string        `json:"passedKycType,omitempty"`
	RequiredType             string        `json:"requiredKycType,omitempty"`
	ReachedLimit             bool          `json:"reachedKycLimit"`
	CurrentType              string        `json:"currentKycType,omitempty"`
	CurrentStatus            string        `json:"currentKycStatus,omitempty"`
	CurrentStatusDescription string        `json:"currentKycStatusDescription,omitempty"`
	PassedCountryIsoCode     string        `json:"passedKycCountryIsoCode,omitempty"`
	Documents                []KYCDocument `json:"kycDocuments"`
	Settings                 []KYCSetting  `json:"kycSettings"`
	URLs                     *KYCURLs      `json:"kycUrls,omitempty"`
	OfframpKYCRequired       bool          `json:"offrampKycRequired"`
	NGBVNBlocked             bool          `json:"isNgBvnBlocked"`
	NGBVNSupportLocked       bool          `json:"isNgBvnSupportLocked"`
	Message                  string        `json:"message,omitempty"`
}

// Satisfied reports whether the user may create the order without another
// document: nothing is required, or the passed tier covers it.
func (s KYCState) Satisfied() bool {
	switch s.RequiredType {
	case "":
		return true
	case KYCBasic:
		return s.PassedType == KYCBasic || s.PassedType == KYCAdvanced
	default:
		return s.PassedType == KYCAdvanced
	}
}

// Document returns the first enabled document for a tier.
func (s KYCState) Document(tier string) (KYCDocument, bool) {
	for _, d := range s.Documents {
		if d.Type == tier {
			return d, true
		}
	}
	return KYCDocument{}, false
}

// KYCSubmission posts one document's fields for a user.
type KYCSubmission struct {
	UserEmail         string         `json:"userEmail"`
	DocumentID        string         `json:"documentId"`
	UserFields        map[string]any `json:"userFields"`
	CountryIsoCode    string         `json:"countryIsoCode,omitempty"`
	KYCCountryIsoCode string         `json:"kycCountryIsoCode,omitempty"`
}

func (q KYCStateQuery) values() url.Values {
	v := url.Values{}
	v.Set("userEmail", q.UserEmail)
	v.Set("countryIsoCode", q.CountryIsoCode)
	if q.KYCCountryIsoCode != "" {
		v.Set("kycCountryIsoCode", q.KYCCountryIsoCode)
	}
	if o := q.Order; o != nil {
		v.Set("depositCurrencyType", o.DepositCurrencyType)
		v.Set("payoutCurrencyType", o.PayoutCurrencyType)
		v.Set("depositAmountUsd", strconv.FormatFloat(o.DepositAmountUSD, 'f', -1, 64))
		v.Set("payoutAmountUsd", strconv.FormatFloat(o.PayoutAmountUSD, 'f', -1, 64))
	}
	if q.CallbackURL != "" {
		v.Set("callbackUrl", q.CallbackURL)
	}
	if q.CallbackBtnText != "" {
		v.Set("callbackBtnText", q.CallbackBtnText)
	}
	return v
}

// GetUserKYCState reads a user's KYC standing. It registers an unseen email
// as a Fonbnk user, and needs the create-users permission.
func (a *FonbnkAdapter) GetUserKYCState(ctx context.Context, q KYCStateQuery) (*KYCState, error) {
	errb := fonbnkErr("get_user_kyc_state").With("country", q.CountryIsoCode)
	if strings.TrimSpace(q.UserEmail) == "" {
		return nil, errb.Code(pkgErrors.CodeMissingPayerEmail).Errorf("user email is required")
	}
	if q.CountryIsoCode == "" {
		return nil, errb.Code(pkgErrors.CodeMissingDependency).Errorf("country is required")
	}
	return call[KYCState](ctx, a, errb, http.MethodGet, withQuery(pathUserKYC, q.values()), nil)
}

// SubmitUserKYC submits one document. The user must exist, so call
// GetUserKYCState first. Verification is asynchronous.
func (a *FonbnkAdapter) SubmitUserKYC(ctx context.Context, s KYCSubmission) (*KYCState, error) {
	errb := fonbnkErr("submit_user_kyc").With("document_id", s.DocumentID)
	switch {
	case strings.TrimSpace(s.UserEmail) == "":
		return nil, errb.Code(pkgErrors.CodeMissingPayerEmail).Errorf("user email is required")
	case s.DocumentID == "" || len(s.UserFields) == 0:
		return nil, errb.Code(pkgErrors.CodeMissingDependency).Errorf("document id and user fields are required")
	}
	return call[KYCState](ctx, a, errb, http.MethodPost, pathUserKYC, s)
}
