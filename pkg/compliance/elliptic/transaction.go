package elliptic

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/Shamba-Records-Limited/microvault/pkg/compliance"

	pkgErrors "github.com/Shamba-Records-Limited/microvault/pkg/errors"
)

var _ compliance.TransactionScreener = (*Client)(nil)

// holistic is the asset and blockchain value for account-based transaction
// screening on POST /v2/analyses/synchronous.
const holistic = "holistic"

type transactionScreeningRequest struct {
	Subject           transactionSubject `json:"subject"`
	Type              string             `json:"type"`
	CustomerReference string             `json:"customer_reference"`
}

type transactionSubject struct {
	Asset         string `json:"asset"`
	Blockchain    string `json:"blockchain"`
	Type          string `json:"type"`
	Hash          string `json:"hash"`
	OutputType    string `json:"output_type"`
	OutputAddress string `json:"output_address"`
}

type transactionScreeningResponse struct {
	ID               string           `json:"id"`
	RiskScore        *float64         `json:"risk_score"`
	ProcessStatus    string           `json:"process_status"`
	ScreeningID      string           `json:"screening_id"`
	EvaluationDetail []ruleEvaluation `json:"evaluation_detail,omitempty"`
	EntityDetails    *txEntityDetails `json:"entity_details,omitempty"`
	Error            *errorDetail     `json:"error,omitempty"`
	Predictive       bool             `json:"predictive"`
}

// txEntityDetails lists sanctioned entities keyed by entity id; only
// sanctioned entities appear.
type txEntityDetails struct {
	Sanctions map[string]json.RawMessage `json:"sanctions,omitempty"`
}

// ScreenTransaction screens where a deposit's funds came from: a
// source_of_funds analysis of req.Hash at its output req.Recipient (the
// address that received the deposit).
func (c *Client) ScreenTransaction(ctx context.Context, req compliance.TxScreenRequest) (*compliance.Screening, error) {
	const op = "screen_transaction"
	if req.Hash == "" || req.Recipient == "" {
		return nil, ellipticErr(op).Code(pkgErrors.CodeBuildFailed).Errorf("transaction hash and recipient are required")
	}
	if req.CustomerReference == "" {
		return nil, ellipticErr(op).Code(pkgErrors.CodeBuildFailed).Errorf("elliptic requires a customer reference for transaction screening")
	}
	resp, err := call[transactionScreeningResponse](ctx, c, op, "POST", "/analyses/synchronous", transactionScreeningRequest{
		Subject: transactionSubject{
			Asset: holistic, Blockchain: holistic, Type: "transaction",
			Hash: req.Hash, OutputType: "address", OutputAddress: req.Recipient,
		},
		Type:              "source_of_funds",
		CustomerReference: req.CustomerReference,
	})
	if err != nil {
		if errors.Is(err, ErrNotInBlockchain) {
			return &compliance.Screening{
				Provider: Provider, Address: req.Address, TxHash: req.Hash,
				Verdict: compliance.VerdictPending, ScreenedAt: time.Now(), Raw: json.RawMessage("{}"),
			}, nil
		}
		return nil, err
	}
	raw, err := json.Marshal(resp)
	if err != nil {
		return nil, ellipticErr(op).Code(pkgErrors.CodeEncodeFailed).Wrapf(err, "could not preserve the raw response")
	}
	sanctioned := resp.EntityDetails != nil && len(resp.EntityDetails.Sanctions) > 0
	rules := make([]compliance.RuleHit, 0, len(resp.EvaluationDetail))
	for _, r := range resp.EvaluationDetail {
		rules = append(rules, compliance.RuleHit{RuleName: r.RuleName, RuleType: r.RuleType})
	}
	return &compliance.Screening{
		Provider:    Provider,
		AnalysisID:  resp.ID,
		ScreeningID: resp.ScreeningID,
		Address:     req.Address,
		TxHash:      req.Hash,
		Verdict:     transactionVerdict(resp, sanctioned, c.thresholds),
		RiskScore:   resp.RiskScore,
		Sanctioned:  sanctioned,
		Rules:       rules,
		ScreenedAt:  time.Now(),
		Raw:         raw,
	}, nil
}

// transactionVerdict reuses the wallet policy; a predictive analysis (too
// close to the chain tip for a full picture) is pending, not final.
func transactionVerdict(resp transactionScreeningResponse, sanctioned bool, th Thresholds) compliance.Verdict {
	if sanctioned {
		return compliance.VerdictRejected
	}
	if resp.Predictive {
		return compliance.VerdictPending
	}
	return deriveVerdict(walletScreeningResponse{
		RiskScore: resp.RiskScore, ProcessStatus: resp.ProcessStatus, Error: resp.Error,
	}, th)
}
