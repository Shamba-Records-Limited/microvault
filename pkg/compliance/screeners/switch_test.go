package screeners

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Shamba-Records-Limited/microvault/pkg/compliance/elliptic"
	"github.com/Shamba-Records-Limited/microvault/pkg/compliance/merkle"
	"github.com/Shamba-Records-Limited/microvault/pkg/config"
)

type fakeSelection struct {
	names []string
	err   error
	reads int
}

func (f *fakeSelection) CurrentProviders(context.Context) ([]string, error) {
	f.reads++
	return f.names, f.err
}

var bothKeys = config.ComplianceConfig{EllipticAPIKey: "e", MerkleAPIKey: "m"}

func TestNormalize(t *testing.T) {
	got, err := Normalize(bothKeys, []string{" Merkle", "elliptic", "merkle", ""})
	require.NoError(t, err)
	assert.Equal(t, []string{"elliptic", "merkle"}, got, "deduplicated in canonical order")

	for name, names := range map[string][]string{
		"no credentials": {"merkle"},
		"didit":          {"didit"},
		"unknown":        {"chainalysis"},
		"empty":          {},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := Normalize(config.ComplianceConfig{EllipticAPIKey: "e"}, names)
			assert.Error(t, err)
		})
	}
	assert.Equal(t, []string{"elliptic", "merkle"}, Available(bothKeys))
}

func newTestSwitch(t *testing.T, sel Selection) (*Switch, *time.Time) {
	t.Helper()
	s, err := NewSwitch(bothKeys, Thresholds{}, sel, slog.New(slog.DiscardHandler))
	require.NoError(t, err)
	now := time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC)
	s.now = func() time.Time { return now }
	return s, &now
}

func TestSwitchFollowsTheApprovedSelection(t *testing.T) {
	sel := &fakeSelection{}
	s, now := newTestSwitch(t, sel)

	a, _, err := s.current(context.Background())
	require.NoError(t, err)
	assert.IsType(t, &elliptic.Client{}, a, "no approved choice uses the environment setting")

	sel.names = []string{"merkle"}
	a, _, err = s.current(context.Background())
	require.NoError(t, err)
	assert.IsType(t, &elliptic.Client{}, a, "the last read is trusted within the TTL")
	assert.Equal(t, 1, sel.reads)

	*now = now.Add(time.Minute)
	a, tx, err := s.current(context.Background())
	require.NoError(t, err)
	assert.IsType(t, &merkle.Client{}, a)
	assert.IsType(t, &merkle.Client{}, tx)
}

func TestSwitchReadFailures(t *testing.T) {
	sel := &fakeSelection{err: errors.New("db down")}
	s, now := newTestSwitch(t, sel)
	_, _, err := s.current(context.Background())
	require.Error(t, err, "never guesses before the first successful read")

	sel.err, sel.names = nil, []string{"merkle"}
	_, _, err = s.current(context.Background())
	require.NoError(t, err)

	sel.err = errors.New("db down")
	*now = now.Add(time.Minute)
	a, _, err := s.current(context.Background())
	require.NoError(t, err)
	assert.IsType(t, &merkle.Client{}, a, "keeps the last known choice")
}

func TestSwitchRefusesAnApprovedProviderWithoutCredentials(t *testing.T) {
	sel := &fakeSelection{names: []string{"merkle"}}
	s, err := NewSwitch(config.ComplianceConfig{EllipticAPIKey: "e"}, Thresholds{}, sel, slog.New(slog.DiscardHandler))
	require.NoError(t, err)
	_, _, err = s.current(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no credentials")
}
