// Package screeners builds the address screener the configuration selects:
// one provider, or several combined with the stricter verdict winning.
package screeners

import (
	"fmt"

	"github.com/Shamba-Records-Limited/microvault/pkg/compliance"
	"github.com/Shamba-Records-Limited/microvault/pkg/compliance/didit"
	"github.com/Shamba-Records-Limited/microvault/pkg/compliance/elliptic"
	"github.com/Shamba-Records-Limited/microvault/pkg/compliance/merkle"
	"github.com/Shamba-Records-Limited/microvault/pkg/config"
)

// VaultChain is the chain the vault's depositors live on.
const VaultChain = "stellar"

// Thresholds carries Elliptic's cut points, which are not in config yet.
type Thresholds struct {
	Elliptic elliptic.Thresholds
}

// New returns the configured screener for vault depositor addresses. It
// refuses a provider that cannot screen the vault's chain rather than
// silently screening with something else.
func New(cfg config.ComplianceConfig, th Thresholds) (compliance.Screener, error) {
	names := cfg.ScreeningProviders
	if len(names) == 0 {
		names = []string{config.ScreeningElliptic}
	}
	providers := make([]compliance.Named, 0, len(names))
	for _, name := range names {
		var s compliance.Screener
		switch name {
		case config.ScreeningElliptic:
			s = elliptic.NewClient(elliptic.Config{
				APIKey: cfg.EllipticAPIKey, APISecret: cfg.EllipticAPISecret,
				BaseURL: cfg.EllipticBaseURL, Thresholds: th.Elliptic,
			})
		case config.ScreeningMerkle:
			s = merkle.NewClient(merkle.Config{
				APIKey: cfg.MerkleAPIKey, BaseURL: cfg.MerkleBaseURL,
				Thresholds: merkle.Thresholds{Review: cfg.MerkleReviewLevel, Reject: cfg.MerkleRejectLevel},
			})
		case config.ScreeningDidit:
			if !didit.Supports(VaultChain) {
				return nil, fmt.Errorf("screening provider didit cannot screen %s addresses (Didit's screening has no Stellar network); use merkle or elliptic — vault doc Security & Compliance/screening-provider-toggle-plan.md", VaultChain)
			}
			s = didit.NewClient(didit.Config{
				APIKey: cfg.DiditAPIKey, BaseURL: cfg.DiditBaseURL, Chain: VaultChain,
				Thresholds: didit.Thresholds{Review: cfg.DiditReviewScore, Reject: cfg.DiditRejectScore},
			})
		default:
			return nil, fmt.Errorf("unknown screening provider %q", name)
		}
		providers = append(providers, compliance.Named{Name: name, Screener: s})
	}
	return compliance.Combine(providers...), nil
}
