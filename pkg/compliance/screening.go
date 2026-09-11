package compliance

import (
	"context"
	"encoding/json"
	"time"
)

// ScreenRequest is what a caller submits for screening.
type ScreenRequest struct {
	// Address is the blockchain address to screen (a Stellar G... account
	// for wallet screening today).
	Address string
	// CustomerReference joins the screening to a KYB entity on the
	// provider's side — see the source design doc §4 on why this is the
	// counterparty's own identifier, decided before the first call.
	CustomerReference string
}

// Screener screens a blockchain address and reports what is known about it.
type Screener interface {
	ScreenAddress(ctx context.Context, req ScreenRequest) (*Screening, error)
}

// Verdict is the policy decision derived from a screening, per the source
// design doc §10. It is a decision, not a score — Screening carries the
// score this was derived from.
type Verdict string

const (
	// VerdictApproved: complete, no sanctions, score under threshold.
	VerdictApproved Verdict = "approved"
	// VerdictRejected: sanctions hit, or score over the hard threshold.
	// Final until a human overrides it.
	VerdictRejected Verdict = "rejected"
	// VerdictReview: score between the soft and hard thresholds, or a
	// review-worthy rule fired. Not permitted by default.
	VerdictReview Verdict = "review"
	// VerdictUnscreenable: the address has no on-chain history for the
	// provider to judge (Elliptic's 404 NotInBlockchain). Not a pass or a
	// fail — see the source design doc §4's "404 is the case that will
	// actually happen".
	VerdictUnscreenable Verdict = "unscreenable"
	// VerdictPending: the provider has not finished analysing the address,
	// or the call itself failed.
	VerdictPending Verdict = "pending"
)

// RuleHit names one provider-configured rule that fired during screening.
// Rules are configured in the provider's own UI, not by us — this surfaces
// what a compliance officer already decided, it does not replicate it.
type RuleHit struct {
	RuleName   string
	RuleType   string
	Sanctioned bool
}

// Screening is the provider-neutral result of screening one address.
type Screening struct {
	// AnalysisID and ScreeningID identify this screening for later lookup
	// against the provider directly (an audit trail beyond Raw).
	AnalysisID  string
	ScreeningID string
	Address     string
	Verdict     Verdict

	// RiskScore is a pointer deliberately: the provider can return null,
	// and a zero-valued float would read as "perfectly clean" — the single
	// most dangerous silent default available in this integration.
	RiskScore *float64

	Sanctioned bool
	Rules      []RuleHit
	ScreenedAt time.Time

	// Raw is the provider's full response, kept because a compliance
	// record has to be reproducible years later and a struct designed
	// today will not have a field for whatever the provider adds later.
	Raw json.RawMessage
}
