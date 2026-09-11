package elliptic

import "github.com/Shamba-Records-Limited/microvault/pkg/compliance"

// deriveVerdict applies the source design doc §10 policy to a decoded
// response, in the order that section specifies:
//
//  1. Sanctions membership is absolute — never reachable by a threshold.
//  2. process_status of anything but "complete" means there is no verdict
//     yet, regardless of what risk_score says.
//  3. The score, against thresholds — deliberately NOT implemented here.
//     Thresholds are a business setting the doc says belongs in config or
//     the admin (§10, §17 Q8: "configurable at runtime"), not a constant
//     in this package. This function returns VerdictReview whenever a
//     score is present and the address is not sanctioned, leaving the
//     approve/reject/soft-review split to whatever holds the threshold —
//     see the caller-supplied Threshold in ScreenAddress's options.
func deriveVerdict(resp walletScreeningResponse, thresholds Thresholds) compliance.Verdict {
	if resp.ProcessStatus != "" && resp.ProcessStatus != "complete" {
		return compliance.VerdictPending
	}
	if resp.Error != nil && resp.Error.Message != "" {
		return compliance.VerdictPending
	}

	if isSanctioned(resp) {
		return compliance.VerdictRejected
	}

	if resp.RiskScore == nil {
		// A complete analysis with no score is not the same as a clean
		// score of zero — see compliance.Screening.RiskScore's doc
		// comment. Route to review rather than silently approving.
		return compliance.VerdictReview
	}

	score := *resp.RiskScore
	switch {
	case thresholds.Reject > 0 && score >= thresholds.Reject:
		return compliance.VerdictRejected
	case thresholds.Review > 0 && score >= thresholds.Review:
		return compliance.VerdictReview
	case thresholds.Reject == 0 && thresholds.Review == 0:
		// No thresholds configured — a complete, unsanctioned analysis
		// with a score present still needs a human decision on where the
		// line is, so default to review rather than approve.
		return compliance.VerdictReview
	default:
		return compliance.VerdictApproved
	}
}

// isSanctioned checks entity_details.sanctions for any clustered entity —
// the doc's primary, "absolute" sanctions signal. The doc also mentions a
// secondary signal (a matched rule's risk_triggers.is_sanctioned), whose
// exact nesting under evaluation_detail isn't confirmed from the public
// reference; not wired here to avoid a false sense of coverage from an
// unverified field path. Revisit once a real response is in hand.
func isSanctioned(resp walletScreeningResponse) bool {
	for _, entity := range resp.ClusterEntities {
		if entity.EntityDetails != nil && len(entity.EntityDetails.Sanctions) > 0 {
			return true
		}
	}
	return false
}

// Thresholds are the risk-score cut points the doc says belong in
// runtime config (§17 Q8), not a constant. Zero means "not configured";
// see deriveVerdict for what that does.
type Thresholds struct {
	// Review is the soft threshold: at or above it, an otherwise-clean
	// score routes to VerdictReview instead of VerdictApproved.
	Review float64
	// Reject is the hard threshold: at or above it, the verdict is
	// VerdictRejected outright.
	Reject float64
}
