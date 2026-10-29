package didit

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/Shamba-Records-Limited/microvault/pkg/compliance"

	pkgErrors "github.com/Shamba-Records-Limited/microvault/pkg/errors"
)

var _ compliance.Screener = (*Client)(nil)

// ErrUnsupportedChain means Didit's screening has no network for the chain.
var ErrUnsupportedChain = errors.New("didit: screening does not support this chain")

// chains maps our chain names to Didit's blockchain identifiers; an empty
// value is a chain Didit cannot screen. Stellar is absent from Didit's enum
// (checked 2026-10-29), and "USDC" there means Ethereum, never Stellar.
var chains = map[string]string{
	"stellar":  "",
	"ethereum": "ETH",
	"bitcoin":  "BTC",
	"tron":     "TRX",
	"solana":   "SOL",
	"polygon":  "MATIC",
	"base":     "BASE",
}

// Supports reports whether Didit can screen addresses on chain.
func Supports(chain string) bool {
	return chains[strings.ToLower(chain)] != ""
}

// Thresholds are cut points on Didit's 0–100 risk_score. Zero means unset;
// an unset pair routes every scored address to review.
type Thresholds struct {
	Review int
	Reject int
}

type walletRequest struct {
	WalletAddress string `json:"wallet_address"`
	Blockchain    string `json:"blockchain"`
	Direction     string `json:"direction,omitempty"`
}

type walletResponse struct {
	Provider             string  `json:"provider"`
	RiskScore            *int    `json:"risk_score"`
	Severity             string  `json:"severity"`
	Status               string  `json:"status"`
	SanctionsHit         bool    `json:"sanctions_hit"`
	DominantRiskCategory *string `json:"dominant_risk_category"`
	ReportSignature      string  `json:"report_signature"`
	RiskFactors          []struct {
		Category   string `json:"category"`
		IsHighRisk bool   `json:"is_high_risk"`
	} `json:"risk_factors"`
}

// ScreenAddress screens a deposit address through Didit's connected provider.
// A chain Didit cannot screen fails before any call is made.
func (c *Client) ScreenAddress(ctx context.Context, req compliance.ScreenRequest) (*compliance.Screening, error) {
	const op = "screen_address"
	network := chains[strings.ToLower(c.chain)]
	if network == "" {
		return nil, diditErr(op).Code(pkgErrors.CodeInvalidConfig).With("chain", c.chain).
			Wrapf(ErrUnsupportedChain, "didit cannot screen %s addresses", c.chain)
	}
	if req.Address == "" {
		return nil, diditErr(op).Code(pkgErrors.CodeMissingAccount).Errorf("address is empty")
	}
	var resp walletResponse
	raw, err := c.do(ctx, op, http.MethodPost, "/v3/wallet-screening/", walletRequest{
		WalletAddress: req.Address, Blockchain: network, Direction: "deposit",
	}, &resp)
	if err != nil {
		return nil, err
	}

	var score *float64
	if resp.RiskScore != nil {
		v := float64(*resp.RiskScore)
		score = &v
	}
	rules := make([]compliance.RuleHit, 0, len(resp.RiskFactors))
	for _, f := range resp.RiskFactors {
		if f.IsHighRisk {
			rules = append(rules, compliance.RuleHit{RuleName: f.Category, RuleType: "risk_factor", Sanctioned: f.Category == "sanctioned"})
		}
	}
	return &compliance.Screening{
		Provider:    Provider,
		ProviderRef: resp.Provider,
		Address:     req.Address,
		Verdict:     deriveVerdict(resp, c.thresholds),
		RiskScore:   score,
		Sanctioned:  resp.SanctionsHit,
		Rules:       rules,
		ScreenedAt:  time.Now(),
		Raw:         raw,
	}, nil
}

// deriveVerdict applies the shared policy to a Didit screening result.
func deriveVerdict(r walletResponse, th Thresholds) compliance.Verdict {
	if r.SanctionsHit {
		return compliance.VerdictRejected
	}
	if r.Status != "SCREENED" || r.RiskScore == nil {
		return compliance.VerdictPending
	}
	score := *r.RiskScore
	switch {
	case th.Reject > 0 && score >= th.Reject:
		return compliance.VerdictRejected
	case th.Review > 0 && score >= th.Review:
		return compliance.VerdictReview
	case th.Reject == 0 && th.Review == 0:
		return compliance.VerdictReview
	default:
		return compliance.VerdictApproved
	}
}
