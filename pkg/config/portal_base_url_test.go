package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPortalBaseURL(t *testing.T) {
	tests := []struct {
		name     string
		env      string
		override string
		want     string
	}{
		{name: "testnet default", env: "development", want: "https://portal-testnet.shambarecords.com"},
		{name: "mainnet default", env: "staging", want: "https://portal.shambarecords.com"},
		{name: "override wins", env: "development", override: "https://portal.example.test/", want: "https://portal.example.test"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setRequiredEnv(t)
			t.Setenv("SERVER_ENVIRONMENT", tt.env)
			t.Setenv("PORTAL_BASE_URL", tt.override)

			cfg, err := New()
			require.NoError(t, err)
			assert.Equal(t, tt.want, cfg.Server.PortalBaseURL)
		})
	}
}
