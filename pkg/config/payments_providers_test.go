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
	_, set, err := parseFloatAlert(" ")
	require.NoError(t, err)
	assert.False(t, set)

	got, set, err := parseFloatAlert("150.5")
	require.NoError(t, err)
	assert.True(t, set)
	assert.Equal(t, "150.5", got.String())

	for _, bad := range []string{"-5", "lots"} {
		_, _, err := parseFloatAlert(bad)
		assert.Error(t, err, bad)
	}
}

func TestParseRepaymentRail(t *testing.T) {
	cases := []struct{ raw, want string }{
		{"", RepaymentRailDaraja},
		{"daraja", RepaymentRailDaraja},
		{"  Chipper\t", RepaymentRailChipper},
	}
	for _, tc := range cases {
		got, err := parseRepaymentRail(tc.raw)
		if err != nil || got != tc.want {
			t.Errorf("parseRepaymentRail(%q) = %q, %v; want %q", tc.raw, got, err, tc.want)
		}
	}
	if _, err := parseRepaymentRail("fonbnk"); err == nil {
		t.Error("an unknown rail must be refused")
	}
}

func TestValidateRepaymentRails(t *testing.T) {
	if err := (PaymentsConfig{RepaymentRailMpesa: RepaymentRailChipper}).ValidateRepaymentRails(); err == nil {
		t.Error("chipper rail without a chipper key must be refused")
	}
	ok := PaymentsConfig{RepaymentRailMpesa: RepaymentRailChipper, Chipper: ChipperConfig{APIKey: "sk_test_x"}}
	if err := ok.ValidateRepaymentRails(); err != nil {
		t.Errorf("configured chipper rail refused: %v", err)
	}
	if err := (PaymentsConfig{RepaymentRailMpesa: RepaymentRailDaraja}).ValidateRepaymentRails(); err != nil {
		t.Errorf("daraja rail refused: %v", err)
	}
}
