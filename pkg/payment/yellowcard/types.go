// Package yellowcard provides types and client for the YellowCard payment API.
package yellowcard

// Channel represents a YellowCard payment channel such as bank transfer or mobile money.
type Channel struct {
	ID                      string                 `json:"id"`
	VendorID                string                 `json:"vendorId"`
	Country                 string                 `json:"country"`
	Currency                string                 `json:"currency"`
	CountryCurrency         string                 `json:"countryCurrency"`
	ChannelType             string                 `json:"channelType"`
	RampType                string                 `json:"rampType"`
	Status                  string                 `json:"status"`
	APIStatus               string                 `json:"apiStatus"`
	WidgetStatus            string                 `json:"widgetStatus"`
	SettlementType          string                 `json:"settlementType"`
	EstimatedSettlementTime int                    `json:"estimatedSettlementTime"`
	Min                     float64                `json:"min"`
	Max                     float64                `json:"max"`
	WidgetMin               float64                `json:"widgetMin,omitempty"`
	WidgetMax               float64                `json:"widgetMax,omitempty"`
	FeeLocal                float64                `json:"feeLocal"`
	FeeUSD                  float64                `json:"feeUSD"`
	Balancer                map[string]interface{} `json:"balancer,omitempty"`
	CreatedAt               string                 `json:"createdAt"`
	UpdatedAt               string                 `json:"updatedAt"`
}

// Network represents a bank or mobile money operator in the YellowCard system.
type Network struct {
	ID                       string   `json:"id"`
	Name                     string   `json:"name"`
	Code                     string   `json:"code"`
	Country                  string   `json:"country"`
	Status                   string   `json:"status"`
	AccountNumberType        string   `json:"accountNumberType"`
	CountryAccountNumberType string   `json:"countryAccountNumberType"`
	ChannelIDs               []string `json:"channelIds"`
	CreatedAt                string   `json:"createdAt"`
	UpdatedAt                string   `json:"updatedAt"`
}

// Rate represents exchange rate information between USD and a local currency.
type Rate struct {
	Buy       float64 `json:"buy"`
	Sell      float64 `json:"sell"`
	Locale    string  `json:"locale"`
	RateID    string  `json:"rateId"`
	Code      string  `json:"code"`
	UpdatedAt string  `json:"updatedAt"`
}

// RatesResponse wraps the rates array returned from the YellowCard rates endpoint.
type RatesResponse struct {
	Rates []Rate `json:"rates"`
}

// Sender contains KYC details for the payment sender.
// Required fields depend on CustomerType: "retail" requires personal info,
// "institution" requires BusinessID and BusinessName.
type Sender struct {
	Name               string `json:"name,omitempty"`
	Country            string `json:"country,omitempty"`
	Phone              string `json:"phone,omitempty"`
	Address            string `json:"address,omitempty"`
	DOB                string `json:"dob,omitempty"`
	Email              string `json:"email,omitempty"`
	IDNumber           string `json:"idNumber,omitempty"`
	IDType             string `json:"idType,omitempty"`
	AdditionalIDType   string `json:"additionalIdType,omitempty"`
	AdditionalIDNumber string `json:"additionalIdNumber,omitempty"`
	BusinessID         string `json:"businessId,omitempty"`
	BusinessName       string `json:"businessName,omitempty"`
}

// Destination represents where funds should be sent.
type Destination struct {
	AccountNumber string `json:"accountNumber"`
	AccountName   string `json:"accountName"`
	AccountType   string `json:"accountType"`
	NetworkID     string `json:"networkId"`
	NetworkName   string `json:"networkName,omitempty"`
	AccountBank   string `json:"accountBank,omitempty"`
	Country       string `json:"country,omitempty"`
	PhoneNumber   string `json:"phoneNumber,omitempty"`
}

// SettlementInfo contains crypto settlement details for direct settlement mode.
//
// In the request, specify CryptoCurrency, CryptoNetwork, and CryptoAmount.
// In the response, YellowCard populates these plus WalletAddress, CryptoUSDRate,
// CryptoLocalRate, and ExpiresAt.
//
// For Stellar/USDC, YellowCard returns the address and memo as a combined string:
//
//	"walletAddress": "GDTY5CDJ...NL5O_4084650351"
//
// Format: {stellar_address}_{memo} — use ParseStellarWalletAddress() to split.
type SettlementInfo struct {
	WalletAddress   string  `json:"walletAddress,omitempty"`
	CryptoCurrency  string  `json:"cryptoCurrency"`
	CryptoNetwork   string  `json:"cryptoNetwork"`
	CryptoAmount    float64 `json:"cryptoAmount,omitempty"`
	CryptoUSDRate   float64 `json:"cryptoUSDRate,omitempty"`
	CryptoLocalRate float64 `json:"cryptoLocalRate,omitempty"`
	WalletTag       string  `json:"walletTag,omitempty"`
	LnInvoice       string  `json:"lnInvoice,omitempty"`
	ExpiresAt       string  `json:"expiresAt,omitempty"`
}

// PaymentRequest represents a disbursement request to the YellowCard API.
// Either Amount (USD) or LocalAmount must be specified, not both.
// ForceAccept is always set to true to skip the approval window.
//
// For direct settlement, set DirectSettlement=true and include SettlementInfo
// with CryptoCurrency, CryptoNetwork, and CryptoAmount. YellowCard returns
// these plus WalletAddress, CryptoUSDRate, CryptoLocalRate, and ExpiresAt.
type PaymentRequest struct {
	ChannelID        string          `json:"channelId"`
	SequenceID       string          `json:"sequenceId"`
	Reason           string          `json:"reason"`
	Sender           Sender          `json:"sender"`
	Destination      Destination     `json:"destination"`
	CustomerUID      string          `json:"customerUID"`
	CustomerType     string          `json:"customerType"`
	ForceAccept      bool            `json:"forceAccept"`
	Amount           int             `json:"amount,omitempty"`
	LocalAmount      int             `json:"localAmount,omitempty"`
	Currency         string          `json:"currency,omitempty"`
	Country          string          `json:"country,omitempty"`
	DirectSettlement bool            `json:"directSettlement,omitempty"`
	SettlementInfo   *SettlementInfo `json:"settlementInfo,omitempty"`
}

// PaymentResponse is returned when a payment is successfully submitted.
type PaymentResponse struct {
	ID                    string          `json:"id"`
	ChannelID             string          `json:"channelId"`
	SequenceID            string          `json:"sequenceId"`
	Currency              string          `json:"currency"`
	Country               string          `json:"country"`
	Amount                int             `json:"amount"`
	ConvertedAmount       int             `json:"convertedAmount"`
	Rate                  float64         `json:"rate"`
	Reason                string          `json:"reason"`
	Status                string          `json:"status"`
	ForceAccept           bool            `json:"forceAccept"`
	DirectSettlement      bool            `json:"directSettlement"`
	PartnerID             string          `json:"partnerId"`
	RequestSource         string          `json:"requestSource"`
	Attempt               int             `json:"attempt"`
	FiatWallet            string          `json:"fiatWallet"`
	Sender                Sender          `json:"sender"`
	Destination           Destination     `json:"destination"`
	SettlementInfo        *SettlementInfo `json:"settlementInfo,omitempty"`
	Reference             string          `json:"reference,omitempty"`
	NetworkFeeAmountUSD   float64         `json:"networkFeeAmountUSD,omitempty"`
	NetworkFeeAmountLocal float64         `json:"networkFeeAmountLocal,omitempty"`
	ServiceFeeAmountUSD   float64         `json:"serviceFeeAmountUSD,omitempty"`
	ServiceFeeAmountLocal float64         `json:"serviceFeeAmountLocal,omitempty"`
	PartnerFeeAmountUSD   float64         `json:"partnerFeeAmountUSD,omitempty"`
	PartnerFeeAmountLocal float64         `json:"partnerFeeAmountLocal,omitempty"`
	CreatedAt             string          `json:"createdAt"`
	UpdatedAt             string          `json:"updatedAt"`
	ExpiresAt             string          `json:"expiresAt"`
}

// PaymentDetails contains full details of a payment retrieved by ID.
type PaymentDetails struct {
	ID               string          `json:"id"`
	ChannelID        string          `json:"channelId"`
	SequenceID       string          `json:"sequenceId"`
	PartnerID        string          `json:"partnerId"`
	SessionID        string          `json:"sessionId,omitempty"`
	Currency         string          `json:"currency"`
	Country          string          `json:"country"`
	Amount           int             `json:"amount"`
	ConvertedAmount  int             `json:"convertedAmount"`
	Rate             float64         `json:"rate"`
	Reason           string          `json:"reason"`
	Status           string          `json:"status"`
	DirectSettlement bool            `json:"directSettlement"`
	Sender           Sender          `json:"sender"`
	Destination      Destination     `json:"destination"`
	SettlementInfo   *SettlementInfo `json:"settlementInfo,omitempty"`
	CreatedAt        string          `json:"createdAt"`
	UpdatedAt        string          `json:"updatedAt"`
	ExpiresAt        string          `json:"expiresAt"`
}

// WebhookEvent represents an incoming webhook payload from YellowCard.
type WebhookEvent struct {
	Event     string         `json:"event"`
	Timestamp string         `json:"timestamp"`
	Data      WebhookPayload `json:"data"`
}

// WebhookPayload contains the payment data embedded in a webhook event.
type WebhookPayload struct {
	PaymentID        string          `json:"id"`
	SequenceID       string          `json:"sequenceId"`
	Status           string          `json:"status"`
	Amount           int             `json:"amount"`
	ConvertedAmount  int             `json:"convertedAmount"`
	Currency         string          `json:"currency"`
	Country          string          `json:"country"`
	Rate             float64         `json:"rate"`
	DirectSettlement bool            `json:"directSettlement"`
	SettlementInfo   *SettlementInfo `json:"settlementInfo,omitempty"`
	CreatedAt        string          `json:"createdAt"`
	UpdatedAt        string          `json:"updatedAt"`
}

// Account represents a YellowCard business account balance.
type Account struct {
	Available    float64 `json:"available"`
	Currency     string  `json:"currency"`
	CurrencyType string  `json:"currencyType"`
}

// AccountsResponse wraps the accounts array returned from the account endpoint.
type AccountsResponse struct {
	Accounts []Account `json:"accounts"`
}

// APIError represents an error response from the YellowCard API.
type APIError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// Options contains YellowCard-specific payment options passed via InitializePaymentRequest.
type Options struct {
	ChannelID      string      `json:"channel_id"`
	Destination    Destination `json:"destination"`
	Sender         Sender      `json:"sender"`
	Reason         string      `json:"reason"`
	CustomerUID    string      `json:"customer_uid"`
	CustomerType   string      `json:"customer_type"`
	IdempotencyKey string      `json:"idempotency_key"`
}

// YellowCard payment event statuses (full lifecycle).
// See: https://docs.yellowcard.engineering/docs/events-api
const (
	StatusCreated           = "created"
	StatusPendingApproval   = "pending_approval"
	StatusPendingSettlement = "pending_settlement" // Direct settlement only: awaiting crypto payment
	StatusProcess           = "process"
	StatusProcessing        = "processing"
	StatusPendingLiquidity  = "pending_liquidity" // Fiat mode: low YC balance, auto-retries 2hrs
	StatusPending           = "pending"
	StatusComplete          = "complete"
	StatusFailed            = "failed"
	StatusExpired           = "expired"
	StatusCancelled         = "cancelled"
	StatusPendingRefund     = "pending_refund"
	StatusRefundProcessing  = "refund_processing"
	StatusRefunded          = "refunded"
	StatusRefundFailed      = "refund_failed"
)

// Internal disbursement tracking statuses (our system, not YellowCard).
const (
	DisbursementPending         = "pending"
	DisbursementDirectSubmitted = "direct_submitted"
	DisbursementCryptoSent      = "crypto_sent"
	DisbursementFiatSubmitted   = "fiat_submitted"
	DisbursementProcessing      = "processing"
	DisbursementComplete        = "complete"
	DisbursementRefundPending   = "refund_pending"
	DisbursementRefundReceived  = "refund_received"
	DisbursementFailed          = "failed"
)

// Settlement method constants.
const (
	SettlementMethodDirect = "direct"
	SettlementMethodFiat   = "fiat"
)

// Channel type constants.
const (
	ChannelTypeMomo = "momo"
	ChannelTypeBank = "bank"
)

// Ramp type constants.
const (
	RampTypeWithdraw = "withdraw"
)

// Customer type constants.
const (
	CustomerTypeRetail      = "retail"
	CustomerTypeInstitution = "institution"
)

// Webhook event constants.
const (
	EventDisbursementComplete     = "DISBURSEMENT.COMPLETE"
	EventDisbursementFailed       = "DISBURSEMENT.FAILED"
	EventPaymentPendingSettlement = "PAYMENT.PENDING_SETTLEMENT"
	EventPaymentComplete          = "PAYMENT.COMPLETE"
	EventPaymentFailed            = "PAYMENT.FAILED"
	EventCollectionComplete       = "COLLECTION.COMPLETE"
	EventCryptoDeposit            = "CRYPTO.DEPOSIT"
)

// Country code constants (ISO 3166-2).
const (
	CountryKenya   = "KE"
	CountryNigeria = "NG"
	CountryGhana   = "GH"
	CountryUganda  = "UG"
)

// Currency code constants (ISO 4217).
const (
	CurrencyKES = "KES"
	CurrencyNGN = "NGN"
	CurrencyGHS = "GHS"
	CurrencyUGX = "UGX"
	CurrencyUSD = "USD"
)

// Cryptocurrency constants for SettlementInfo.
const (
	CryptoCurrencyUSDC     = "USDC"
	CryptoCurrencyUSDT     = "USDT"
	CryptoNetworkXLM       = "XLM"
	CryptoNetworkTRC20     = "TRC20"
	CryptoNetworkERC20     = "ERC20"
	CryptoNetworkLightning = "LIGHTNING"
)
