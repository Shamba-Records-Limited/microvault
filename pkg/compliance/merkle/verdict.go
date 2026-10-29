package merkle

import (
	"strings"

	"github.com/Shamba-Records-Limited/microvault/pkg/compliance"
)

// Thresholds are Compass risk_level cut points (1 Info … 5 Critical). Zero
// means unset; an unset pair routes every scored address to review.
type Thresholds struct {
	Review int
	Reject int
}

const sanctionsTag = "sanctions"

func isSanctionsTag(t *tag) bool {
	return t != nil && strings.EqualFold(strings.TrimSpace(t.TagType), sanctionsTag)
}

// sanctioned reports an address that is itself attributed to a sanctioned
// owner or user, or that has direct exposure to one.
func sanctioned(r screenResponse) bool {
	if r.Tags != nil && (isSanctionsTag(r.Tags.Owner) || isSanctionsTag(r.Tags.User)) {
		return true
	}
	for _, list := range [][]tag{r.Originator, r.Beneficiary} {
		for i := range list {
			if isSanctionsTag(&list[i]) && !strings.EqualFold(list[i].Exposure, "indirect") {
				return true
			}
		}
	}
	return false
}

// indirectSanctions reports exposure to sanctions through intermediaries.
func indirectSanctions(r screenResponse) bool {
	for _, list := range [][]tag{r.Originator, r.Beneficiary} {
		for i := range list {
			if isSanctionsTag(&list[i]) && strings.EqualFold(list[i].Exposure, "indirect") {
				return true
			}
		}
	}
	return false
}

// unscreenable reports an address with no on-chain history for Compass to
// judge, the counterpart of Elliptic's 404.
func unscreenable(r screenResponse) bool {
	return r.EarliestTxTime == nil && r.LatestTxTime == nil && len(r.Originator) == 0 && len(r.Beneficiary) == 0
}

// deriveVerdict applies the shared policy: sanctions are absolute, indirect
// sanctions exposure always needs a person, then the risk level against
// thresholds, never approving when thresholds are unset.
func deriveVerdict(r screenResponse, th Thresholds, isAddress bool) compliance.Verdict {
	if sanctioned(r) {
		return compliance.VerdictRejected
	}
	if indirectSanctions(r) {
		return compliance.VerdictReview
	}
	if isAddress && unscreenable(r) {
		return compliance.VerdictUnscreenable
	}
	if r.RiskLevel == nil {
		return compliance.VerdictPending
	}
	level := *r.RiskLevel
	switch {
	case th.Reject > 0 && level >= th.Reject:
		return compliance.VerdictRejected
	case th.Review > 0 && level >= th.Review:
		return compliance.VerdictReview
	case th.Reject == 0 && th.Review == 0:
		return compliance.VerdictReview
	default:
		return compliance.VerdictApproved
	}
}

func rules(r screenResponse) []compliance.RuleHit {
	out := make([]compliance.RuleHit, 0, len(r.Alerts))
	for _, a := range r.Alerts {
		out = append(out, compliance.RuleHit{
			RuleName:   a.RuleTriggered,
			RuleType:   a.Policy,
			Sanctioned: strings.Contains(strings.ToLower(a.RuleTriggered), "ofac") || strings.Contains(strings.ToLower(a.RuleTriggered), sanctionsTag),
		})
	}
	return out
}
