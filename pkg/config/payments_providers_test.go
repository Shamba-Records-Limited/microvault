package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Shamba-Records-Limited/microvault/pkg/payment/chipper"
)

func TestParseMethodCodes(t *testing.T) {
	codes, err := parseMethodCodes(" mpesa = KE_MPESA , AIRTEL=ke_airtel,")
	require.NoError(t, err)
	assert.Equal(t, map[string]string{"MPESA": "ke_mpesa", "AIRTEL": "ke_airtel"}, codes)

	empty, err := parseMethodCodes("")
	require.NoError(t, err)
	assert.Empty(t, empty)

	for _, bad := range []string{"MPESA", "=ke_mpesa", "MPESA="} {
		_, err := parseMethodCodes(bad)
		assert.Error(t, err, bad)
	}
}

func TestChipperConfigValidate(t *testing.T) {
	fees, err := chipper.ParseFeeSchedule(`{"deposit":{"pct":"1.5"},"payouts":{"ke_mpesa":{"pct":"1.5"}}}`)
	require.NoError(t, err)
	full := ChipperConfig{
		APIKey: "sk_test_x", WebhookSecret: "whsec_x", DepositAddress: "G...", DepositMemo: "1",
		MethodCodes: map[string]string{"MPESA": "ke_mpesa"}, FeeSchedule: fees,
	}

	assert.NoError(t, (&ChipperConfig{}).Validate("production"), "an unconfigured provider is valid")
	assert.NoError(t, full.Validate("development"))

	half := full
	half.DepositMemo, half.FeeSchedule = "", nil
	err = half.Validate("development")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "CHIPPER_DEPOSIT_MEMO")
	assert.Contains(t, err.Error(), "CHIPPER_FEE_SCHEDULE")

	t.Setenv("CHIPPER_BASE_URL", "")
	err = full.Validate("production")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "CHIPPER_BASE_URL")
}

func TestEnabledProviders(t *testing.T) {
	p := PaymentsConfig{
		YellowCard: YellowCardConfig{PublicKey: "pk", SecretKey: "sk"},
		Fonbnk:     FonbnkConfig{ClientID: "id"},
		Chipper:    ChipperConfig{APIKey: "sk_test_x"},
		MoneyGram:  MoneyGramConfig{HomeDomain: "extmgxanchor.moneygram.com"},
	}
	assert.Equal(t, []string{ProviderMoneyGram, ProviderYellowCard, ProviderChipper}, p.EnabledProviders())
	assert.True(t, p.ProviderEnabled(ProviderChipper))
	assert.False(t, p.ProviderEnabled(ProviderFonbnk), "fonbnk needs both credentials")
}

func TestParseFloatAlert(t *testing.T) {
	off, err := parseFloatAlert(" ")
	require.NoError(t, err)
	assert.Nil(t, off)

	got, err := parseFloatAlert("150.5")
	require.NoError(t, err)
	assert.Equal(t, "150.5", got.String())

	for _, bad := range []string{"-5", "lots"} {
		_, err := parseFloatAlert(bad)
		assert.Error(t, err, bad)
	}
}
