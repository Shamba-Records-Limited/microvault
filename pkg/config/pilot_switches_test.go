package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Unset must mean on: mobile money payouts are live on testnet today, so
// adding the switch changes no existing deployment.
func TestMobileMoneyBorrowDefaultsOn(t *testing.T) {
	setRequiredEnv(t)

	cfg, err := New()
	require.NoError(t, err)
	assert.True(t, cfg.Payments.MobileMoneyBorrow)
}

func TestMobileMoneyBorrowDisabled(t *testing.T) {
	setRequiredEnv(t)
	t.Setenv("ENABLE_MOBILE_MONEY_BORROW", "false")

	cfg, err := New()
	require.NoError(t, err)
	assert.False(t, cfg.Payments.MobileMoneyBorrow)
}

func TestMobileMoneyBorrowRejectsNonBoolean(t *testing.T) {
	setRequiredEnv(t)
	t.Setenv("ENABLE_MOBILE_MONEY_BORROW", "off")

	_, err := New()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "ENABLE_MOBILE_MONEY_BORROW")
}

func TestPilotAccessGateDefaultsOff(t *testing.T) {
	setRequiredEnv(t)

	cfg, err := New()
	require.NoError(t, err)
	assert.False(t, cfg.Mobile.PilotAccessGate)
}

func TestPilotAccessGateEnabled(t *testing.T) {
	setRequiredEnv(t)
	t.Setenv("ENABLE_PILOT_ACCESS_GATE", "true")

	cfg, err := New()
	require.NoError(t, err)
	assert.True(t, cfg.Mobile.PilotAccessGate)
}
