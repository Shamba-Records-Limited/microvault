package rpc

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	protocol "github.com/stellar/go-stellar-sdk/protocols/rpc"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	stellartesting "github.com/Shamba-Records-Limited/microvault/pkg/stellar/testing"
	"github.com/Shamba-Records-Limited/microvault/pkg/stellar/types"
)

// Verify MockRPCClient satisfies the TransactionGetter interface.
var _ TransactionGetter = (*stellartesting.MockRPCClient)(nil)

// testPollConfig returns a config with a tiny interval and a discard logger so
// retry branches don't add real wall-clock delay or noisy output.
func testPollConfig(maxAttempts int) PollConfig {
	return PollConfig{
		MaxAttempts:  maxAttempts,
		PollInterval: time.Millisecond,
		Logger:       slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
}

func getTxResponse(status string, ledger uint32) protocol.GetTransactionResponse {
	resp := protocol.GetTransactionResponse{}
	resp.Status = status
	resp.Ledger = ledger
	return resp
}

func TestPollTransaction(t *testing.T) {
	tests := []struct {
		name       string
		setupMock  func(*stellartesting.MockRPCClient)
		wantErr    error
		wantStatus string
	}{
		{
			name: "success on first attempt",
			setupMock: func(m *stellartesting.MockRPCClient) {
				m.GetTransactionFunc = func(ctx context.Context, req protocol.GetTransactionRequest) (protocol.GetTransactionResponse, error) {
					return getTxResponse(protocol.TransactionStatusSuccess, 12345), nil
				}
			},
			wantErr:    nil,
			wantStatus: protocol.TransactionStatusSuccess,
		},
		{
			name: "failed on ledger",
			setupMock: func(m *stellartesting.MockRPCClient) {
				m.GetTransactionFunc = func(ctx context.Context, req protocol.GetTransactionRequest) (protocol.GetTransactionResponse, error) {
					return getTxResponse(protocol.TransactionStatusFailed, 12345), nil
				}
			},
			wantErr: types.ErrTransactionFailedOnLedger,
		},
		{
			name: "unknown status",
			setupMock: func(m *stellartesting.MockRPCClient) {
				m.GetTransactionFunc = func(ctx context.Context, req protocol.GetTransactionRequest) (protocol.GetTransactionResponse, error) {
					return getTxResponse("WEIRD_STATUS", 0), nil
				}
			},
			wantErr: types.ErrUnknownTransactionStatus,
		},
		{
			name: "times out when never found",
			setupMock: func(m *stellartesting.MockRPCClient) {
				m.GetTransactionFunc = func(ctx context.Context, req protocol.GetTransactionRequest) (protocol.GetTransactionResponse, error) {
					return getTxResponse(protocol.TransactionStatusNotFound, 0), nil
				}
			},
			wantErr: types.ErrTransactionTimeout,
		},
		{
			name: "retries past not-found then succeeds",
			setupMock: func(m *stellartesting.MockRPCClient) {
				attempt := 0
				m.GetTransactionFunc = func(ctx context.Context, req protocol.GetTransactionRequest) (protocol.GetTransactionResponse, error) {
					attempt++
					if attempt < 3 {
						return getTxResponse(protocol.TransactionStatusNotFound, 0), nil
					}
					return getTxResponse(protocol.TransactionStatusSuccess, 12345), nil
				}
			},
			wantErr:    nil,
			wantStatus: protocol.TransactionStatusSuccess,
		},
		{
			name: "retries past transient fetch error then succeeds",
			setupMock: func(m *stellartesting.MockRPCClient) {
				attempt := 0
				m.GetTransactionFunc = func(ctx context.Context, req protocol.GetTransactionRequest) (protocol.GetTransactionResponse, error) {
					attempt++
					if attempt < 2 {
						return protocol.GetTransactionResponse{}, errors.New("temporary RPC failure")
					}
					return getTxResponse(protocol.TransactionStatusSuccess, 12345), nil
				}
			},
			wantErr:    nil,
			wantStatus: protocol.TransactionStatusSuccess,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockClient := stellartesting.NewMockRPCClient()
			tt.setupMock(mockClient)

			resp, err := PollTransaction(context.Background(), mockClient, "test_tx_hash", testPollConfig(5))

			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.wantStatus, resp.Status)
		})
	}
}

func TestPollTransaction_ContextCancelled(t *testing.T) {
	mockClient := stellartesting.NewMockRPCClient()
	mockClient.GetTransactionFunc = func(ctx context.Context, req protocol.GetTransactionRequest) (protocol.GetTransactionResponse, error) {
		return getTxResponse(protocol.TransactionStatusNotFound, 0), nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := PollTransaction(ctx, mockClient, "test_tx_hash", testPollConfig(5))

	require.ErrorIs(t, err, types.ErrContextCancelled)
	assert.Empty(t, mockClient.GetTransactionCalls, "should bail before any RPC call when context is already cancelled")
}
