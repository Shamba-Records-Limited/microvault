package rpc

import (
	"context"
	"errors"
	"testing"

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
