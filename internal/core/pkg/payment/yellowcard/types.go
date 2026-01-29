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

// PaymentRequest represents a disbursement request to the YellowCard API.
// Either Amount (USD) or LocalAmount must be specified, not both.
// ForceAccept set to default True to skip accepting payment.
type PaymentRequest struct {
	ChannelID    string      `json:"channelId"`
	SequenceID   string      `json:"sequenceId"`
	Reason       string      `json:"reason"`
	Sender       Sender      `json:"sender"`
	Destination  Destination `json:"destination"`
	CustomerUID  string      `json:"customerUID"`
	CustomerType string      `json:"customerType"`
	ForceAccept  bool        `json:"forceAccept"`
	Amount       int         `json:"amount,omitempty"`
	LocalAmount  int         `json:"localAmount,omitempty"`
}

// PaymentResponse is returned when a payment is successfully submitted.
type PaymentResponse struct {
	ID               string      `json:"id"`
	ChannelID        string      `json:"channelId"`
	SequenceID       string      `json:"sequenceId"`
	Currency         string      `json:"currency"`
	Country          string      `json:"country"`
	Amount           int         `json:"amount"`
	ConvertedAmount  int         `json:"convertedAmount"`
	Rate             float64     `json:"rate"`
	Reason           string      `json:"reason"`
	Status           string      `json:"status"`
	ForceAccept      bool        `json:"forceAccept"`
	DirectSettlement bool        `json:"directSettlement"`
	PartnerID        string      `json:"partnerId"`
	RequestSource    string      `json:"requestSource"`
	Attempt          int         `json:"attempt"`
	FiatWallet       string      `json:"fiatWallet"`
	Sender           Sender      `json:"sender"`
	Destination      Destination `json:"destination"`
	CreatedAt        string      `json:"createdAt"`
	UpdatedAt        string      `json:"updatedAt"`
	ExpiresAt        string      `json:"expiresAt"`
}

// PaymentDetails contains full details of a payment retrieved by ID.
type PaymentDetails struct {
	ID              string      `json:"id"`
	ChannelID       string      `json:"channelId"`
	SequenceID      string      `json:"sequenceId"`
	PartnerID       string      `json:"partnerId"`
	SessionID       string      `json:"sessionId,omitempty"`
	Currency        string      `json:"currency"`
	Country         string      `json:"country"`
	Amount          int         `json:"amount"`
	ConvertedAmount int         `json:"convertedAmount"`
	Rate            float64     `json:"rate"`
	Reason          string      `json:"reason"`
	Status          string      `json:"status"`
	Sender          Sender      `json:"sender"`
	Destination     Destination `json:"destination"`
	CreatedAt       string      `json:"createdAt"`
	UpdatedAt       string      `json:"updatedAt"`
	ExpiresAt       string      `json:"expiresAt"`
}

// WebhookEvent represents an incoming webhook payload from YellowCard.
type WebhookEvent struct {
	Event     string         `json:"event"`
	Timestamp string         `json:"timestamp"`
	Data      WebhookPayload `json:"data"`
}

// WebhookPayload contains the payment data embedded in a webhook event.
type WebhookPayload struct {
	PaymentID       string  `json:"id"`
	SequenceID      string  `json:"sequenceId"`
	Status          string  `json:"status"`
	Amount          int     `json:"amount"`
	ConvertedAmount int     `json:"convertedAmount"`
	Currency        string  `json:"currency"`
	Country         string  `json:"country"`
	Rate            float64 `json:"rate"`
	CreatedAt       string  `json:"createdAt"`
	UpdatedAt       string  `json:"updatedAt"`
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

// Payment status constants.
const (
	StatusCreated    = "created"
	StatusPending    = "pending"
	StatusProcessing = "processing"
	StatusComplete   = "complete"
	StatusFailed     = "failed"
	StatusExpired    = "expired"
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
	EventDisbursementComplete = "DISBURSEMENT.COMPLETE"
	EventDisbursementFailed   = "DISBURSEMENT.FAILED"
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
