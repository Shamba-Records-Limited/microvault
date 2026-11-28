//go:build chipper_sandbox

package chipper

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/govalues/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Checks against the real sandbox, run by hand:
//
//	CHIPPER_SANDBOX_KEY=sk_test_… go test -tags chipper_sandbox -run Sandbox ./pkg/payment/chipper/...
//
// All are read-only except TestSandboxRepaymentOrder, which runs only when
// CHIPPER_SANDBOX_ORDER_DESTINATION is set and pays real testnet USDC to it.
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

// TestSandboxRepaymentOrder runs a repayment-shaped order: a simulated
// M-Pesa collection paid out as USDC on Stellar testnet. It logs whether the
// payout reports a Stellar hash.
func TestSandboxRepaymentOrder(t *testing.T) {
	c, _ := sandboxClient(t)
	dest := os.Getenv("CHIPPER_SANDBOX_ORDER_DESTINATION")
	if dest == "" {
		t.Skip("set CHIPPER_SANDBOX_ORDER_DESTINATION (a testnet G… address with a USDC trustline) to run an order")
	}
	phone := os.Getenv("CHIPPER_SANDBOX_ORDER_PHONE")
	if phone == "" {
		phone = "254700000123"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Minute)
	t.Cleanup(cancel)

	order, replayed, err := c.CreateOrder(ctx, OrderRequest{
		From:              OrderLeg{Code: "ke_mpesa", AccountNumber: phone, Currency: "KES"},
		To:                OrderLeg{Code: MethodUSDCStellar, AccountNumber: dest, Tag: os.Getenv("CHIPPER_SANDBOX_ORDER_MEMO"), Currency: "USDC", Amount: NewAmount(decimal.MustParse("1"))},
		ExternalReference: fmt.Sprintf("sandbox-rp-%d", time.Now().UnixNano()),
	})
	require.NoError(t, err)
	require.False(t, replayed)
	t.Logf("order %s status=%s expected=%v fee=%+v rate=%v", order.ID, order.Status, order.ExpectedAmount, order.Fee, order.Rate)
	if order.Instructions != nil {
		t.Logf("instructions %s %v %s: %s", order.Instructions.Type, order.Instructions.Amount, order.Instructions.Currency, order.Instructions.Message)
	}

	last := order.Status
	for !order.IsTerminal() {
		select {
		case <-ctx.Done():
			t.Fatalf("order %s still %s", order.ID, order.Status)
		case <-time.After(5 * time.Second):
		}
		order, err = c.GetOrder(ctx, order.ID)
		require.NoError(t, err)
		if order.Status != last {
			t.Logf("-> %s", order.Status)
			last = order.Status
		}
	}
	t.Logf("final %s received=%v collection=%v payout=%v message=%v", order.Status, order.ReceivedAmount, deref(order.CollectionID), deref(order.PayoutID), deref(order.StatusMessage))
	require.Equal(t, OrderCompleted, order.Status)

	if order.CollectionID != nil {
		col, err := c.GetCollection(ctx, *order.CollectionID)
		require.NoError(t, err)
		t.Logf("collection %s status=%s ref=%v", col.ID, col.Status, deref(col.ExternalReference))
	}
	require.NotNil(t, order.PayoutID, "a completed order names its payout")
	payout, err := c.GetPayout(ctx, *order.PayoutID)
	require.NoError(t, err)
	t.Logf("payout %s status=%s details=%s", payout.ID, payout.Status, payout.Details)
	if hash, ok := payout.TxHash(); ok {
		t.Logf("STELLAR HASH REPORTED: %s", hash)
	} else {
		t.Log("NO STELLAR HASH on the payout: settlement will rely on Chipper's reported amount")
	}
}

func deref(s *string) string {
	if s == nil {
		return "<nil>"
	}
	return *s
}
