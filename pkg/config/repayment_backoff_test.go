package config

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setRequiredEnv(t *testing.T) {
	t.Helper()
	// These are parsed unconditionally rather than defaulted, so New() fails on
	// an empty value even though requiredEnv does not list them.
	unlisted := []string{
		"AT_SANDBOX_MODE", "ENABLE_MULTI_SIG",
		"MULTI_SIG_LOW_THRESHOLD", "MULTI_SIG_MEDIUM_THRESHOLD", "MULTI_SIG_HIGH_THRESHOLD",
	}

	for _, key := range append(requiredEnv, unlisted...) {
		switch key {
		case "USSD_DIAL_STRING":
			t.Setenv(key, "*384*52203#")
		case "AT_SANDBOX_MODE", "ENABLE_MULTI_SIG":
			t.Setenv(key, "true")
		case "MULTI_SIG_LOW_THRESHOLD", "MULTI_SIG_MEDIUM_THRESHOLD", "MULTI_SIG_HIGH_THRESHOLD":
			t.Setenv(key, "1")
		case "TREASURY_SECRET_KEY":
			t.Setenv(key, "SAN5L2CY3B57KZHWQ27KSO4DBRN7OLF4WON4REWDZYKR7A37OR4FQSTH")
		case "ADMIN_SECRET_KEY":
			t.Setenv(key, "SDAWGPZAXFD3ZZGIED2LMZISDFRV2ENNZOA57PV6BAMSTGN4OOA6T2XL")
		case "SERVER_SECRET_KEY":
			t.Setenv(key, "SDDQRIBKZB4WZ2D6WA4US22YSHEUNMQQYMDYSTOHXCZXYZIK7XCHAW7S")
		case "USDC_ISSUER":
			t.Setenv(key, "GA5ZSEJYB37JRC5AVCIA5MOP4RHTM335X2KGX3IHOJAPP5RE34K4KZVN")
		case "CONTRACT_ID":
			t.Setenv(key, "CAAQEAYEAUDAOCAJBIFQYDIOB4IBCEQTCQKRMFYYDENBWHA5DYPSBFLM")
		default:
			t.Setenv(key, "placeholder")
		}
	}
}

// The per-row backoffs write repayment_next_poll_at, which is what decides
// whether GetDueRepayments returns a row at all. REPAYMENT_POLL_INTERVAL only
// changes how often the runner asks — a row parked 30 minutes out stays parked
// however fast the ticker runs, which is why development could not watch a
// deposit move.
func TestRepaymentBackoffsAreOverridable(t *testing.T) {
	setRequiredEnv(t)
	t.Setenv("REPAYMENT_ACTIVE_BACKOFF", "5")
	t.Setenv("REPAYMENT_IDLE_BACKOFF", "10")
	t.Setenv("REPAYMENT_VAULT_RETRY_BACKOFF", "15")

	cfg, err := New()
	require.NoError(t, err)

	mg := cfg.Payments.MoneyGram
	assert.Equal(t, 5*time.Second, mg.RepaymentActiveBackoff)
	assert.Equal(t, 10*time.Second, mg.RepaymentIdleBackoff)
	assert.Equal(t, 15*time.Second, mg.RepaymentVaultRetryBackoff)
}

// Unset means zero, and the overlay in cmd/credit leaves the poller's own
// defaults in place rather than config restating them.
func TestRepaymentBackoffsDefaultToZero(t *testing.T) {
	setRequiredEnv(t)

	cfg, err := New()
	require.NoError(t, err)

	mg := cfg.Payments.MoneyGram
	assert.Zero(t, mg.RepaymentActiveBackoff)
	assert.Zero(t, mg.RepaymentIdleBackoff)
	assert.Zero(t, mg.RepaymentVaultRetryBackoff)
}
