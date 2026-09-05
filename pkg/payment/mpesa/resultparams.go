package mpesa

import (
	"encoding/json"
	"strconv"
	"strings"
	"time"

	pkgErrors "github.com/Shamba-Records-Limited/microvault/pkg/errors"
)

// FlexibleInt64 decodes a JSON number or a quoted number.
//
// Daraja is inconsistent about which it sends, and the inconsistency follows
// success and failure rather than the endpoint: ResultCode arrives as a number
// on a successful reversal and as a string on a failed one. A plain int64 field
// therefore fails to unmarshal exactly when something has already gone wrong.
type FlexibleInt64 int64

// UnmarshalJSON accepts 0, "0", "" and null.
func (f *FlexibleInt64) UnmarshalJSON(raw []byte) error {
	text := strings.Trim(strings.TrimSpace(string(raw)), `"`)
	if text == "" || text == "null" {
		*f = 0
		return nil
	}
	parsed, err := strconv.ParseInt(text, 10, 64)
	if err != nil {
		return err
	}
	*f = FlexibleInt64(parsed)
	return nil
}

// Int64 is the decoded value.
func (f FlexibleInt64) Int64() int64 { return int64(f) }

// Result is the asynchronous envelope every Initiator-bearing endpoint posts to
// a ResultURL or a QueueTimeOutURL.
type Result struct {
	ResultType               int64
	ResultCode               int64
	ResultDesc               string
	OriginatorConversationID string
	ConversationID           string
	TransactionID            string

	// Parameters holds ResultParameters.ResultParameter.
	Parameters Parameters

	// Reference holds ReferenceData.ReferenceItem.
	Reference Parameters
}

// Succeeded reports whether Daraja processed the request. Zero is the only
// success; every other code means something else happened.
func (r Result) Succeeded() bool { return r.ResultCode == 0 }

type resultEnvelope struct {
	Result rawResult `json:"Result"`
}

type rawResult struct {
	ResultType               FlexibleInt64 `json:"ResultType"`
	ResultCode               FlexibleInt64 `json:"ResultCode"`
	ResultDesc               string        `json:"ResultDesc"`
	OriginatorConversationID string        `json:"OriginatorConversationID"`
	ConversationID           string        `json:"ConversationID"`
	TransactionID            string        `json:"TransactionID"`

	ResultParameters *struct {
		ResultParameter json.RawMessage `json:"ResultParameter"`
	} `json:"ResultParameters"`

	ReferenceData *struct {
		ReferenceItem json.RawMessage `json:"ReferenceItem"`
	} `json:"ReferenceData"`
}

// keyValue is one entry of a Daraja parameter list. Value is held raw because
// it may be a string, a number, or absent.
type keyValue struct {
	Key   string          `json:"Key"`
	Value json.RawMessage `json:"Value"`
}

// Parameters is a decoded Daraja key/value list. It is a multi-map because
// Daraja repeats keys — an Account Balance result carries one entry per
// account under the same key.
type Parameters map[string][]string

// decodeParameters reads a Daraja parameter list that may be a JSON object or a
// JSON array of objects.
//
// Both forms occur for both fields, and which one appears depends on the
// endpoint and on whether the call succeeded. The reference SDK declares
// ResultParameter as array-only and ReferenceItem as object-only, so each is
// correct for some endpoints and fails to unmarshal for others.
func decodeParameters(raw json.RawMessage) Parameters {
	params := make(Parameters)

	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" || trimmed == "null" {
		return params
	}

	var entries []keyValue
	switch trimmed[0] {
	case '[':
		if err := json.Unmarshal(raw, &entries); err != nil {
			return params
		}
	case '{':
		var single keyValue
		if err := json.Unmarshal(raw, &single); err != nil {
			return params
		}
		entries = []keyValue{single}
	default:
		return params
	}

	for _, entry := range entries {
		if entry.Key == "" {
			continue
		}
		params[entry.Key] = append(params[entry.Key], scalarString(entry.Value))
	}
	return params
}

// scalarString renders a raw JSON scalar as text, unquoting strings and leaving
// numbers as written.
func scalarString(raw json.RawMessage) string {
	text := strings.TrimSpace(string(raw))
	if text == "" || text == "null" {
		return ""
	}
	var asString string
	if err := json.Unmarshal(raw, &asString); err == nil {
		return asString
	}
	return text
}

// ParseResult decodes an asynchronous result envelope.
func ParseResult(raw []byte) (*Result, error) {
	var envelope resultEnvelope
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return nil, mpesaErr("parse_result").
			Code(pkgErrors.CodeDecodeFailed).
			Wrapf(err, "could not decode the result envelope")
	}

	result := &Result{
		ResultType:               envelope.Result.ResultType.Int64(),
		ResultCode:               envelope.Result.ResultCode.Int64(),
		ResultDesc:               envelope.Result.ResultDesc,
		OriginatorConversationID: envelope.Result.OriginatorConversationID,
		ConversationID:           envelope.Result.ConversationID,
		TransactionID:            envelope.Result.TransactionID,
		Parameters:               make(Parameters),
		Reference:                make(Parameters),
	}
	if envelope.Result.ResultParameters != nil {
		result.Parameters = decodeParameters(envelope.Result.ResultParameters.ResultParameter)
	}
	if envelope.Result.ReferenceData != nil {
		result.Reference = decodeParameters(envelope.Result.ReferenceData.ReferenceItem)
	}
	return result, nil
}

// Get returns the first value for key.
func (p Parameters) Get(key string) (string, bool) {
	values, ok := p[key]
	if !ok || len(values) == 0 {
		return "", false
	}
	return values[0], true
}

// All returns every value for key, in arrival order.
func (p Parameters) All(key string) []string { return p[key] }

// Int returns the first value for key as an integer.
func (p Parameters) Int(key string) (int64, bool) {
	value, ok := p.Get(key)
	if !ok {
		return 0, false
	}
	parsed, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
	if err != nil {
		return 0, false
	}
	return parsed, true
}

// Minor returns the first value for key as minor units — cents — so money is
// never held as a float.
func (p Parameters) Minor(key string) (int64, bool) {
	value, ok := p.Get(key)
	if !ok {
		return 0, false
	}
	parsed, err := parseMinor(value)
	if err != nil {
		return 0, false
	}
	return parsed, true
}

// Daraja timestamp layouts. One concept, three renderings: B2B sends
// 20221110110717, B2C sends 06.07.2024 22:48:52, and M-Pesa Express sends
// 20191219102115.
var timestampLayouts = []string{
	"20060102150405",
	"02.01.2006 15:04:05",
	"2006-01-02 15:04:05",
	"2006-01-02T15:04:05",
}

// Time returns the first value for key as a timestamp, trying each layout
// Daraja is known to use.
func (p Parameters) Time(key string) (time.Time, bool) {
	value, ok := p.Get(key)
	if !ok {
		return time.Time{}, false
	}
	return ParseTimestamp(value)
}

// ParseTimestamp decodes any of the timestamp renderings Daraja uses.
func ParseTimestamp(value string) (time.Time, bool) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return time.Time{}, false
	}
	for _, layout := range timestampLayouts {
		if parsed, err := time.Parse(layout, trimmed); err == nil {
			return parsed, true
		}
	}
	return time.Time{}, false
}

// AccountBalance is one account out of a Daraja balance string.
type AccountBalance struct {
	Name      string
	Currency  string
	Available int64
	Uncleared int64
	Reserved  int64
}

// Balances returns the first value for key parsed as Daraja's pipe-delimited
// balance encoding.
func (p Parameters) Balances(key string) ([]AccountBalance, bool) {
	value, ok := p.Get(key)
	if !ok {
		return nil, false
	}
	balances := ParseBalances(value)
	return balances, len(balances) > 0
}

// ParseBalances decodes Daraja's balance encoding, which is not JSON:
//
//	Working Account|KES|346568.83|6186.83|340382.00|0.00
//	Working Account|KES|700000.00|0.00|0.00|0.00&Utility Account|KES|228037.00|...
//
// Accounts are separated by &, fields by |. Parsing is deliberately lenient:
// this arrives after the money has already moved, so a field Safaricom changes
// must not be able to fail a transaction.
func ParseBalances(value string) []AccountBalance {
	var balances []AccountBalance

	for _, chunk := range strings.Split(value, "&") {
		fields := strings.Split(strings.TrimSpace(chunk), "|")
		if len(fields) < 2 || strings.TrimSpace(fields[0]) == "" {
			continue
		}
		balance := AccountBalance{
			Name:     strings.TrimSpace(fields[0]),
			Currency: strings.TrimSpace(fields[1]),
		}
		amounts := []*int64{&balance.Available, &balance.Uncleared, &balance.Reserved}
		for i, target := range amounts {
			if len(fields) <= i+2 {
				break
			}
			if parsed, err := parseMinor(fields[i+2]); err == nil {
				*target = parsed
			}
		}
		balances = append(balances, balance)
	}
	return balances
}

// WrappedAmount is Daraja's other non-JSON encoding:
//
//	{Amount={CurrencyCode=KES, MinimumAmount=618683, BasicAmount=6186.83}}
//
// MinimumAmount is the value in minor units and BasicAmount the same figure
// with a decimal point, so the two disagree by a factor of a hundred by design.
type WrappedAmount struct {
	CurrencyCode string
	Minor        int64
}

// WrappedAmount returns the first value for key parsed as the brace encoding.
func (p Parameters) WrappedAmount(key string) (WrappedAmount, bool) {
	value, ok := p.Get(key)
	if !ok {
		return WrappedAmount{}, false
	}
	return ParseWrappedAmount(value)
}

// ParseWrappedAmount decodes the brace-and-equals encoding. Lenient for the
// same reason as ParseBalances.
func ParseWrappedAmount(value string) (WrappedAmount, bool) {
	trimmed := strings.Trim(strings.TrimSpace(value), "{}")
	if trimmed == "" {
		return WrappedAmount{}, false
	}
	if index := strings.Index(trimmed, "{"); index >= 0 {
		trimmed = strings.Trim(trimmed[index:], "{}")
	}

	var amount WrappedAmount
	var found bool
	for _, field := range strings.Split(trimmed, ",") {
		key, raw, ok := strings.Cut(field, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		raw = strings.TrimSpace(raw)

		switch key {
		case "CurrencyCode":
			amount.CurrencyCode = raw
			found = true
		case "MinimumAmount":
			if parsed, err := strconv.ParseInt(raw, 10, 64); err == nil {
				amount.Minor = parsed
				found = true
			}
		case "BasicAmount":
			if amount.Minor == 0 {
				if parsed, err := parseMinor(raw); err == nil {
					amount.Minor = parsed
					found = true
				}
			}
		}
	}
	return amount, found
}

// parseMinor converts a decimal string to minor units without going through a
// float. "6186.83" is 618683 cents; "6186.8" is 618680.
func parseMinor(value string) (int64, error) {
	text := strings.TrimSpace(value)
	if text == "" {
		return 0, strconv.ErrSyntax
	}

	negative := strings.HasPrefix(text, "-")
	text = strings.TrimPrefix(strings.TrimPrefix(text, "-"), "+")

	whole, fraction, _ := strings.Cut(text, ".")
	if whole == "" {
		whole = "0"
	}
	fraction = (fraction + "00")[:2]

	units, err := strconv.ParseInt(whole, 10, 64)
	if err != nil {
		return 0, err
	}
	cents, err := strconv.ParseInt(fraction, 10, 64)
	if err != nil {
		return 0, err
	}

	minor := units*100 + cents
	if negative {
		minor = -minor
	}
	return minor, nil
}
