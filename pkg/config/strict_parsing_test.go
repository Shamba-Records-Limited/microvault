package config

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOptionalSettingsDefaults(t *testing.T) {
	setRequiredEnv(t)

	cfg, err := New()
	require.NoError(t, err)
	assert.Zero(t, cfg.Redis.DBNumber)
	assert.Equal(t, 24*time.Hour, cfg.Redis.IdempotencyTTL)
	assert.Equal(t, 5*time.Minute, cfg.Mobile.SessionTimeout)
	assert.Equal(t, 15*time.Minute, cfg.Auth.PINLockoutDuration)
	assert.Zero(t, cfg.Payments.Mpesa.CollectionShortcode)
	assert.Equal(t, "https://sandbox-api.fonbnk.com", cfg.Payments.Fonbnk.BaseURL)
}

func TestOptionalSettingsRejectMalformedValues(t *testing.T) {
	tests := []struct{ name, key, value string }{
		{"redis db not numeric", "REDIS_DB_NUMBER", "one"},
		{"redis db negative", "REDIS_DB_NUMBER", "-1"},
		{"idempotency ttl with unit", "REDIS_IDEMPOTENCY_TTL", "24h"},
		{"ussd session timeout zero", "USSD_SESSION_TIMEOUT", "0"},
		{"pin lockout with unit", "PIN_LOCKOUT_SECONDS", "15m"},
		{"collection shortcode with letter", "MPESA_COLLECTION_SHORTCODE", "4O1234"},
		{"disbursement shortcode negative", "MPESA_DISBURSEMENT_SHORTCODE", "-600000"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setRequiredEnv(t)
			t.Setenv(tt.key, tt.value)

			_, err := New()
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.key)
		})
	}
}

func TestOptionalSettingsParseValidValues(t *testing.T) {
	setRequiredEnv(t)
	t.Setenv("REDIS_DB_NUMBER", "3")
	t.Setenv("REDIS_IDEMPOTENCY_TTL", "3600")
	t.Setenv("USSD_SESSION_TIMEOUT", "120")
	t.Setenv("PIN_LOCKOUT_SECONDS", "600")
	t.Setenv("MPESA_COLLECTION_SHORTCODE", "4012345")
	t.Setenv("MPESA_DISBURSEMENT_SHORTCODE", "600000")

	cfg, err := New()
	require.NoError(t, err)
	assert.Equal(t, 3, cfg.Redis.DBNumber)
	assert.Equal(t, time.Hour, cfg.Redis.IdempotencyTTL)
	assert.Equal(t, 2*time.Minute, cfg.Mobile.SessionTimeout)
	assert.Equal(t, 10*time.Minute, cfg.Auth.PINLockoutDuration)
	assert.Equal(t, uint(4012345), cfg.Payments.Mpesa.CollectionShortcode)
	assert.Equal(t, uint(600000), cfg.Payments.Mpesa.DisbursementShortcode)
}

func TestFonbnkBaseURLRequiredInProduction(t *testing.T) {
	setRequiredEnv(t)
	t.Setenv("SERVER_ENVIRONMENT", "production")

	_, err := New()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "FONBNK_BASE_URL")

	t.Setenv("FONBNK_BASE_URL", "https://api.fonbnk.com")
	cfg, err := New()
	require.NoError(t, err)
	assert.Equal(t, "https://api.fonbnk.com", cfg.Payments.Fonbnk.BaseURL)
}
