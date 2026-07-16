package stellaranchor

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Status is the SEP-24 transaction status.
type Status string

// SEP-24 transaction states. The full set documented by [SEP-0024 protocol](https://github.com/stellar/stellar-protocol/blob/master/ecosystem/sep-0024.md)
// in the "Shared fields for both deposits and withdrawals" section.
const (
	StatusIncomplete                  Status = "incomplete"
	StatusPendingUserTransferStart    Status = "pending_user_transfer_start"
	StatusPendingUserTransferComplete Status = "pending_user_transfer_complete"
	StatusPendingExternal             Status = "pending_external"
	StatusPendingAnchor               Status = "pending_anchor"
	StatusOnHold                      Status = "on_hold"
	StatusPendingStellar              Status = "pending_stellar"
	StatusPendingTrust                Status = "pending_trust"
	StatusPendingUser                 Status = "pending_user"
	StatusCompleted                   Status = "completed"
	StatusRefunded                    Status = "refunded"
	StatusExpired                     Status = "expired"
	StatusNoMarket                    Status = "no_market"
	StatusTooSmall                    Status = "too_small"
	StatusTooLarge                    Status = "too_large"
	StatusError                       Status = "error"
)

// Terminal returns true when no further status transitions are possible —
// pollers should stop polling on terminal statuses.
func (s Status) Terminal() bool {
	switch s {
	case StatusCompleted, StatusRefunded, StatusExpired,
		StatusNoMarket, StatusTooSmall, StatusTooLarge, StatusError:
		return true
	}
	return false
}

// WithdrawRequest is the body of a SEP-24 /transactions/withdraw/interactive
// call. Customer fields are SEP-9 — see Customer.
type WithdrawRequest struct {
	AssetCode string   // "USDC"
	Amount    string   // decimal USD, e.g. "50.00" — required for custodial wallets
	Lang      string   // ISO-639-1, defaults to "en"
	Account   string   // funds wallet G... address — the withdrawal payment source
	Customer  Customer // optional SEP-9 prefill
}

// WithdrawResponse is what MoneyGram returns from /transactions/withdraw/interactive.
type WithdrawResponse struct {
	Type string `json:"type"` // typically "interactive_customer_info_needed"
	URL  string `json:"url"`  // interactive webview URL — SMS this to the user
	ID   string `json:"id"`   // MoneyGram's transaction ID — persist and poll
}

// Transaction is the SEP-24 transaction object returned from /transaction.
// Field set is intentionally a superset of what we need to drive the off-
// ramp state machine — some fields are populated only at certain statuses.
type Transaction struct {
	ID                    string `json:"id"`
	Kind                  string `json:"kind"` // "deposit" | "withdrawal"
	Status                Status `json:"status"`
	StatusEta             int64  `json:"status_eta,omitempty"`
	AmountIn              string `json:"amount_in,omitempty"`
	AmountInAsset         string `json:"amount_in_asset,omitempty"`
	AmountOut             string `json:"amount_out,omitempty"`
	AmountOutAsset        string `json:"amount_out_asset,omitempty"`
	AmountFee             string `json:"amount_fee,omitempty"`
	AmountFeeAsset        string `json:"amount_fee_asset,omitempty"`
	StartedAt             string `json:"started_at,omitempty"`
	CompletedAt           string `json:"completed_at,omitempty"`
	StellarTransactionID  string `json:"stellar_transaction_id,omitempty"`
	ExternalTransactionID string `json:"external_transaction_id,omitempty"` // cash-pickup reference
	WithdrawAnchorAccount string `json:"withdraw_anchor_account,omitempty"`
	WithdrawMemo          string `json:"withdraw_memo,omitempty"`
	WithdrawMemoType      string `json:"withdraw_memo_type,omitempty"` // typically "id"
	MoreInfoURL           string `json:"more_info_url,omitempty"`
	Message               string `json:"message,omitempty"`
	Refunded              bool   `json:"refunded,omitempty"`
}

// AnchorConfig configures the SEP-24 client.
type AnchorConfig struct {
	TransferServerURL string // from TOML's TRANSFER_SERVER_SEP0024
}

// AnchorClient implements the subset of SEP-24 needed for cash-pickup off-ramp:
// /transactions/withdraw/interactive and /transaction.
type AnchorClient struct {
	cfg        AnchorConfig
	httpClient *http.Client
	logger     *slog.Logger
}

// NewAnchorClient validates configuration. httpClient may be nil; logger may be nil.
func NewAnchorClient(cfg AnchorConfig, httpClient *http.Client, logger *slog.Logger) (*AnchorClient, error) {
	if cfg.TransferServerURL == "" {
		return nil, fmt.Errorf("stellaranchor: %w: TransferServerURL required", ErrInvalidConfig)
	}
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 15 * time.Second}
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &AnchorClient{cfg: cfg, httpClient: httpClient, logger: logger}, nil
}

// InitiateWithdrawal calls POST /transactions/withdraw/interactive with the
// given JWT (from AuthClient/JWTCache), the USDC amount, and any prefilled
// SEP-9 customer fields. Returns the interactive URL and MG transaction ID.
func (c *AnchorClient) InitiateWithdrawal(ctx context.Context, jwt string, req WithdrawRequest) (*WithdrawResponse, error) {
	if jwt == "" {
		return nil, fmt.Errorf("stellaranchor: %w: JWT required", ErrInvalidConfig)
	}
	if req.AssetCode == "" {
		req.AssetCode = "USDC"
	}
	if req.Lang == "" {
		req.Lang = "en"
	}
	if req.Amount == "" {
		return nil, fmt.Errorf("stellaranchor: %w: Amount required (custodial wallets must specify amount)", ErrInvalidConfig)
	}
	if req.Account == "" {
		return nil, fmt.Errorf("stellaranchor: %w: Account required (funds wallet address)", ErrInvalidConfig)
	}

	body, err := buildWithdrawBody(req)
	if err != nil {
		return nil, err
	}

	endpoint := strings.TrimRight(c.cfg.TransferServerURL, "/") + "/transactions/withdraw/interactive"
	resp, err := doHTTPWithRetry(ctx, c.httpClient, func() (*http.Request, error) {
		httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(string(body)))
		if err != nil {
			return nil, err
		}
		httpReq.Header.Set("Content-Type", "application/json")
		httpReq.Header.Set("Accept", "application/json")
		httpReq.Header.Set("Authorization", "Bearer "+jwt)
		return httpReq, nil
	})
	if err != nil {
		return nil, fmt.Errorf("stellaranchor: withdraw request: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 64*1024))

	if resp.StatusCode == http.StatusUnauthorized {
		return nil, fmt.Errorf("stellaranchor: %w: SEP-24 withdraw HTTP 401", ErrUnauthorized)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("stellaranchor: SEP-24 withdraw HTTP %d: %s",
			resp.StatusCode, truncate(string(respBody), 300))
	}

	var parsed WithdrawResponse
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return nil, fmt.Errorf("stellaranchor: decode withdraw response: %w", err)
	}
	if parsed.URL == "" || parsed.ID == "" {
		return nil, fmt.Errorf("stellaranchor: withdraw response missing url or id: %s", truncate(string(respBody), 200))
	}
	return &parsed, nil
}

// GetTransaction calls GET /transaction?id={txID} with the given JWT.
func (c *AnchorClient) GetTransaction(ctx context.Context, jwt, txID string) (*Transaction, error) {
	if jwt == "" {
		return nil, fmt.Errorf("stellaranchor: %w: JWT required", ErrInvalidConfig)
	}
	if txID == "" {
		return nil, fmt.Errorf("stellaranchor: %w: txID required", ErrInvalidConfig)
	}

	q := url.Values{}
	q.Set("id", txID)
	endpoint := strings.TrimRight(c.cfg.TransferServerURL, "/") + "/transaction?" + q.Encode()

	resp, err := doHTTPWithRetry(ctx, c.httpClient, func() (*http.Request, error) {
		httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
		if err != nil {
			return nil, err
		}
		httpReq.Header.Set("Accept", "application/json")
		httpReq.Header.Set("Authorization", "Bearer "+jwt)
		return httpReq, nil
	})
	if err != nil {
		return nil, fmt.Errorf("stellaranchor: transaction request: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 64*1024))

	if resp.StatusCode == http.StatusUnauthorized {
		return nil, fmt.Errorf("stellaranchor: %w: SEP-24 transaction HTTP 401", ErrUnauthorized)
	}
	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("stellaranchor: SEP-24 transaction not found: %s", txID)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("stellaranchor: SEP-24 transaction HTTP %d: %s",
			resp.StatusCode, truncate(string(respBody), 300))
	}

	var envelope struct {
		Transaction Transaction `json:"transaction"`
	}
	if err := json.Unmarshal(respBody, &envelope); err != nil {
		return nil, fmt.Errorf("stellaranchor: decode transaction response: %w", err)
	}
	if envelope.Transaction.ID == "" {
		return nil, fmt.Errorf("stellaranchor: transaction response missing id: %s", truncate(string(respBody), 200))
	}
	return &envelope.Transaction, nil
}

// buildWithdrawBody marshals the request to JSON with SEP-24 fields plus the
// flattened SEP-9 customer fields. SEP-9 keys are top-level (not nested) per
// the protocol; we manually assemble the map so the Customer struct can stay
// JSON-tagged independently for transport.
func buildWithdrawBody(req WithdrawRequest) ([]byte, error) {
	m := map[string]string{
		"asset_code": req.AssetCode,
		"amount":     req.Amount,
		"lang":       req.Lang,
		"account":    req.Account,
	}
	addIfSet(m, "first_name", req.Customer.FirstName)
	addIfSet(m, "last_name", req.Customer.LastName)
	addIfSet(m, "mobile_number", req.Customer.MobileNumber)
	addIfSet(m, "birth_date", req.Customer.BirthDate)
	addIfSet(m, "address", req.Customer.Address)
	addIfSet(m, "city", req.Customer.City)
	addIfSet(m, "postal_code", req.Customer.PostalCode)
	addIfSet(m, "address_country_code", req.Customer.AddressCountryCode)
	addIfSet(m, "state_or_province", req.Customer.StateOrProvince)

	body, err := json.Marshal(m)
	if err != nil {
		return nil, fmt.Errorf("stellaranchor: marshal withdraw body: %w", err)
	}
	return body, nil
}

func addIfSet(m map[string]string, k, v string) {
	if v != "" {
		m[k] = v
	}
}
