package stellaranchor

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/samber/oops"

	"github.com/stellar/go-stellar-sdk/amount"

	pkgErrors "github.com/Shamba-Records-Limited/microvault/pkg/errors"
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

// DepositRequest is the body of a SEP-24 /transactions/deposit/interactive
// call. Customer fields are SEP-9 — see Customer.
type DepositRequest struct {
	AssetCode string   // "USDC"
	Amount    string   // decimal USDC, e.g. "50.00" — required for custodial wallets
	Lang      string   // ISO-639-1, defaults to "en"
	Account   string   // destination wallet G... address the anchor credits
	Customer  Customer // optional SEP-9 prefill

	// Memo asks the anchor to attach a memo to the Stellar payment it makes to
	// Account. SEP-24 offers it precisely so a client can match inbound
	// payments to its own records, and it is independent of the SEP-10 memo:
	// that one identifies the borrower, this one can identify the loan.
	//
	// Optional in the specification, so an anchor may ignore it. MoneyGram
	// does not: the sandbox echoes it on deposit_memo from the first poll, at
	// status incomplete, well before the borrower finishes the webview.
	//
	// Every borrower's deposit lands on the same Account, so without this the
	// payments are distinguishable on-chain only by amount and timing.
	Memo     string
	MemoType string // "text", "id" or "hash"; defaults to "text" when Memo is set
}

// DepositResponse is what MoneyGram returns from /transactions/deposit/interactive.
//
// Same shape as WithdrawResponse, but the user's obligation is reversed: they
// must complete the webview to pick an agent and commit before any cash can be
// paid in, and nothing on our side compels them to.
type DepositResponse struct {
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

	// Deposit-side fields. DepositMemo is the memo the anchor will use to
	// transfer the asset to To, and MoneyGram populates it with whatever
	// DepositRequest.Memo asked for — verified against the sandbox, which
	// echoed "LR-1A03C985D8A-61ce" at status incomplete.
	//
	// It is a statement of intent until a payment actually exists: the field
	// says what the anchor means to attach, not what landed on the ledger.
	//
	// This is never ChildAccountMemo. That one scopes the SEP-10 session and
	// identifies the borrower; this one is per-transaction and is what makes
	// concurrent deposits to a shared account distinguishable on-chain.
	To              string `json:"to,omitempty"` // account the anchor credits
	DepositMemo     string `json:"deposit_memo,omitempty"`
	DepositMemoType string `json:"deposit_memo_type,omitempty"`

	// UserActionRequiredBy is MoneyGram's own deadline for the borrower to act.
	// It is theirs, not ours: a deposit lapses on this timestamp whatever our
	// configured window says, so it is the figure to schedule and quote against.
	UserActionRequiredBy string `json:"user_action_required_by,omitempty"`

	// FeeDetails is what the borrower pays on top of the amount. SEP-24 carries
	// it separately from amount_in, so a quote that ignores it understates what
	// they hand over at the counter.
	FeeDetails *FeeDetails `json:"fee_details,omitempty"`

	// Refunded is the deprecated SEP-24 boolean, kept because some anchors
	// still emit it. Refunds is the authoritative field.
	Refunded bool     `json:"refunded,omitempty"`
	Refunds  *Refunds `json:"refunds,omitempty"`
}

// FeeDetails is the SEP-24 breakdown of what an anchor charges.
type FeeDetails struct {
	Total   string      `json:"total"`
	Asset   string      `json:"asset"`
	Details []FeeDetail `json:"details,omitempty"`
}

// FeeDetail is one named line of a fee breakdown.
type FeeDetail struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Amount      string `json:"amount"`
}

// Refunds describes money returned to the user for a transaction. For a
// withdrawal this is the anchor sending our USDC back on Stellar.
type Refunds struct {
	AmountRefunded string          `json:"amount_refunded"`
	AmountFee      string          `json:"amount_fee"`
	Payments       []RefundPayment `json:"payments"`
}

// RefundPayment is one individual refund payment within a Refunds object.
type RefundPayment struct {
	// ID is the Stellar transaction hash when IDType is "stellar", or an
	// anchor-internal identifier when it is "external".
	ID     string `json:"id"`
	IDType string `json:"id_type"`
	Amount string `json:"amount"`
	Fee    string `json:"fee"`
}

// RefundIDTypeStellar marks a refund payment whose ID is a Stellar
// transaction hash and can therefore be verified on-ledger.
const RefundIDTypeStellar = "stellar"

// StellarPayments returns the refund payments settled on Stellar, whose IDs
// are transaction hashes.
//
// An empty IDType counts as Stellar: the field is required by SEP-24 but
// anchors omit it in practice, and for a withdrawal refund the funds can only
// come back over Stellar.
func (r *Refunds) StellarPayments() []RefundPayment {
	if r == nil {
		return nil
	}
	out := make([]RefundPayment, 0, len(r.Payments))
	for _, p := range r.Payments {
		if p.ID == "" {
			continue
		}
		if p.IDType == "" || p.IDType == RefundIDTypeStellar {
			out = append(out, p)
		}
	}
	return out
}

// NetRefundedStroops is the anchor's stated refund total in stroops, taken as
// AmountRefunded less AmountFee.
func (r *Refunds) NetRefundedStroops() (int64, error) {
	if r == nil {
		return 0, nil
	}
	gross, err := parseAmountStroops(r.AmountRefunded)
	if err != nil {
		return 0, anchorErr("refund").With("field", "amount_refunded").
			Code(pkgErrors.CodeDecodeFailed).Wrapf(err, "refund amount is not a decimal")
	}
	fee, err := parseAmountStroops(r.AmountFee)
	if err != nil {
		return 0, anchorErr("refund").With("field", "amount_fee").
			Code(pkgErrors.CodeDecodeFailed).Wrapf(err, "refund fee is not a decimal")
	}
	net := gross - fee
	if net < 0 {
		// A fee larger than the refund is arithmetically impossible, so the
		// anchor's own figures disagree and neither can be settled against.
		return 0, anchorErr("refund").
			With("amount_fee", r.AmountFee).
			With("amount_refunded", r.AmountRefunded).
			Code(pkgErrors.CodeIncompleteResponse).
			Errorf("anchor reported a refund fee larger than the refund")
	}
	return net, nil
}

// parseAmountStroops converts a SEP-24 decimal amount string to stroops.
// An empty string is zero, which is how anchors represent "no fee".
func parseAmountStroops(s string) (int64, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, nil
	}
	v, err := amount.ParseInt64(s)
	if err != nil {
		return 0, anchorErr("amount").With("value", s).
			Code(pkgErrors.CodeDecodeFailed).Wrapf(ErrInvalidAmount, "amount is not a valid decimal")
	}
	return v, nil
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
		return nil, anchorErr("config").With("setting", "TransferServerURL").
			Code(pkgErrors.CodeMissingDependency).Wrapf(ErrInvalidConfig, "required anchor setting is missing")
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
	res, err := c.initiateInteractive(ctx, jwt, "withdraw", interactiveRequest{
		AssetCode: req.AssetCode,
		Amount:    req.Amount,
		Lang:      req.Lang,
		Account:   req.Account,
		Customer:  req.Customer,
	})
	if err != nil {
		return nil, err
	}
	return &WithdrawResponse{Type: res.Type, URL: res.URL, ID: res.ID}, nil
}

// InitiateDeposit calls POST /transactions/deposit/interactive with the given
// JWT (from AuthClient/JWTCache), the USDC amount, and any prefilled SEP-9
// customer fields. Returns the interactive URL and MG transaction ID.
func (c *AnchorClient) InitiateDeposit(ctx context.Context, jwt string, req DepositRequest) (*DepositResponse, error) {
	memoType := req.MemoType
	if req.Memo != "" && memoType == "" {
		memoType = "text"
	}

	res, err := c.initiateInteractive(ctx, jwt, "deposit", interactiveRequest{
		AssetCode: req.AssetCode,
		Amount:    req.Amount,
		Lang:      req.Lang,
		Account:   req.Account,
		Memo:      req.Memo,
		MemoType:  memoType,
		Customer:  req.Customer,
	})
	if err != nil {
		return nil, err
	}
	return &DepositResponse{Type: res.Type, URL: res.URL, ID: res.ID}, nil
}

// errDomain is the oops domain for the SEP-24 anchor client.
const errDomain = pkgErrors.DomainStellarAnchor

// anchorErr starts an error builder scoped to one SEP-24 direction.
func anchorErr(kind string) oops.OopsErrorBuilder {
	return oops.In(errDomain).Tags("sep24", kind).With("kind", kind)
}

// interactiveRequest is the common field set of the deposit and withdraw
// interactive endpoints, which SEP-24 defines identically.
type interactiveRequest struct {
	AssetCode string
	Amount    string
	Lang      string
	Account   string
	Memo      string
	MemoType  string
	Customer  Customer
}

type interactiveResponse struct {
	Type string `json:"type"`
	URL  string `json:"url"`
	ID   string `json:"id"`
}

// initiateInteractive posts to /transactions/{kind}/interactive. kind is
// "deposit" or "withdraw".
func (c *AnchorClient) initiateInteractive(ctx context.Context, jwt, kind string, req interactiveRequest) (*interactiveResponse, error) {
	errb := anchorErr(kind)

	if jwt == "" {
		return nil, errb.Code(pkgErrors.CodeMissingJWT).Wrapf(ErrInvalidConfig, "JWT is required")
	}
	if req.AssetCode == "" {
		req.AssetCode = "USDC"
	}
	if req.Lang == "" {
		req.Lang = "en"
	}
	if req.Amount == "" {
		return nil, errb.Code(pkgErrors.CodeMissingAmount).Wrapf(ErrInvalidConfig, "amount is required: custodial wallets must specify it")
	}
	if req.Account == "" {
		return nil, errb.Code(pkgErrors.CodeMissingAccount).Wrapf(ErrInvalidConfig, "account is required: the funds wallet address")
	}

	body, err := buildInteractiveBody(kind, req)
	if err != nil {
		return nil, err
	}

	endpoint := strings.TrimRight(c.cfg.TransferServerURL, "/") + "/transactions/" + kind + "/interactive"
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
		return nil, errb.Code(pkgErrors.CodeTransportFailed).Wrapf(err, "interactive request could not be sent")
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 64*1024))

	if resp.StatusCode == http.StatusUnauthorized {
		return nil, errb.Code(pkgErrors.CodeUnauthorized).Wrapf(ErrUnauthorized, "anchor rejected the JWT")
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, errb.
			Code(pkgErrors.CodeHTTPError).
			With(pkgErrors.AttrStatusCode, resp.StatusCode).
			With("body", truncate(string(respBody), 300)).
			Errorf("anchor returned a non-2xx status")
	}

	var parsed interactiveResponse
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return nil, errb.Code(pkgErrors.CodeDecodeFailed).Wrapf(err, "could not decode the interactive response")
	}
	if parsed.URL == "" || parsed.ID == "" {
		return nil, errb.
			Code(pkgErrors.CodeIncompleteResponse).
			With("body", truncate(string(respBody), 200)).
			Errorf("interactive response is missing url or id")
	}
	return &parsed, nil
}

// GetTransaction calls GET /transaction?id={txID} with the given JWT.
func (c *AnchorClient) GetTransaction(ctx context.Context, jwt, txID string) (*Transaction, error) {
	errb := anchorErr("transaction").With("tx_id", txID)

	if jwt == "" {
		return nil, errb.Code(pkgErrors.CodeMissingJWT).Wrapf(ErrInvalidConfig, "JWT is required")
	}
	if txID == "" {
		return nil, errb.Code(pkgErrors.CodeMissingAccount).Wrapf(ErrInvalidConfig, "transaction id is required")
	}

	q := url.Values{}
	q.Set("id", txID)
	endpoint := strings.TrimRight(c.cfg.TransferServerURL, "/") + "/transaction?" + q.Encode()

	resp, err := doHTTPWithRetry(ctx, c.httpClient, func() (*http.Request, error) {
		httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, http.NoBody)
		if err != nil {
			return nil, err
		}
		httpReq.Header.Set("Accept", "application/json")
		httpReq.Header.Set("Authorization", "Bearer "+jwt)
		return httpReq, nil
	})
	if err != nil {
		return nil, errb.Code(pkgErrors.CodeTransportFailed).Wrapf(err, "transaction request did not complete")
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 64*1024))

	if resp.StatusCode == http.StatusUnauthorized {
		return nil, errb.Code(pkgErrors.CodeUnauthorized).Wrapf(ErrUnauthorized, "anchor rejected the JWT")
	}
	if resp.StatusCode == http.StatusNotFound {
		return nil, errb.Code(pkgErrors.CodeNotFound).Errorf("anchor has no such transaction")
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, errb.
			With(pkgErrors.AttrStatusCode, resp.StatusCode).
			With("body", truncate(string(respBody), 300)).
			Code(pkgErrors.CodeHTTPError).
			Errorf("anchor returned a non-2xx for the transaction")
	}

	var envelope struct {
		Transaction Transaction `json:"transaction"`
	}
	if err := json.Unmarshal(respBody, &envelope); err != nil {
		return nil, errb.Code(pkgErrors.CodeDecodeFailed).Wrapf(err, "could not decode the transaction response")
	}
	if envelope.Transaction.ID == "" {
		return nil, errb.With("body", truncate(string(respBody), 200)).
			Code(pkgErrors.CodeIncompleteResponse).Errorf("transaction response has no id")
	}

	// Deposits are new and MoneyGram's field population is not documented —
	// testnet showed external_transaction_id empty at
	// pending_user_transfer_start, which is where the reference was expected.
	// Log the raw body so which field carries the borrower's code is an
	// observation rather than a guess. Drop this once the shape is known.
	if envelope.Transaction.Kind == "deposit" {
		c.logDepositPayload(&envelope.Transaction, respBody)
	}

	// Refund payloads are the least-exercised part of the protocol and anchors
	// vary in what they populate, so log the raw body verbatim the first time
	// we see one. This is what confirms an anchor's actual refunds shape
	// against the spec.
	if envelope.Transaction.Status == StatusRefunded {
		c.logger.Info("SEP-24 refunded transaction raw payload",
			"tx_id", envelope.Transaction.ID,
			"body", truncate(string(respBody), 2000),
		)
	}
	return &envelope.Transaction, nil
}

// logDepositPayload records what an anchor actually returns for a deposit.
func (c *AnchorClient) logDepositPayload(tx *Transaction, body []byte) {
	attrs := []any{
		"tx_id", tx.ID,
		"status", tx.Status,
		"external_transaction_id", tx.ExternalTransactionID,
		"deposit_memo", tx.DepositMemo,
		"more_info_url", tx.MoreInfoURL,
		"amount_in", tx.AmountIn,
		"amount_in_asset", tx.AmountInAsset,
		"amount_out", tx.AmountOut,
		"amount_out_asset", tx.AmountOutAsset,
		"user_action_required_by", tx.UserActionRequiredBy,
	}
	if tx.FeeDetails != nil {
		attrs = append(attrs, "fee_total", tx.FeeDetails.Total, "fee_asset", tx.FeeDetails.Asset)
	}

	// json.RawMessage marshals inline, so the handler emits real nested JSON
	// rather than a quoted blob.
	if json.Valid(body) {
		attrs = append(attrs, "payload", json.RawMessage(body))
	} else {
		attrs = append(attrs, "payload_raw", truncate(string(body), 2000))
	}

	c.logger.Info("SEP-24 deposit transaction", attrs...)
}

// buildInteractiveBody marshals the request to JSON with SEP-24 fields plus the
// flattened SEP-9 customer fields. SEP-9 keys are top-level (not nested) per
// the protocol; we manually assemble the map so the Customer struct can stay
// JSON-tagged independently for transport.
func buildInteractiveBody(kind string, req interactiveRequest) ([]byte, error) {
	m := map[string]string{
		"asset_code": req.AssetCode,
		"amount":     req.Amount,
		"lang":       req.Lang,
		"account":    req.Account,
	}
	addIfSet(m, "memo", req.Memo)
	addIfSet(m, "memo_type", req.MemoType)
	addIfSet(m, "first_name", req.Customer.FirstName)
	addIfSet(m, "last_name", req.Customer.LastName)
	addIfSet(m, "mobile_number", req.Customer.MobileNumber)
	addIfSet(m, "birth_date", req.Customer.BirthDate)
	addIfSet(m, "address", req.Customer.Address)
	addIfSet(m, "city", req.Customer.City)
	addIfSet(m, "postal_code", req.Customer.PostalCode)
	addIfSet(m, "address_country_code", req.Customer.AddressCountryCode)

	body, err := json.Marshal(m)
	if err != nil {
		return nil, anchorErr(kind).Code(pkgErrors.CodeEncodeFailed).Wrapf(err, "could not encode the interactive request body")
	}
	return body, nil
}

func addIfSet(m map[string]string, k, v string) {
	if v != "" {
		m[k] = v
	}
}
