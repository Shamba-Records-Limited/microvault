package mpesa

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	pkgErrors "github.com/Shamba-Records-Limited/microvault/pkg/errors"
)

// Pull Transaction endpoints.
const (
	pathPullRegister = "/pulltransactions/v1/register"
	pathPullQuery    = "/pulltransactions/v1/query"
)

// Pull response statuses.
const (
	pullRegistered        = "1000"
	pullAlreadyRegistered = "1001"
	pullQuerySuccess      = "1000"
	pullQueryNoResults    = "1001"
)

// pullTimeLayout is Daraja's Pull window format. The documented samples are
// inconsistent about zero padding — "2020-08-04 8:36:00" alongside
// "2020-08-16 10:10:000" — so requests are rendered padded and responses are
// parsed leniently.
const pullTimeLayout = "2006-01-02 15:04:05"

// PullRegisterRequest binds a shortcode to the Pull API.
type PullRegisterRequest struct {
	// Shortcode defaults to the configured collection shortcode.
	Shortcode uint

	// NominatedNumber is the number Safaricom associates with the
	// registration.
	NominatedNumber string

	CallbackURL string
}

// PullRegisterResponse acknowledges a Pull registration.
type PullRegisterResponse struct {
	ResponseRefID       string `json:"ResponseRefID"`
	ResponseStatus      string `json:"ResponseStatus"`
	ShortCode           string `json:"ShortCode"`
	ResponseDescription string `json:"ResponseDescription"`
}

// AlreadyRegistered reports whether the shortcode was already bound, which is a
// success for our purposes rather than a failure.
func (r PullRegisterResponse) AlreadyRegistered() bool {
	return r.ResponseStatus == pullAlreadyRegistered
}

// PullRegister binds a shortcode to the Pull API.
func (c *Client) PullRegister(ctx context.Context, req PullRegisterRequest) (*PullRegisterResponse, error) {
	errb := mpesaErr("pull_register")

	if req.Shortcode == 0 {
		req.Shortcode = c.collectionShortcode
	}
	if req.CallbackURL == "" {
		return nil, errb.Code(pkgErrors.CodeMissingDependency).Errorf("callback URL is required")
	}
	if err := AssertCallbackURL(req.CallbackURL); err != nil {
		return nil, err
	}

	body := map[string]string{
		"ShortCode":       strconv.FormatUint(uint64(req.Shortcode), 10),
		"RequestType":     "Pull",
		"NominatedNumber": req.NominatedNumber,
		"CallBackURL":     req.CallbackURL,
	}
	return call[PullRegisterResponse](ctx, c, errb.With("shortcode", req.Shortcode), http.MethodPost, pathPullRegister, body)
}

// PulledTransaction is one settled payment as Pull reports it.
//
// Unlike a C2B confirmation, msisdn here is not masked. That makes Pull the
// only route to the payer's actual number, and therefore the compliance path
// rather than a reconciliation convenience.
type PulledTransaction struct {
	TransactionID   string
	CompletedAt     time.Time
	MSISDN          string
	TransactionType string
	BillReference   string
	AmountMinor     int64
	Organization    string
}

type pullQueryResponse struct {
	ResponseRefID   string          `json:"ResponseRefID"`
	ResponseCode    string          `json:"ResponseCode"`
	ResponseMessage string          `json:"ResponseMessage"`
	Response        json.RawMessage `json:"Response"`
}

type pulledTransactionWire struct {
	TransactionID   string `json:"transactionId"`
	TrxDate         string `json:"trxDate"`
	MSISDN          any    `json:"msisdn"`
	TransactionType string `json:"transactiontype"`
	BillReference   string `json:"billreference"`
	Amount          any    `json:"amount"`
	Organization    string `json:"organizationname"`
}

// PullTransactions fetches settled payments in a window.
//
// An empty window is reported as response code 1001 with a Response body of the
// string "[[]]" rather than an empty array. That is a success — no payments
// happened — and treating it as an error makes every quiet hour look like an
// outage.
func (c *Client) PullTransactions(ctx context.Context, from, to time.Time, offset int, shortcode uint) ([]PulledTransaction, error) {
	errb := mpesaErr("pull_query").With("offset", offset)

	if shortcode == 0 {
		shortcode = c.collectionShortcode
	}
	if !to.After(from) {
		return nil, errb.Code(pkgErrors.CodeBuildFailed).Errorf("the window end must be after its start")
	}

	body := map[string]string{
		"ShortCode":   strconv.FormatUint(uint64(shortcode), 10),
		"StartDate":   from.Format(pullTimeLayout),
		"EndDate":     to.Format(pullTimeLayout),
		"OffSetValue": strconv.Itoa(offset),
	}

	response, err := call[pullQueryResponse](ctx, c, errb, http.MethodPost, pathPullQuery, body)
	if err != nil {
		return nil, err
	}
	if response.ResponseCode == pullQueryNoResults {
		return nil, nil
	}
	if response.ResponseCode != pullQuerySuccess && response.ResponseCode != "" {
		return nil, errb.
			Code(pkgErrors.CodeHTTPError).
			With("daraja_code", response.ResponseCode).
			Errorf("Daraja refused the pull query")
	}
	return decodePulled(response.Response), nil
}

// decodePulled reads the transaction list, tolerating the "[[]]" sentinel and
// the nested array Daraja wraps results in.
func decodePulled(raw json.RawMessage) []PulledTransaction {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" || trimmed == "null" || strings.Contains(trimmed, "[[]]") {
		return nil
	}

	var nested [][]pulledTransactionWire
	if err := json.Unmarshal(raw, &nested); err == nil {
		var out []PulledTransaction
		for _, group := range nested {
			out = append(out, convertPulled(group)...)
		}
		return out
	}

	var flat []pulledTransactionWire
	if err := json.Unmarshal(raw, &flat); err == nil {
		return convertPulled(flat)
	}
	return nil
}

func convertPulled(wires []pulledTransactionWire) []PulledTransaction {
	out := make([]PulledTransaction, 0, len(wires))
	for _, wire := range wires {
		transaction := PulledTransaction{
			TransactionID:   wire.TransactionID,
			MSISDN:          anyToString(wire.MSISDN),
			TransactionType: wire.TransactionType,
			BillReference:   strings.TrimSpace(wire.BillReference),
			Organization:    wire.Organization,
		}
		if minor, err := parseMinor(anyToString(wire.Amount)); err == nil {
			transaction.AmountMinor = minor
		}
		if parsed, ok := parsePullTime(wire.TrxDate); ok {
			transaction.CompletedAt = parsed
		}
		out = append(out, transaction)
	}
	return out
}

func parsePullTime(value string) (time.Time, bool) {
	trimmed := strings.TrimSpace(value)
	for _, layout := range []string{time.RFC3339, pullTimeLayout, "2006-01-02T15:04:05"} {
		if parsed, err := time.Parse(layout, trimmed); err == nil {
			return parsed, true
		}
	}
	return ParseTimestamp(trimmed)
}

// anyToString renders a JSON scalar that Daraja sends as either a string or a
// number — msisdn and amount are both inconsistent.
func anyToString(value any) string {
	switch typed := value.(type) {
	case nil:
		return ""
	case string:
		return typed
	case float64:
		return strconv.FormatFloat(typed, 'f', -1, 64)
	case json.Number:
		return typed.String()
	}
	return ""
}

// PullAll walks every page in a window.
//
// The offset must advance or the walk would loop forever on a Daraja that
// ignores it, so a non-advancing page fails loudly rather than hanging.
func (c *Client) PullAll(ctx context.Context, from, to time.Time, shortcode uint) ([]PulledTransaction, error) {
	const maxPages = 500

	var all []PulledTransaction
	offset := 0
	for page := 0; page < maxPages; page++ {
		batch, err := c.PullTransactions(ctx, from, to, offset, shortcode)
		if err != nil {
			return all, err
		}
		if len(batch) == 0 {
			return all, nil
		}
		all = append(all, batch...)
		offset += len(batch)
	}
	return all, mpesaErr("pull_all").
		Code(pkgErrors.CodeHTTPError).
		With("pages", maxPages).
		Errorf("pull pagination did not terminate")
}
