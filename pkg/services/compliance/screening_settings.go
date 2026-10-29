package compliance

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/Shamba-Records-Limited/microvault/pkg/models"
	"github.com/Shamba-Records-Limited/microvault/pkg/repository"
)

var (
	// ErrSelfApproval means the proposer tried to decide their own change.
	ErrSelfApproval = errors.New("a second operator must decide this change")
	// ErrReasonRequired means a provider change was proposed without a reason.
	ErrReasonRequired = errors.New("give a reason for the change")
	// ErrNoProviderChange means the proposal matches the providers in force.
	ErrNoProviderChange = errors.New("these providers are already in force")
)

// ProviderValidator checks and normalises a provider selection.
type ProviderValidator func(names []string) ([]string, error)

// ScreeningSettings runs the two-person change of the screening providers.
// The latest approved change is in force; before any, the environment
// setting is.
type ScreeningSettings struct {
	repo     repository.ScreeningSettingRepository
	validate ProviderValidator
	boot     []string
	now      func() time.Time
}

// NewScreeningSettings builds the service. boot is the environment setting.
func NewScreeningSettings(repo repository.ScreeningSettingRepository, validate ProviderValidator, boot []string) *ScreeningSettings {
	return &ScreeningSettings{repo: repo, validate: validate, boot: boot, now: time.Now}
}

// ScreeningProviderState is what the portal shows.
type ScreeningProviderState struct {
	InForce  []string
	FromEnv  bool
	Boot     []string
	Approved *models.ScreeningProviderSetting
	Open     *models.ScreeningProviderSetting
	History  []*models.ScreeningProviderSetting
}

// CurrentProviders returns the approved selection, or nil when none.
func (s *ScreeningSettings) CurrentProviders(ctx context.Context) ([]string, error) {
	cur, err := s.repo.Current(ctx)
	if err != nil || cur == nil {
		return nil, err
	}
	return cur.ProviderList(), nil
}

// State returns the providers in force, the waiting change and the history.
func (s *ScreeningSettings) State(ctx context.Context, limit int) (*ScreeningProviderState, error) {
	cur, err := s.repo.Current(ctx)
	if err != nil {
		return nil, err
	}
	hist, err := s.repo.List(ctx, limit)
	if err != nil {
		return nil, err
	}
	st := &ScreeningProviderState{InForce: s.boot, FromEnv: true, Boot: s.boot, Approved: cur, History: hist}
	if cur != nil {
		st.InForce, st.FromEnv = cur.ProviderList(), false
	}
	for _, h := range hist {
		if h.Status == models.ScreeningSettingProposed {
			st.Open = h
			break
		}
	}
	return st, nil
}

// Propose records a change for a second operator to decide.
func (s *ScreeningSettings) Propose(ctx context.Context, names []string, reason, actor string) (*models.ScreeningProviderSetting, error) {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return nil, ErrReasonRequired
	}
	picked, err := s.validate(names)
	if err != nil {
		return nil, err
	}
	inForce, err := s.CurrentProviders(ctx)
	if err != nil {
		return nil, err
	}
	if inForce == nil {
		inForce = s.boot
	}
	if strings.Join(picked, ",") == strings.Join(inForce, ",") {
		return nil, ErrNoProviderChange
	}
	setting := &models.ScreeningProviderSetting{
		Providers:  strings.Join(picked, ","),
		Reason:     reason,
		Status:     models.ScreeningSettingProposed,
		ProposedBy: actor,
		ProposedAt: s.now(),
	}
	if err := s.repo.Propose(ctx, setting); err != nil {
		return nil, err
	}
	return setting, nil
}

// Approve puts a waiting change in force. The proposer cannot approve it,
// and the providers are checked again against this deployment.
func (s *ScreeningSettings) Approve(ctx context.Context, id, actor, note string) error {
	setting, err := s.decidable(ctx, id, actor)
	if err != nil {
		return err
	}
	if _, err := s.validate(setting.ProviderList()); err != nil {
		return err
	}
	return s.repo.Decide(ctx, id, models.ScreeningSettingApproved, actor, strings.TrimSpace(note), s.now())
}

// Reject closes a waiting change. The proposer may withdraw their own.
func (s *ScreeningSettings) Reject(ctx context.Context, id, actor, note string) error {
	if _, err := s.repo.Get(ctx, id); err != nil {
		return err
	}
	return s.repo.Decide(ctx, id, models.ScreeningSettingRejected, actor, strings.TrimSpace(note), s.now())
}

func (s *ScreeningSettings) decidable(ctx context.Context, id, actor string) (*models.ScreeningProviderSetting, error) {
	setting, err := s.repo.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if setting.Status != models.ScreeningSettingProposed {
		return nil, repository.ErrScreeningProposalNotOpen
	}
	if setting.ProposedBy == actor {
		return nil, ErrSelfApproval
	}
	return setting, nil
}
