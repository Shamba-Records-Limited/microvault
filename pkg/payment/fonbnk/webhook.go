package fonbnk

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"time"

	pkgErrors "github.com/Shamba-Records-Limited/microvault/pkg/errors"
)

// EventOrderStatusChange is the only event a server integration subscribes to
// by default.
const EventOrderStatusChange = "order-status-change"

// SignatureHeader carries the webhook signature.
const SignatureHeader = "x-signature"

// WebhookCashout is the pricing summary a webhook carries. Unlike Cashout on
// an order it has no fee breakdown.
type WebhookCashout struct {
	ExchangeRate          float64 `json:"exchangeRate"`
	ExchangeRateAfterFees float64 `json:"exchangeRateAfterFees"`
	AmountBeforeFees      float64 `json:"amountBeforeFees"`
	AmountAfterFees       float64 `json:"amountAfterFees"`
	AmountBeforeFeesUSD   float64 `json:"amountBeforeFeesUsd"`
	AmountAfterFeesUSD    float64 `json:"amountAfterFeesUsd"`
}

// WebhookLeg is one side of an order as a webhook reports it.
type WebhookLeg struct {
	PaymentChannel  string          `json:"paymentChannel"`
	CurrencyType    string          `json:"currencyType"`
	CurrencyCode    string          `json:"currencyCode"`
	CurrencyDetails CurrencyDetails `json:"currencyDetails"`
	Cashout         WebhookCashout  `json:"cashout"`
	Transaction     *LegTransaction `json:"transaction,omitempty"`
}

// WebhookUserKyc is the KYC state of the order's user.
type WebhookUserKyc struct {
	PassedKycType   string `json:"passedKycType,omitempty"`
	PassedKycHash   string `json:"passedKycHash,omitempty"`
	LatestKycType   string `json:"latestKycType,omitempty"`
	LatestKycStatus string `json:"latestKycStatus,omitempty"`
}

// WebhookOrder is the order summary delivered on a status change. It is not
// the full order — no fee breakdown, no transfer instructions, no
// statusChangeLogs. Call GetOrder for those.
type WebhookOrder struct {
	ID                  string      `json:"_id"`
	UserID              string      `json:"userId"`
	UserEmail           string      `json:"userEmail"`
	MerchantOrderParams string      `json:"merchantOrderParams,omitempty"`
	CountryIsoCode      string      `json:"countryIsoCode"`
	Flow                string      `json:"flow"`
	Type                string      `json:"type"`
	Source              string      `json:"source"`
	Status              string      `json:"status"`
	Deposit             WebhookLeg  `json:"deposit"`
	Payout              WebhookLeg  `json:"payout"`
	Refund              *WebhookLeg `json:"refund,omitempty"`
	CreatedAt           time.Time   `json:"createdAt"`
	UpdatedAt           time.Time   `json:"updatedAt"`
}

// WebhookEvent is the delivered payload.
type WebhookEvent struct {
	Event string `json:"event"`
	Data  struct {
		Order   WebhookOrder   `json:"order"`
		UserKyc WebhookUserKyc `json:"userKyc"`
	} `json:"data"`
}

// VerifyWebhookSignature checks a delivery against the webhook secret.
//
// The scheme is a nested plain SHA-256, not an HMAC:
//
//	hex(SHA256(rawBody || hex(SHA256(secret))))
//
// rawBody must be the exact bytes received — parsing and re-serialising the
// JSON changes the hash.
func VerifyWebhookSignature(rawBody []byte, signature, secret string) error {
	errb := fonbnkErr("verify_webhook")

	if signature == "" {
		return errb.Code(pkgErrors.CodeUnauthorized).Errorf("webhook carried no signature")
	}
	if secret == "" {
		return errb.Code(pkgErrors.CodeMissingDependency).Errorf("webhook secret is not configured")
	}

	if !hmac.Equal([]byte(signature), []byte(webhookSignature(rawBody, secret))) {
		return errb.Code(pkgErrors.CodeUnauthorized).Errorf("webhook signature does not match")
	}
	return nil
}

// webhookSignature computes the expected signature.
func webhookSignature(rawBody []byte, secret string) string {
	secretHash := sha256.Sum256([]byte(secret))
	secretHashHex := hex.EncodeToString(secretHash[:])

	outer := sha256.New()
	outer.Write(rawBody)
	outer.Write([]byte(secretHashHex))
	return hex.EncodeToString(outer.Sum(nil))
}

// ParseWebhook verifies the signature and decodes the payload.
func ParseWebhook(rawBody []byte, signature, secret string) (*WebhookEvent, error) {
	if err := VerifyWebhookSignature(rawBody, signature, secret); err != nil {
		return nil, err
	}

	var event WebhookEvent
	if err := json.Unmarshal(rawBody, &event); err != nil {
		return nil, fonbnkErr("parse_webhook").
			Code(pkgErrors.CodeDecodeFailed).
			Wrapf(err, "could not decode the webhook payload")
	}
	return &event, nil
}
