//go:build chipper_sandbox

package chipper

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Read-only checks against the real sandbox, run by hand:
//
//	CHIPPER_SANDBOX_KEY=sk_test_… go test -tags chipper_sandbox -run Sandbox ./pkg/payment/chipper/...
func sandboxClient(t *testing.T) (*Client, context.Context) {
	t.Helper()
	key := os.Getenv("CHIPPER_SANDBOX_KEY")
	if key == "" {
		t.Skip("set CHIPPER_SANDBOX_KEY to run the sandbox suite")
	}
	c, err := New(Config{APIKey: key})
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)
	return c, ctx
}

func TestSandboxOrganization(t *testing.T) {
	c, ctx := sandboxClient(t)
	org, err := c.GetOrganization(ctx)
	require.NoError(t, err)
	assert.NotEmpty(t, org.ID)
}

func TestSandboxRate(t *testing.T) {
	c, ctx := sandboxClient(t)
	rate, err := c.GetRate(ctx, "USDC", "KES")
	require.NoError(t, err)
	t.Logf("USDC/KES = %s", rate.Rate)
}

func TestSandboxCapabilities(t *testing.T) {
	c, ctx := sandboxClient(t)
	caps, err := c.GetCapabilities(ctx)
	require.NoError(t, err)

	m, ok := caps.PayoutMethod("KE", "ke_mpesa")
	require.True(t, ok, "ke_mpesa is not offered")
	t.Logf("ke_mpesa status=%s limits=%+v", m.Status, m.Limits)

	ch, ok := caps.DepositChain("USDC", ChainStellar)
	require.True(t, ok, "USDC on Stellar is not accepted for deposit")
	assert.True(t, ch.RequiresMemo)
}

func TestSandboxAccounts(t *testing.T) {
	c, ctx := sandboxClient(t)
	accounts, err := c.ListAccounts(ctx)
	require.NoError(t, err)
	for _, a := range accounts {
		t.Logf("%s available=%s pending=%s", a.Currency, a.AvailableBalance, a.PendingBalance)
	}
}

func TestSandboxStellarDepositAddress(t *testing.T) {
	c, ctx := sandboxClient(t)
	addr, err := c.CreateCryptoAddress(ctx, CryptoAddressRequest{Currency: "USDC", Chain: ChainStellar})
	require.NoError(t, err)
	require.NotNil(t, addr.Tag, "a Stellar deposit address must carry a memo")
	t.Logf("deposit %s memo %s", *addr.Address, *addr.Tag)
}

func TestSandboxFeeLedger(t *testing.T) {
	c, ctx := sandboxClient(t)
	page, err := c.ListLedger(ctx, LedgerFilter{Type: LedgerFeeDebit, Limit: 10})
	require.NoError(t, err)
	for _, e := range page.Data {
		t.Logf("%s %s %s ref=%v", e.CreatedAt.Format(time.RFC3339), e.Amount, e.Currency, e.ReferenceID)
	}
}
