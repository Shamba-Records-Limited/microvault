//go:build stellar_live

package rpc

import (
	"context"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/stellar/go-stellar-sdk/clients/rpcclient"
	"github.com/stretchr/testify/require"
)

// Run by hand against a public RPC node:
//
//	STELLAR_LIVE_DESTINATION=G… STELLAR_LIVE_MEMO=… go test -tags stellar_live -run Live -v ./pkg/stellar/rpc/
//
// It finds a USDC payment into the destination carrying the memo within the
// last STELLAR_LIVE_DAYS (default 6) on testnet.
func TestLivePaymentFinder(t *testing.T) {
	dest, memo := os.Getenv("STELLAR_LIVE_DESTINATION"), os.Getenv("STELLAR_LIVE_MEMO")
	if dest == "" || memo == "" {
		t.Skip("set STELLAR_LIVE_DESTINATION and STELLAR_LIVE_MEMO")
	}
	url := os.Getenv("STELLAR_LIVE_RPC_URL")
	if url == "" {
		url = "https://soroban-testnet.stellar.org"
	}
	issuer := os.Getenv("STELLAR_LIVE_USDC_ISSUER")
	if issuer == "" {
		issuer = "GBBD47IF6LWK7P7MDEVSCWR7DPUWV3NY3DTQEVFL4NAT4AQH3ZLLFLA5"
	}
	days := 6 * 24 * time.Hour
	if v := os.Getenv("STELLAR_LIVE_DAYS"); v != "" {
		d, err := time.ParseDuration(v + "h")
		require.NoError(t, err)
		days = d * 24
	}

	client := rpcclient.NewClient(url, &http.Client{Timeout: 30 * time.Second})
	f := NewPaymentFinder(client, "Test SDF Network ; September 2015")
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	found, ok, err := f.Find(ctx, PaymentSearch{Destination: dest, AssetCode: "USDC", AssetIssuer: issuer, Memo: memo, Since: time.Now().Add(-days)})
	require.NoError(t, err)
	t.Logf("found=%v tx=%s ledger=%d stroops=%d", ok, found.TxHash, found.Ledger, found.AmountStroops)
}
