package rpc

import (
	"context"
	"errors"
	"testing"
	"time"

	protocol "github.com/stellar/go-stellar-sdk/protocols/rpc"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type stubGetter struct {
	resp protocol.GetTransactionResponse
	err  error
}

func (s stubGetter) GetTransaction(_ context.Context, _ protocol.GetTransactionRequest) (protocol.GetTransactionResponse, error) {
	return s.resp, s.err
}

func TestVerifier_TransactionSucceeded(t *testing.T) {
	tests := []struct {
		name    string
		getter  stubGetter
		hash    string
		want    bool
		wantErr bool
	}{
		{
			name:   "success is confirmed",
			getter: stubGetter{resp: protocol.GetTransactionResponse{TransactionDetails: protocol.TransactionDetails{Status: protocol.TransactionStatusSuccess}}},
			hash:   "abc",
			want:   true,
		},
		{
			// Definitive: the caller may safely treat this as "did not happen".
			name:   "failed is a definitive no",
			getter: stubGetter{resp: protocol.GetTransactionResponse{TransactionDetails: protocol.TransactionDetails{Status: protocol.TransactionStatusFailed}}},
			hash:   "abc",
			want:   false,
		},
		{
			// Not found means unknown, never "failed" — the transaction may be
			// outside the RPC retention window.
			name:    "not found is unknown, not failure",
			getter:  stubGetter{resp: protocol.GetTransactionResponse{TransactionDetails: protocol.TransactionDetails{Status: protocol.TransactionStatusNotFound}}},
			hash:    "abc",
			wantErr: true,
		},
		{
			name:    "rpc error propagates",
			getter:  stubGetter{err: errors.New("connection refused")},
			hash:    "abc",
			wantErr: true,
		},
		{
			name:    "empty hash is rejected",
			getter:  stubGetter{},
			hash:    "",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := NewVerifier(tt.getter).TransactionSucceeded(context.Background(), tt.hash)
			if tt.wantErr {
				require.Error(t, err)
				assert.False(t, got, "an unknown outcome must never report success")
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestVerifier_ResolveSubmitted(t *testing.T) {
	submitted := time.Unix(1_000_000, 0)
	validUntil := submitted.Add(5 * time.Minute)
	notFound := func(oldest, latest time.Time) protocol.GetTransactionResponse {
		return protocol.GetTransactionResponse{
			OldestLedgerCloseTime: oldest.Unix(),
			LatestLedgerCloseTime: latest.Unix(),
			TransactionDetails:    protocol.TransactionDetails{Status: protocol.TransactionStatusNotFound},
		}
	}

	tests := []struct {
		name    string
		getter  stubGetter
		want    TxOutcome
		wantErr bool
	}{
		{
			name:   "success carries the ledger",
			getter: stubGetter{resp: protocol.GetTransactionResponse{TransactionDetails: protocol.TransactionDetails{Status: protocol.TransactionStatusSuccess, Ledger: 42}}},
			want:   TxSucceeded,
		},
		{
			name:   "failed on ledger",
			getter: stubGetter{resp: protocol.GetTransactionResponse{TransactionDetails: protocol.TransactionDetails{Status: protocol.TransactionStatusFailed}}},
			want:   TxFailed,
		},
		{
			name:   "not found before the ledger passes validUntil is unresolved",
			getter: stubGetter{resp: notFound(submitted.Add(-time.Hour), validUntil)},
			want:   TxUnresolved,
		},
		{
			name:   "not found once history covers the whole window never landed",
			getter: stubGetter{resp: notFound(submitted.Add(-time.Hour), validUntil.Add(time.Second))},
			want:   TxNeverLanded,
		},
		{
			name:   "not found with history starting after submission is outside retention",
			getter: stubGetter{resp: notFound(submitted, validUntil.Add(time.Hour))},
			want:   TxOutsideRetention,
		},
		{
			name:   "a freshly restarted RPC before expiry stays unresolved",
			getter: stubGetter{resp: notFound(submitted.Add(time.Minute), validUntil.Add(-time.Second))},
			want:   TxUnresolved,
		},
		{
			name:    "transport error is an error, not an outcome",
			getter:  stubGetter{err: errors.New("rpc down")},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := NewVerifier(tt.getter).ResolveSubmitted(t.Context(), "abc", submitted, validUntil)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got.Outcome)
		})
	}
}
