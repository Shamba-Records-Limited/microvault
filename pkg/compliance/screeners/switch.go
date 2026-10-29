package screeners

import (
	"context"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/Shamba-Records-Limited/microvault/pkg/compliance"
	"github.com/Shamba-Records-Limited/microvault/pkg/config"
)

// Available lists the providers this deployment can screen the vault with:
// those whose credentials are set and that support the vault's chain.
func Available(cfg config.ComplianceConfig) []string {
	var out []string
	if cfg.EllipticAPIKey != "" {
		out = append(out, config.ScreeningElliptic)
	}
	if cfg.MerkleAPIKey != "" {
		out = append(out, config.ScreeningMerkle)
	}
	return out
}

// Normalize checks a provider selection against Available and returns it
// deduplicated in canonical order.
func Normalize(cfg config.ComplianceConfig, names []string) ([]string, error) {
	avail := Available(cfg)
	picked := map[string]bool{}
	for _, n := range names {
		n = strings.ToLower(strings.TrimSpace(n))
		if n == "" {
			continue
		}
		if !slices.Contains(avail, n) {
			if n == config.ScreeningDidit {
				return nil, fmt.Errorf("screening provider didit cannot screen %s addresses: Didit's wallet screening does not support Stellar", VaultChain)
			}
			if slices.Contains(config.ScreeningProviderNames, n) {
				return nil, fmt.Errorf("screening provider %s has no credentials in this deployment", n)
			}
			return nil, fmt.Errorf("unknown screening provider %q", n)
		}
		picked[n] = true
	}
	if len(picked) == 0 {
		return nil, fmt.Errorf("choose at least one screening provider")
	}
	var out []string
	for _, n := range config.ScreeningProviderNames {
		if picked[n] {
			out = append(out, n)
		}
	}
	return out, nil
}

// Selection supplies the provider choice made in the portal; nil names mean
// no choice has been approved and the environment setting applies.
type Selection interface {
	CurrentProviders(ctx context.Context) ([]string, error)
}

// defaultSwitchTTL is how long a Switch trusts its last read of the
// selection.
const defaultSwitchTTL = 30 * time.Second

// Switch screens addresses and transactions with whichever providers the
// selection names at the time of the call, falling back to the environment
// setting only when nothing has been approved. If the selection cannot be
// read and was never read, the screening fails rather than guessing.
type Switch struct {
	cfg       config.ComplianceConfig
	th        Thresholds
	selection Selection
	boot      []string
	ttl       time.Duration
	now       func() time.Time
	logger    *slog.Logger

	mu       sync.Mutex
	key      string
	readAt   time.Time
	address  compliance.Screener
	tx       compliance.TransactionScreener
	resolved bool
}

// NewSwitch builds a Switch and checks the environment setting builds.
// selection may be nil to always use the environment setting.
func NewSwitch(cfg config.ComplianceConfig, th Thresholds, selection Selection, logger *slog.Logger) (*Switch, error) {
	if logger == nil {
		logger = slog.Default()
	}
	boot := cfg.ScreeningProviders
	if len(boot) == 0 {
		boot = []string{config.ScreeningElliptic}
	}
	s := &Switch{cfg: cfg, th: th, selection: selection, boot: boot, ttl: defaultSwitchTTL, now: time.Now, logger: logger.With("component", "screening_switch")}
	if _, _, err := s.build(boot); err != nil {
		return nil, err
	}
	return s, nil
}

// Boot returns the environment setting.
func (s *Switch) Boot() []string { return slices.Clone(s.boot) }

// ScreenAddress screens with the providers in force.
func (s *Switch) ScreenAddress(ctx context.Context, req compliance.ScreenRequest) (*compliance.Screening, error) {
	a, _, err := s.current(ctx)
	if err != nil {
		return nil, err
	}
	return a.ScreenAddress(ctx, req)
}

// ScreenTransaction screens with the providers in force.
func (s *Switch) ScreenTransaction(ctx context.Context, req compliance.TxScreenRequest) (*compliance.Screening, error) {
	_, t, err := s.current(ctx)
	if err != nil {
		return nil, err
	}
	return t.ScreenTransaction(ctx, req)
}

func (s *Switch) current(ctx context.Context) (compliance.Screener, compliance.TransactionScreener, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.resolved && s.now().Sub(s.readAt) < s.ttl {
		return s.address, s.tx, nil
	}
	names := s.boot
	if s.selection != nil {
		picked, err := s.selection.CurrentProviders(ctx)
		if err != nil {
			if s.resolved {
				s.logger.WarnContext(ctx, "could not read the screening provider choice; keeping the last one", "providers", s.key, "error", err)
				return s.address, s.tx, nil
			}
			return nil, nil, fmt.Errorf("read screening provider choice: %w", err)
		}
		if len(picked) > 0 {
			if _, err := Normalize(s.cfg, picked); err != nil {
				return nil, nil, fmt.Errorf("the screening providers approved in the portal (%s) cannot be used: %w", strings.Join(picked, ","), err)
			}
			names = picked
		}
	}
	key := strings.Join(names, ",")
	if !s.resolved || key != s.key {
		a, t, err := s.build(names)
		if err != nil {
			return nil, nil, err
		}
		if s.resolved {
			s.logger.InfoContext(ctx, "screening providers changed", "from", s.key, "to", key)
		}
		s.address, s.tx, s.key = a, t, key
	}
	s.resolved, s.readAt = true, s.now()
	return s.address, s.tx, nil
}

func (s *Switch) build(names []string) (compliance.Screener, compliance.TransactionScreener, error) {
	cfg := s.cfg
	cfg.ScreeningProviders = names
	a, err := New(cfg, s.th)
	if err != nil {
		return nil, nil, err
	}
	t, err := NewTransaction(cfg, s.th)
	if err != nil {
		return nil, nil, err
	}
	return a, t, nil
}
