package elliptic

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	pkgErrors "github.com/Shamba-Records-Limited/microvault/pkg/errors"

	"github.com/Shamba-Records-Limited/microvault/pkg/compliance"
)

// stellarAsset and stellarBlockchain are the literal strings Elliptic's
// coverage table confirmed (source design doc §7, 2026-09-10) — the vault
// holds USDC issued on Stellar, never native XLM, so this package screens
// exactly one asset/blockchain pair.
const (
	stellarAsset      = "USDC"
	stellarBlockchain = "stellar"
)

var _ compliance.Screener = (*Client)(nil)

// ScreenAddress screens req.Address via POST /v2/wallet/synchronous and
// applies the source design doc §10 verdict policy, using the thresholds
// set in Config (zero-valued Thresholds routes every scored, unsanctioned
// analysis to VerdictReview — see deriveVerdict's doc comment).
func (c *Client) ScreenAddress(ctx context.Context, req compliance.ScreenRequest) (*compliance.Screening, error) {
	const op = "screen_address"

	if req.Address == "" {
		return nil, ellipticErr(op).Code(pkgErrors.CodeMissingAccount).
			Errorf("address is empty")
	}

	body := walletScreeningRequest{
		Subject: walletSubject{
			Asset:      stellarAsset,
			Blockchain: stellarBlockchain,
			Type:       "address",
			Hash:       req.Address,
		},
		Type:              "wallet_exposure",
		CustomerReference: req.CustomerReference,
	}

	raw, err := json.Marshal(body)
	if err != nil {
		return nil, ellipticErr(op).Code(pkgErrors.CodeEncodeFailed).
			Wrapf(err, "could not encode the screening request")
	}

	resp, err := call[walletScreeningResponse](ctx, c, op, "POST", "/wallet/synchronous", body)
	if err != nil {
		if errors.Is(err, ErrNotInBlockchain) {
			return &compliance.Screening{
				Address:    req.Address,
				Verdict:    compliance.VerdictUnscreenable,
				ScreenedAt: time.Now(),
				Raw:        raw,
			}, nil
		}
		return nil, err
	}

	rawResp, err := json.Marshal(resp)
	if err != nil {
		// The response already decoded successfully; re-marshalling it for
		// the audit copy failing would be unusual enough to surface rather
		// than silently drop the raw payload.
		return nil, ellipticErr(op).Code(pkgErrors.CodeEncodeFailed).
			Wrapf(err, "could not preserve the raw response")
	}

	rules := make([]compliance.RuleHit, 0, len(evaluationSource(resp)))
	for _, r := range evaluationSource(resp) {
		rules = append(rules, compliance.RuleHit{RuleName: r.RuleName, RuleType: r.RuleType})
	}

	return &compliance.Screening{
		AnalysisID:  resp.ID,
		ScreeningID: resp.ScreeningID,
		Address:     req.Address,
		Verdict:     deriveVerdict(resp, c.thresholds),
		RiskScore:   resp.RiskScore,
		Sanctioned:  isSanctioned(resp),
		Rules:       rules,
		ScreenedAt:  time.Now(),
		Raw:         rawResp,
	}, nil
}

func evaluationSource(resp walletScreeningResponse) []ruleEvaluation {
	if resp.EvaluationDetail == nil {
		return nil
	}
	return resp.EvaluationDetail.Source
}
