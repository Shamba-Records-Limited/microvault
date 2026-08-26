package fonbnk

import (
	"bytes"
	"encoding/json"
	"slices"
	"strconv"
	"time"

	"github.com/samber/lo"
)

// ProviderName is the value carried on every error's provider attribute.
const ProviderName = "fonbnk"

// Currency types.
const (
	CurrencyTypeFiat            = "fiat"
	CurrencyTypeCrypto          = "crypto"
	CurrencyTypeMerchantBalance = "merchant_balance"
)

// Payment channels. Paybill and BuyGoods are Kenya, payout only.
const (
	ChannelBank            = "bank"
	ChannelAirtime         = "airtime"
	ChannelMobileMoney     = "mobile_money"
	ChannelPaybill         = "paybill"
	ChannelBuyGoods        = "buy_goods"
	ChannelDigitalWallet   = "digital_wallet"
	ChannelMerchantBalance = "merchant_balance"
	ChannelCrypto          = "crypto"
)

// Deposit transfer types.
const (
	TransferTypeManual     = "manual"
	TransferTypeRedirect   = "redirect"
	TransferTypeSTKPush    = "stk_push"
	TransferTypeOTPSTKPush = "otp_stk_push"
	TransferTypeUSSD       = "ussd"
)

// Order statuses. Only StatusPayoutSuccessful and StatusRefundSuccessful are
// final; see IsTerminal.
const (
	StatusDepositAwaiting   = "deposit_awaiting"
	StatusDepositValidating = "deposit_validating"
	StatusDepositSuccessful = "deposit_successful"
	StatusDepositInvalid    = "deposit_invalid"
	StatusDepositCanceled   = "deposit_canceled"
	StatusDepositExpired    = "deposit_expired"
	StatusPayoutPending     = "payout_pending"
	StatusPayoutSuccessful  = "payout_successful"
	StatusPayoutFailed      = "payout_failed"
	StatusRefundInitiated   = "refund_initiated"
	StatusRefundPending     = "refund_pending"
	StatusRefundSuccessful  = "refund_successful"
	StatusRefundFailed      = "refund_failed"
)

// IsTerminal reports whether a status can still change.
//
// deposit_canceled and deposit_expired are deliberately excluded: Fonbnk still
// accepts a late payment and runs the payout.
func IsTerminal(status string) bool {
	return status == StatusPayoutSuccessful || status == StatusRefundSuccessful
}

// Order types.
const (
	OrderTypeOnRamp                    = "on_ramp"
	OrderTypeOffRamp                   = "off_ramp"
	OrderTypeSettlement                = "settlement"
	OrderTypeMerchantBalanceDeposit    = "merchant_balance_deposit"
	OrderTypeMerchantBalanceWithdrawal = "merchant_balance_withdrawal"
)

// Fee recipients. Merchant is our revenue, blockchain is gas.
const (
	FeeRecipientMerchant   = "merchant"
	FeeRecipientProvider   = "provider"
	FeeRecipientPlatform   = "platform"
	FeeRecipientBlockchain = "blockchain"
)

// Fee types.
const (
	FeeTypePercentage = "percentage"
	FeeTypeFlatAmount = "flat_amount"
)

// Sandbox-only forced flows, set on Order.FieldsToCreateOrder. Read the
// field's own options array rather than assuming a leg accepts all of these.
const (
	FieldDepositSandboxForcedFlow = "depositSandboxForcedFlow"
	FieldPayoutSandboxForcedFlow  = "payoutSandboxForcedFlow"

	ForcedDepositSuccess      = "deposit_success"
	ForcedDepositInvalid      = "deposit_invalid"
	ForcedDepositUnderpayment = "deposit_underpayment"
	ForcedDepositOverpayment  = "deposit_overpayment"
	ForcedPayoutSuccess       = "payout_success"
	ForcedPayoutFailed        = "payout_failed"
)

// Field keys Fonbnk asks for on the corridors we use.
const (
	FieldPhoneNumber             = "phoneNumber"
	FieldBlockchainWalletAddress = "blockchainWalletAddress"
	FieldBlockchainMemo          = "blockchainMemo"
	FieldBlockchainTxHash        = "blockchainTransactionHash"
	FieldOTPCode                 = "otpCode"
)

// MaxBound is a fee-band or limit ceiling. Fonbnk sends either a number or the
// string "Infinity", so a plain float64 fails to decode the top band of every
// fee table.
type MaxBound struct {
	Value    float64
	Infinite bool
}

// UnmarshalJSON accepts a number or "Infinity".
func (m *MaxBound) UnmarshalJSON(data []byte) error {
	trimmed := bytes.TrimSpace(data)
	if bytes.Equal(trimmed, []byte("null")) {
		return nil
	}
	if trimmed[0] == '"' {
		var s string
		if err := json.Unmarshal(trimmed, &s); err != nil {
			return err
		}
		if s == "Infinity" {
			m.Infinite, m.Value = true, 0
			return nil
		}
		v, err := strconv.ParseFloat(s, 64)
		if err != nil {
			return err
		}
		m.Infinite, m.Value = false, v
		return nil
	}
	m.Infinite = false
	return json.Unmarshal(trimmed, &m.Value)
}

// MarshalJSON round-trips the "Infinity" sentinel.
func (m MaxBound) MarshalJSON() ([]byte, error) {
	if m.Infinite {
		return []byte(`"Infinity"`), nil
	}
	return json.Marshal(m.Value)
}

// Covers reports whether amount falls in [min, max].
func (m MaxBound) Covers(amount float64) bool {
	return m.Infinite || amount <= m.Value
}

// FeeSetting is one fee rule and the amount band it applies to. MinCap and
// MaxCap bound the fee itself and are only set on percentage rules.
type FeeSetting struct {
	ID        string   `json:"id"`
	Recipient string   `json:"recipient"`
	Type      string   `json:"type"`
	Value     float64  `json:"value"`
	Min       float64  `json:"min"`
	Max       MaxBound `json:"max"`
	MinCap    *float64 `json:"minCap,omitempty"`
	MaxCap    *float64 `json:"maxCap,omitempty"`
}

// ChargedFee is what one rule actually cost.
type ChargedFee struct {
	ID        string  `json:"id"`
	Type      string  `json:"type"`
	Recipient string  `json:"recipient"`
	Amount    float64 `json:"amount"`
}

// Cashout is the pricing of one leg.
//
// ExchangeRateAfterFees is amountBeforeFees/amountAfterFeesUsd — a per-leg
// figure, not what the user pays or receives. Use EffectiveRate on Quote.
type Cashout struct {
	Rate                       float64            `json:"exchangeRate"`
	RateAfterFees              float64            `json:"exchangeRateAfterFees"`
	AmountBeforeFees           float64            `json:"amountBeforeFees"`
	AmountAfterFees            float64            `json:"amountAfterFees"`
	AmountBeforeFeesUSD        float64            `json:"amountBeforeFeesUsd"`
	AmountAfterFeesUSD         float64            `json:"amountAfterFeesUsd"`
	FeeSettings                []FeeSetting       `json:"feeSettings"`
	ChargedFees                []ChargedFee       `json:"chargedFees"`
	ChargedFeesUSD             []ChargedFee       `json:"chargedFeesUsd"`
	TotalChargedFees           float64            `json:"totalChargedFees"`
	TotalChargedFeesUSD        float64            `json:"totalChargedFeesUsd"`
	ChargedFeesPerRecipient    map[string]float64 `json:"chargedFeesPerRecipient"`
	ChargedFeesPerRecipientUSD map[string]float64 `json:"chargedFeesPerRecipientUsd"`
}

// Carrier is the mobile-money or airtime operator chosen for an order.
type Carrier struct {
	ID   string `json:"_id,omitempty"`
	Code string `json:"code"`
	Name string `json:"name"`
}

// CurrencyDetails flattens Fonbnk's crypto, fiat and merchant-balance detail
// shapes. ContractAddress is omitted on a native asset, never null.
type CurrencyDetails struct {
	Network         string   `json:"network,omitempty"`
	Asset           string   `json:"asset,omitempty"`
	ContractAddress string   `json:"contractAddress,omitempty"`
	CountryIsoCode  string   `json:"countryIsoCode,omitempty"`
	Carrier         *Carrier `json:"carrier,omitempty"`
	MerchantName    string   `json:"merchantName,omitempty"`
}

// FieldOption is one choice on an enum RequiredField.
type FieldOption struct {
	Value    string `json:"value"`
	Label    string `json:"label"`
	IconURL  string `json:"iconUrl,omitempty"`
	Featured bool   `json:"featured,omitempty"`
}

// RequiredField is one input Fonbnk wants collected. For an enum field,
// Options is the authoritative list of accepted values.
type RequiredField struct {
	Key          string        `json:"key"`
	Type         string        `json:"type"`
	Label        string        `json:"label"`
	Required     bool          `json:"required"`
	Options      []FieldOption `json:"options,omitempty"`
	DefaultValue string        `json:"defaultValue,omitempty"`
}

// TransferDetail is one line of pay-to instructions. Render as it arrives —
// the id set grows.
type TransferDetail struct {
	ID          string `json:"id"`
	Label       string `json:"label"`
	Description string `json:"description,omitempty"`
	Value       string `json:"value,omitempty"`
}

// Transfer detail ids seen on the corridors we use.
const (
	DetailRecipientWalletAddress = "recipientWalletAddress"
	DetailSenderWalletAddress    = "senderWalletAddress"
	DetailAmountToSend           = "amountToSend"
	DetailRecipientPhoneNumber   = "recipientPhoneNumber"
	DetailBankNarration          = "bankTransferNarration"
)

// TransferInstructions is the union of Fonbnk's five transfer-type shapes,
// discriminated by Type. Fields outside a type's own variant stay zero.
type TransferInstructions struct {
	Type             string           `json:"type"`
	InstructionsText string           `json:"instructionsText"`
	WarningText      string           `json:"warningText,omitempty"`
	TransferDetails  []TransferDetail `json:"transferDetails"`

	// FieldsToConfirmOrder must be passed to ConfirmOrder. A crypto deposit
	// asks for blockchainTransactionHash here.
	FieldsToConfirmOrder []RequiredField `json:"fieldsToConfirmOrder"`

	// PaymentURL is set on a redirect transfer.
	PaymentURL             string `json:"paymentUrl,omitempty"`
	RedirectedToPaymentURL bool   `json:"redirectedToPaymentUrl,omitempty"`

	// UssdCode is set on a ussd transfer and may contain a {pin} placeholder.
	UssdCode string `json:"ussdCode,omitempty"`

	// Intermediate-action state, on stk_push and otp_stk_push.
	IsIntermediateActionAvailable            bool            `json:"isIntermediateActionAvailable,omitempty"`
	IntermediateActionRequired               bool            `json:"intermediateActionRequired,omitempty"`
	IntermediateActionExecuted               bool            `json:"intermediateActionExecuted,omitempty"`
	IntermediateActionButtonText             string          `json:"intermediateActionButtonText,omitempty"`
	IntermediateActionMaxAttempts            int             `json:"intermediateActionMaxAttempts,omitempty"`
	IntermediateActionAttempts               int             `json:"intermediateActionAttempts,omitempty"`
	IntermediateActionNextAttemptAvailableAt *time.Time      `json:"intermediateActionNextAttemptAvailableAt,omitempty"`
	IntermediateActionTimeoutMs              int             `json:"intermediateActionTimeoutMs,omitempty"`
	FieldsForIntermediateAction              []RequiredField `json:"fieldsForIntermediateAction,omitempty"`

	// OtpChannel is how the code reached the user: sms, ussd, email or
	// whatsapp. Do not assume sms.
	OtpChannel       string `json:"otpChannel,omitempty"`
	OtpAssociativeID string `json:"otpAssociativeId,omitempty"`
}

// CanRetryIntermediateAction reports whether another push or OTP submission is
// allowed right now.
//
// Calling TriggerIntermediateAction once attempts have reached the cap expires
// the order, so this gate is not advisory.
func (t TransferInstructions) CanRetryIntermediateAction(now time.Time) bool {
	if !t.IsIntermediateActionAvailable {
		return false
	}
	if t.IntermediateActionMaxAttempts > 0 && t.IntermediateActionAttempts >= t.IntermediateActionMaxAttempts {
		return false
	}
	if t.IntermediateActionNextAttemptAvailableAt != nil && now.Before(*t.IntermediateActionNextAttemptAvailableAt) {
		return false
	}
	return true
}

// TransactionMeta is the on-chain or provider reference for a settled leg.
type TransactionMeta struct {
	TransactionHash              string `json:"transactionHash,omitempty"`
	FromAddress                  string `json:"fromAddress,omitempty"`
	ToAddress                    string `json:"toAddress,omitempty"`
	PaymentChannelAdditionalInfo string `json:"paymentChannelAdditionalInfo,omitempty"`
}

// LegTransaction is present only once a leg has moved funds.
type LegTransaction struct {
	Meta *TransactionMeta `json:"meta,omitempty"`
}

// FormattedUserField is a provided field echoed back labelled for display.
type FormattedUserField struct {
	Key   string `json:"key"`
	Label string `json:"label"`
	Value any    `json:"value"`
	Type  string `json:"type"`
}

// OrderLeg is one side of an order as Fonbnk reports it.
type OrderLeg struct {
	PaymentChannel  string          `json:"paymentChannel"`
	CurrencyType    string          `json:"currencyType"`
	CurrencyCode    string          `json:"currencyCode"`
	CurrencyDetails CurrencyDetails `json:"currencyDetails"`
	Cashout         Cashout         `json:"cashout"`

	ProvidedFieldsToCreateOrder      map[string]string    `json:"providedFieldsToCreateOrder,omitempty"`
	ProvidedFieldsToConfirmOrder     map[string]string    `json:"providedFieldsToConfirmOrder,omitempty"`
	FormattedUserFieldsToCreateOrder []FormattedUserField `json:"formattedUserFieldsToCreateOrder,omitempty"`

	// TransferInstructions is populated on the deposit leg only.
	TransferInstructions *TransferInstructions `json:"transferInstructions,omitempty"`
	Transaction          *LegTransaction       `json:"transaction,omitempty"`
}

// StatusChangeLog is one recorded transition. OldStatus is absent on a
// deposit_canceled entry.
type StatusChangeLog struct {
	OldStatus string    `json:"oldStatus,omitempty"`
	NewStatus string    `json:"newStatus"`
	Date      time.Time `json:"date"`
}

// Order is the canonical order object. Every order endpoint returns it.
type Order struct {
	ID                  string `json:"_id"`
	CountryIsoCode      string `json:"countryIsoCode"`
	UserID              string `json:"userId"`
	UserEmail           string `json:"userEmail"`
	MerchantOrderParams string `json:"merchantOrderParams,omitempty"`
	Status              string `json:"status"`
	Type                string `json:"type,omitempty"`
	Flow                string `json:"flow,omitempty"`
	Source              string `json:"source,omitempty"`

	Deposit OrderLeg  `json:"deposit"`
	Payout  OrderLeg  `json:"payout"`
	Refund  *OrderLeg `json:"refund,omitempty"`

	StatusChangeLogs []StatusChangeLog `json:"statusChangeLogs"`

	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
	ExpiresAt time.Time `json:"expiresAt"`
}

// LegSpec names one side of a requested quote or order. Set Amount on exactly
// one of the two legs.
type LegSpec struct {
	PaymentChannel string   `json:"paymentChannel"`
	CurrencyType   string   `json:"currencyType"`
	CurrencyCode   string   `json:"currencyCode"`
	CountryIsoCode string   `json:"countryIsoCode,omitempty"`
	CarrierCode    string   `json:"carrierCode,omitempty"`
	Amount         *float64 `json:"amount,omitempty"`
}

// QuoteDepositLeg is a quote's deposit side. TransferType pins the deposit
// shape and narrows the offer search; leaving it unset lets Fonbnk choose.
//
// Fonbnk rejects unknown keys, so this field exists on the quote deposit leg
// only.
type QuoteDepositLeg struct {
	LegSpec
	TransferType string `json:"transferType,omitempty"`
}

// QuoteRequest prices one corridor.
type QuoteRequest struct {
	Deposit QuoteDepositLeg `json:"deposit"`
	Payout  LegSpec         `json:"payout"`
}

// QuoteLeg is one side of a quote response.
type QuoteLeg struct {
	PaymentChannel      string          `json:"paymentChannel"`
	CurrencyType        string          `json:"currencyType"`
	CurrencyCode        string          `json:"currencyCode"`
	CurrencyDetails     CurrencyDetails `json:"currencyDetails"`
	Cashout             Cashout         `json:"cashout"`
	FieldsToCreateOrder []RequiredField `json:"fieldsToCreateOrder"`
	TransferType        string          `json:"transferType,omitempty"`
}

// Quote is a locked price and the fields an order from it must carry.
type Quote struct {
	QuoteID        string    `json:"quoteId"`
	QuoteExpiresAt time.Time `json:"quoteExpiresAt"`
	Deposit        QuoteLeg  `json:"deposit"`
	Payout         QuoteLeg  `json:"payout"`
}

// Direction reports OrderTypeOffRamp or OrderTypeOnRamp from the leg currency
// types, or "" for a corridor that is neither.
func (q Quote) Direction() string {
	return directionOf(q.Deposit.CurrencyType, q.Payout.CurrencyType)
}

// EffectiveRate is the fiat that changes hands per unit of crypto, after fees
// on both legs. Maximise it on an off-ramp, minimise it on an on-ramp.
//
// This is the figure to compare across providers. Cashout.RateAfterFees is
// not: it mixes a pre-fee local amount with a post-fee USD one and moves
// opposite to the user's outcome.
func (q Quote) EffectiveRate() (float64, bool) {
	switch q.Direction() {
	case OrderTypeOffRamp:
		return ratio(q.Payout.Cashout.AmountAfterFees, q.Deposit.Cashout.AmountBeforeFees)
	case OrderTypeOnRamp:
		return ratio(q.Deposit.Cashout.AmountBeforeFees, q.Payout.Cashout.AmountAfterFees)
	}
	return 0, false
}

func ratio(numerator, denominator float64) (float64, bool) {
	if numerator <= 0 || denominator <= 0 {
		return 0, false
	}
	return numerator / denominator, true
}

// RequiredFieldKeys returns the keys both legs mark required.
func (q Quote) RequiredFieldKeys() []string {
	fields := slices.Concat(q.Deposit.FieldsToCreateOrder, q.Payout.FieldsToCreateOrder)
	return lo.FilterMap(fields, func(f RequiredField, _ int) (string, bool) {
		return f.Key, f.Required
	})
}

// CreateOrderRequest opens an order, optionally at a quoted price.
//
// FieldsToCreateOrder is the flat union of both quote legs' fields.
type CreateOrderRequest struct {
	QuoteID             string            `json:"quoteId,omitempty"`
	UserEmail           string            `json:"userEmail"`
	UserCountryIsoCode  string            `json:"userCountryIsoCode"`
	UserIP              string            `json:"userIp"`
	Deposit             LegSpec           `json:"deposit"`
	Payout              LegSpec           `json:"payout"`
	FieldsToCreateOrder map[string]string `json:"fieldsToCreateOrder,omitempty"`
	// OrderParams is our own reference, echoed back as merchantOrderParams and
	// searchable through GetOrderByParams.
	OrderParams string `json:"orderParams,omitempty"`
	CallbackURL string `json:"callbackUrl,omitempty"`
	// WebhookURL overrides the dashboard's global URL for this order.
	WebhookURL string `json:"webhookUrl,omitempty"`
}

// CreateOrderResponse wraps the order and reports whether the quote was used.
type CreateOrderResponse struct {
	QuoteUsed bool  `json:"quoteUsed"`
	Order     Order `json:"order"`
}

// PaymentChannelInfo is one channel a currency supports.
//
// TransferTypes is collected from every offer including disabled ones, so it
// is not gated by IsDepositAllowed.
type PaymentChannelInfo struct {
	Name             string    `json:"name"`
	Type             string    `json:"type"`
	TransferTypes    []string  `json:"transferTypes"`
	IsDepositAllowed bool      `json:"isDepositAllowed"`
	IsPayoutAllowed  bool      `json:"isPayoutAllowed"`
	Carriers         []Carrier `json:"carriers,omitempty"`
}

// Currency is one tradable currency. Fiat currencies appear once per country.
type Currency struct {
	CurrencyType    string               `json:"currencyType"`
	CurrencyCode    string               `json:"currencyCode"`
	PaymentChannels []PaymentChannelInfo `json:"paymentChannels"`
	CurrencyDetails CurrencyDetails      `json:"currencyDetails"`
	Pairs           []string             `json:"pairs"`
}

// LimitSide is the min/max window for one leg. Step and SupportsDecimals drive
// amount rounding.
type LimitSide struct {
	Min              float64 `json:"min"`
	Max              float64 `json:"max"`
	MinUSD           float64 `json:"minUsd"`
	MaxUSD           float64 `json:"maxUsd"`
	Step             float64 `json:"step"`
	SupportsDecimals bool    `json:"supportsDecimals"`
}

// OrderLimits is the tradable window for one corridor.
type OrderLimits struct {
	Deposit LimitSide `json:"deposit"`
	Payout  LimitSide `json:"payout"`
}

// IsTradable reports whether the corridor is live. Fonbnk answers 200 with
// every field zero when no provider can price the pair.
func (l OrderLimits) IsTradable() bool {
	return l.Deposit.Max > 0 || l.Payout.Max > 0
}

// OrderLimitsQuery names the corridor to look limits up for.
type OrderLimitsQuery struct {
	DepositPaymentChannel string
	DepositCurrencyType   string
	DepositCurrencyCode   string
	DepositCarrierCode    string
	DepositCountryIsoCode string
	PayoutPaymentChannel  string
	PayoutCurrencyType    string
	PayoutCurrencyCode    string
	PayoutCarrierCode     string
	PayoutCountryIsoCode  string
}

// APIError is Fonbnk's error body.
type APIError struct {
	Message string `json:"message"`
	Code    string `json:"code"`
	Error   string `json:"error"`
}
