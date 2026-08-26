package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Unset must mean off, so adding the flag routes every existing deployment to
// YellowCard exactly as before.
func TestRelaySwitchDefaultsOff(t *testing.T) {
	setRequiredEnv(t)

	cfg, err := New()
	require.NoError(t, err)
	assert.False(t, cfg.Payments.EnableProviderRelaySwitch)
}

func TestRelaySwitchEnabled(t *testing.T) {
	setRequiredEnv(t)
	t.Setenv("ENABLE_PAYMENT_PROVIDER_RELAY_SWITCH", "true")

	cfg, err := New()
	require.NoError(t, err)
	assert.True(t, cfg.Payments.EnableProviderRelaySwitch)
}

func TestRelaySwitchRejectsNonBoolean(t *testing.T) {
	setRequiredEnv(t)
	t.Setenv("ENABLE_PAYMENT_PROVIDER_RELAY_SWITCH", "yes-please")

	_, err := New()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "ENABLE_PAYMENT_PROVIDER_RELAY_SWITCH")
}

func TestEnvBool(t *testing.T) {
	tests := map[string]struct {
		raw     string
		want    bool
		wantErr bool
	}{
		"unset": {"", false, false},
		"true":  {"true", true, false},
		"1":     {"1", true, false},
		"false": {"false", false, false},
		"0":     {"0", false, false},
		"junk":  {"maybe", false, true},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			if tt.raw != "" {
				t.Setenv("TEST_ENV_BOOL", tt.raw)
			}
			got, err := envBool("TEST_ENV_BOOL")
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}
