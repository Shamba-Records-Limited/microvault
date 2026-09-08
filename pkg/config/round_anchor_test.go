package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Unset must mean off: full stroop precision is the default, so adding the
// flag changes no existing deployment.
func TestRoundAnchorAmountsDefaultsOff(t *testing.T) {
	setRequiredEnv(t)

	cfg, err := New()
	require.NoError(t, err)
	assert.False(t, cfg.Payments.RoundAnchorAmounts)
}

func TestRoundAnchorAmountsEnabled(t *testing.T) {
	setRequiredEnv(t)
	t.Setenv("ROUND_ANCHOR_AMOUNTS", "true")

	cfg, err := New()
	require.NoError(t, err)
	assert.True(t, cfg.Payments.RoundAnchorAmounts)
}

func TestRoundAnchorAmountsRejectsNonBoolean(t *testing.T) {
	setRequiredEnv(t)
	t.Setenv("ROUND_ANCHOR_AMOUNTS", "yes-please")

	_, err := New()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "ROUND_ANCHOR_AMOUNTS")
}
