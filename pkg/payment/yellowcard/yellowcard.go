package yellowcard

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/samber/lo"
	"github.com/samber/oops"

	pkgErrors "github.com/Shamba-Records-Limited/microvault/pkg/errors"
)

// yellowcardTransport is a custom http.RoundTripper that signs requests
// using YellowCard's YcHmacV1 authentication scheme.
type yellowcardTransport struct {
	publicKey string
	secretKey string
	base      http.RoundTripper
}

// RoundTrip signs the request and passes it to the base transport.
func (t *yellowcardTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	date := time.Now().UTC().Format("2006-01-02T15:04:05.000Z")

	// Sign path only — query string is excluded per YellowCard docs.
	path := req.URL.Path

	h := hmac.New(sha256.New, []byte(t.secretKey))
	h.Write([]byte(date))
	h.Write([]byte(path))
	h.Write([]byte(req.Method))

	if req.Body != nil && req.Body != http.NoBody {
		bodyBytes, err := io.ReadAll(req.Body)
		if err != nil {
			return nil, ycErr("sign_request").Code(pkgErrors.CodeTransportFailed).
				Wrapf(err, "could not read the request body for signing")
		}
		req.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))

		bodyHash := sha256.Sum256(bodyBytes)
		bodyBase64 := base64.StdEncoding.EncodeToString(bodyHash[:])
		h.Write([]byte(bodyBase64))
	}

	signature := base64.StdEncoding.EncodeToString(h.Sum(nil))

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-YC-Timestamp", date)
	req.Header.Set("Authorization", fmt.Sprintf("YcHmacV1 %s:%s", t.publicKey, signature))

	return t.base.RoundTrip(req)
}

// YellowcardAdapter is the low-level HTTP client for the YellowCard API.
// The OffRampService-side wrapper lives in pkg/mobile/ussd/adapters.
type YellowcardAdapter struct {
	httpClient *http.Client
	baseURL    string
}

// ycErr starts an error builder for YellowCard calls. The provider is an
// attribute rather than part of the domain so YellowCard and Fonbnk failures
// group together as off-ramp problems and can still be told apart.
func ycErr(op string) oops.OopsErrorBuilder {
	return oops.
		In(pkgErrors.DomainOffRamp).
		Tags("yellowcard").
		With(pkgErrors.AttrProvider, "yellowcard").
		With(pkgErrors.AttrOperation, op)
}

// NewYellowcardAdapter creates a new YellowCard adapter with HMAC request signing.
func NewYellowcardAdapter(publicKey, secretKey, baseURL string) *YellowcardAdapter {
	signingTransport := &yellowcardTransport{
		publicKey: publicKey,
		secretKey: secretKey,
		base:      http.DefaultTransport,
	}

	client := &http.Client{
		Transport: signingTransport,
		Timeout:   30 * time.Second,
	}

	return &YellowcardAdapter{
		httpClient: client,
		baseURL:    baseURL,
	}
}

// GetChannels retrieves available payment channels for a country.
func (y *YellowcardAdapter) GetChannels(ctx context.Context, country string) ([]Channel, error) {
	endpoint := fmt.Sprintf("%s/channels", y.baseURL)
	if country != "" {
		endpoint = fmt.Sprintf("%s?country=%s", endpoint, country)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, http.NoBody)
	if err != nil {
		return nil, ycErr("get_channels").Code(pkgErrors.CodeBuildFailed).Wrapf(err, "could not build the request")
	}

	resp, err := y.httpClient.Do(req)
	if err != nil {
		return nil, ycErr("get_channels").Code(pkgErrors.CodeTransportFailed).Wrapf(err, "request did not complete")
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, y.parseError(resp)
	}

	var channelsResp ChannelsResponse
	if err := json.NewDecoder(resp.Body).Decode(&channelsResp); err != nil {
		return nil, ycErr("get_channels").Code(pkgErrors.CodeDecodeFailed).Wrapf(err, "could not decode the response")
	}

	return channelsResp.Channels, nil
}

// GetNetworks retrieves available banks and mobile money operators for a country.
func (y *YellowcardAdapter) GetNetworks(ctx context.Context, country string) ([]Network, error) {
	endpoint := fmt.Sprintf("%s/networks", y.baseURL)
	if country != "" {
		endpoint = fmt.Sprintf("%s?country=%s", endpoint, country)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, http.NoBody)
	if err != nil {
		return nil, ycErr("get_networks").Code(pkgErrors.CodeBuildFailed).Wrapf(err, "could not build the request")
	}

	resp, err := y.httpClient.Do(req)
	if err != nil {
		return nil, ycErr("get_networks").Code(pkgErrors.CodeTransportFailed).Wrapf(err, "request did not complete")
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, y.parseError(resp)
	}

	var networksResp NetworksResponse
	if err := json.NewDecoder(resp.Body).Decode(&networksResp); err != nil {
		return nil, ycErr("get_networks").Code(pkgErrors.CodeDecodeFailed).Wrapf(err, "could not decode the response")
	}

	return networksResp.Networks, nil
}

// GetAccount retrieves account balances.
func (y *YellowcardAdapter) GetAccount(ctx context.Context) ([]Account, error) {
	endpoint := fmt.Sprintf("%s/account", y.baseURL)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, http.NoBody)
	if err != nil {
		return nil, ycErr("get_account").Code(pkgErrors.CodeBuildFailed).Wrapf(err, "could not build the request")
	}

	resp, err := y.httpClient.Do(req)
	if err != nil {
		return nil, ycErr("get_account").Code(pkgErrors.CodeTransportFailed).Wrapf(err, "request did not complete")
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, y.parseError(resp)
	}

	var accountsResp AccountsResponse
	if err := json.NewDecoder(resp.Body).Decode(&accountsResp); err != nil {
		return nil, ycErr("get_account").Code(pkgErrors.CodeDecodeFailed).Wrapf(err, "could not decode the response")
	}

	return accountsResp.Accounts, nil
}

// GetAvailableBalance returns the available USD balance.
func (y *YellowcardAdapter) GetAvailableBalance(ctx context.Context) (float64, error) {
	accounts, err := y.GetAccount(ctx)
	if err != nil {
		return 0, err
	}

	for _, acc := range accounts {
		if acc.Currency == CurrencyUSD && acc.CurrencyType == "fiat" {
			return acc.Available, nil
		}
	}

	return 0, ycErr("get_available_balance").Code(pkgErrors.CodeNotFound).
		Errorf("no USD account found on the YellowCard profile")
}

// GetRates retrieves exchange rates for a currency.
func (y *YellowcardAdapter) GetRates(ctx context.Context, currency string) ([]Rate, error) {
	endpoint := fmt.Sprintf("%s/rates", y.baseURL)
	if currency != "" {
		endpoint = fmt.Sprintf("%s?currency=%s", endpoint, currency)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, http.NoBody)
	if err != nil {
		return nil, ycErr("get_rates").Code(pkgErrors.CodeBuildFailed).Wrapf(err, "could not build the request")
	}

	resp, err := y.httpClient.Do(req)
	if err != nil {
		return nil, ycErr("get_rates").Code(pkgErrors.CodeTransportFailed).Wrapf(err, "request did not complete")
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, y.parseError(resp)
	}

	var ratesResp RatesResponse
	if err := json.NewDecoder(resp.Body).Decode(&ratesResp); err != nil {
		return nil, ycErr("get_rates").Code(pkgErrors.CodeDecodeFailed).Wrapf(err, "could not decode the response")
	}

	return ratesResp.Rates, nil
}

// SubmitPayment sends a disbursement request to YellowCard.
//
// Uses the /send endpoint (formerly /payments). YellowCard renamed Payments to
// Sends; the request/response schema is unchanged. The legacy /payments path is
// deprecated but still functional.
func (y *YellowcardAdapter) SubmitPayment(ctx context.Context, req PaymentRequest) (*PaymentResponse, error) {
	endpoint := fmt.Sprintf("%s/send", y.baseURL)

	req.ForceAccept = true

	body, err := json.Marshal(req)
	if err != nil {
		return nil, ycErr("submit_payment").Code(pkgErrors.CodeEncodeFailed).Wrapf(err, "could not encode the request")
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewBuffer(body))
	if err != nil {
		return nil, ycErr("submit_payment").Code(pkgErrors.CodeBuildFailed).Wrapf(err, "could not build the request")
	}

	resp, err := y.httpClient.Do(httpReq)
	if err != nil {
		return nil, ycErr("submit_payment").Code(pkgErrors.CodeTransportFailed).Wrapf(err, "request did not complete")
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		var apiErr APIError
		if json.Unmarshal(respBody, &apiErr) == nil && apiErr.Code != "" {
			return nil, ycErr("submit_payment").
				With("api_code", apiErr.Code).
				With("api_message", apiErr.Message).
				Code(pkgErrors.CodeHTTPError).
				Errorf("YellowCard rejected the payment")
		}
		return nil, ycErr("submit_payment").
			With(pkgErrors.AttrStatusCode, resp.StatusCode).
			With("body", string(respBody)).
			Code(pkgErrors.CodeHTTPError).
			Errorf("YellowCard returned a non-2xx for the payment")
	}

	var paymentResp PaymentResponse
	if err := json.Unmarshal(respBody, &paymentResp); err != nil {
		return nil, ycErr("submit_payment").Code(pkgErrors.CodeDecodeFailed).Wrapf(err, "could not decode the response")
	}

	return &paymentResp, nil
}

// LookupPayment retrieves payment details by ID.
//
// Uses the /send/{id} endpoint (formerly /payments/{id}). See SubmitPayment for
// the Payments→Sends rename note.
func (y *YellowcardAdapter) LookupPayment(ctx context.Context, paymentID string) (*PaymentDetails, error) {
	endpoint := fmt.Sprintf("%s/send/%s", y.baseURL, paymentID)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, http.NoBody)
	if err != nil {
		return nil, ycErr("lookup_payment").Code(pkgErrors.CodeBuildFailed).Wrapf(err, "could not build the request")
	}

	resp, err := y.httpClient.Do(req)
	if err != nil {
		return nil, ycErr("lookup_payment").Code(pkgErrors.CodeTransportFailed).Wrapf(err, "request did not complete")
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, y.parseError(resp)
	}

	var details PaymentDetails
	if err := json.NewDecoder(resp.Body).Decode(&details); err != nil {
		return nil, ycErr("lookup_payment").Code(pkgErrors.CodeDecodeFailed).Wrapf(err, "could not decode the response")
	}

	return &details, nil
}

// parseError reads a non-OK HTTP response body and returns a structured error,
// preferring the YellowCard API error format when available.
func (y *YellowcardAdapter) parseError(resp *http.Response) error {
	return parseErrorWith(ycErr("api"), resp)
}

// parseErrorWith is parseError against a caller-supplied builder, so the
// failing operation and its identifiers survive onto the error.
//
// The HTTP-error path is the common one, and parseError's own builder names
// the operation "api" — which loses which call failed exactly when that is
// what an on-call engineer needs.
func parseErrorWith(errb oops.OopsErrorBuilder, resp *http.Response) error {
	body, _ := io.ReadAll(resp.Body)

	var apiErr APIError
	if json.Unmarshal(body, &apiErr) == nil && apiErr.Code != "" {
		return errb.
			With("api_code", apiErr.Code).
			With("api_message", apiErr.Message).
			With(pkgErrors.AttrStatusCode, resp.StatusCode).
			Code(pkgErrors.CodeHTTPError).
			Errorf("YellowCard returned an API error")
	}

	return errb.
		With(pkgErrors.AttrStatusCode, resp.StatusCode).
		With("body", string(body)).
		Code(pkgErrors.CodeHTTPError).
		Errorf("YellowCard returned a non-2xx")
}

// ParseStellarWalletAddress splits a YellowCard combined wallet address into
// the Stellar address and memo components. YellowCard returns XLM addresses
// in the format "{stellar_address}_{memo}" e.g.:
//
//	"GDTY5CDJDVEI4RF5RE7HNIT26FCNA3DNXFNYNMZ6TNTLQTL34YG5NL5O_4084650351"
func ParseStellarWalletAddress(combined string) (address string, memo string, err error) {
	parts := strings.SplitN(combined, "_", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", ycErr("parse_wallet_address").
			With(pkgErrors.AttrAddress, combined).
			Code(pkgErrors.CodeInvalidAddress).
			Errorf("stellar wallet address is not in the expected address:memo form")
	}
	return parts[0], parts[1], nil
}

// FilterActiveChannels returns channels that are active and match the specified type.
func FilterActiveChannels(channels []Channel, channelType string) []Channel {
	result := make([]Channel, 0)
	for _, ch := range channels {
		if ch.Status == "active" && ch.APIStatus == "active" {
			if channelType == "" || ch.ChannelType == channelType {
				result = append(result, ch)
			}
		}
	}
	return result
}

// FilterChannelsByRampType returns channels serving one direction. A channel
// is either a withdraw channel or a deposit one, so disbursement and
// collection code must not draw from the same list.
func FilterChannelsByRampType(channels []Channel, rampType string) []Channel {
	return lo.Filter(channels, func(ch Channel, _ int) bool {
		return ch.RampType == rampType
	})
}

// FilterActiveNetworks returns networks that are active.
func FilterActiveNetworks(networks []Network) []Network {
	result := make([]Network, 0)
	for _, n := range networks {
		if n.Status == "active" {
			result = append(result, n)
		}
	}
	return result
}

// FindNetworksByChannel returns networks associated with a specific channel ID.
func FindNetworksByChannel(networks []Network, channelID string) []Network {
	result := make([]Network, 0)
	for _, n := range networks {
		if slices.Contains(n.ChannelIDs, channelID) {
			result = append(result, n)
		}
	}
	return result
}
