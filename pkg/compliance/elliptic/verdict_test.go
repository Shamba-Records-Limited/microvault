package elliptic

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/Shamba-Records-Limited/microvault/pkg/compliance"
)

func f(v float64) *float64 { return &v }

func TestDeriveVerdict(t *testing.T) {
	sanctionedEntity := clusterEntity{
		EntityDetails: &entityDetails{Sanctions: []sanctionEntry{{List: sanctionList{Key: "ofac-sdn"}}}},
	}

	cases := []struct {
		name       string
		resp       walletScreeningResponse
		thresholds Thresholds
		want       compliance.Verdict
	}{
		{
			name:       "complete, no sanctions, score under threshold approves",
			resp:       walletScreeningResponse{ProcessStatus: "complete", RiskScore: f(10)},
			thresholds: Thresholds{Review: 50, Reject: 80},
			want:       compliance.VerdictApproved,
		},
		{
			name:       "score at or above reject threshold rejects",
			resp:       walletScreeningResponse{ProcessStatus: "complete", RiskScore: f(85)},
			thresholds: Thresholds{Review: 50, Reject: 80},
			want:       compliance.VerdictRejected,
		},
		{
			name:       "score between review and reject thresholds reviews",
			resp:       walletScreeningResponse{ProcessStatus: "complete", RiskScore: f(60)},
			thresholds: Thresholds{Review: 50, Reject: 80},
			want:       compliance.VerdictReview,
		},
		{
			name:       "sanctions hit rejects regardless of a low score",
			resp:       walletScreeningResponse{ProcessStatus: "complete", RiskScore: f(1), ClusterEntities: []clusterEntity{sanctionedEntity}},
			thresholds: Thresholds{Review: 50, Reject: 80},
			want:       compliance.VerdictRejected,
		},
		{
			name:       "sanctions hit rejects even when score is null",
			resp:       walletScreeningResponse{ProcessStatus: "complete", RiskScore: nil, ClusterEntities: []clusterEntity{sanctionedEntity}},
			thresholds: Thresholds{Review: 50, Reject: 80},
			want:       compliance.VerdictRejected,
		},
		{
			name:       "null risk score on an otherwise clean complete analysis is review, never approved",
			resp:       walletScreeningResponse{ProcessStatus: "complete", RiskScore: nil},
			thresholds: Thresholds{Review: 50, Reject: 80},
			want:       compliance.VerdictReview,
		},
		{
			name:       "process_status running is pending even with a score present",
			resp:       walletScreeningResponse{ProcessStatus: "running", RiskScore: f(1)},
			thresholds: Thresholds{Review: 50, Reject: 80},
			want:       compliance.VerdictPending,
		},
		{
			name:       "an error on the analysis is pending",
			resp:       walletScreeningResponse{ProcessStatus: "error", Error: &errorDetail{Message: "internal issue"}},
			thresholds: Thresholds{Review: 50, Reject: 80},
			want:       compliance.VerdictPending,
		},
		{
			name:       "no thresholds configured defaults a scored, unsanctioned analysis to review",
			resp:       walletScreeningResponse{ProcessStatus: "complete", RiskScore: f(1)},
			thresholds: Thresholds{},
			want:       compliance.VerdictReview,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, deriveVerdict(tc.resp, tc.thresholds))
		})
	}
}

func TestIsSanctioned(t *testing.T) {
	t.Run("no cluster entities is not sanctioned", func(t *testing.T) {
		assert.False(t, isSanctioned(walletScreeningResponse{}))
	})

	t.Run("a cluster entity with no sanctions entries is not sanctioned", func(t *testing.T) {
		resp := walletScreeningResponse{ClusterEntities: []clusterEntity{{EntityDetails: &entityDetails{}}}}
		assert.False(t, isSanctioned(resp))
	})

	t.Run("any clustered entity with a sanctions entry is sanctioned", func(t *testing.T) {
		resp := walletScreeningResponse{ClusterEntities: []clusterEntity{
			{},
			{EntityDetails: &entityDetails{Sanctions: []sanctionEntry{{List: sanctionList{Key: "ofac-sdn"}}}}},
		}}
		assert.True(t, isSanctioned(resp))
	})

	t.Run("is_after_sanction_date decodes distinctly per entity", func(t *testing.T) {
		resp := walletScreeningResponse{ClusterEntities: []clusterEntity{
			{IsAfterSanctionDate: true},
			{IsAfterSanctionDate: false},
		}}
		assert.True(t, resp.ClusterEntities[0].IsAfterSanctionDate)
		assert.False(t, resp.ClusterEntities[1].IsAfterSanctionDate)
	})
}
