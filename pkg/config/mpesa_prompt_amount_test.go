package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Unset means prompts carry the quoted payoff.
func TestMpesaPromptAmountDefaultsOff(t *testing.T) {
	setRequiredEnv(t)

	cfg, err := New()
	require.NoError(t, err)
	assert.Zero(t, cfg.Payments.Mpesa.PromptAmountKES)
}

func TestMpesaPromptAmountSet(t *testing.T) {
	setRequiredEnv(t)
	t.Setenv("MPESA_PROMPT_AMOUNT_KES", "1")

	cfg, err := New()
	require.NoError(t, err)
	assert.Equal(t, 1, cfg.Payments.Mpesa.PromptAmountKES)
}

// The override exists to charge a real handset a token amount against
// Daraja's simulator-less sandbox; in production it would under-collect
// every repayment, so it is a boot error there.
func TestMpesaPromptAmountRejectedInProduction(t *testing.T) {
	setRequiredEnv(t)
	t.Setenv("SERVER_ENVIRONMENT", "production")
	t.Setenv("MPESA_PROMPT_AMOUNT_KES", "1")

	_, err := New()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "MPESA_PROMPT_AMOUNT_KES")
}

func TestMpesaPromptAmountRejectsGarbage(t *testing.T) {
	setRequiredEnv(t)
	t.Setenv("MPESA_PROMPT_AMOUNT_KES", "lots")

	_, err := New()
	require.Error(t, err)
}
