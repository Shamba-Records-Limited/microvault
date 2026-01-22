package soroban

import (
	"context"
	"testing"

	protocol "github.com/stellar/go-stellar-sdk/protocols/rpc"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	stellartesting "github.com/Shamba-Records-Limited/microvault/internal/core/services/stellar/testing"
	"github.com/Shamba-Records-Limited/microvault/internal/core/services/stellar/types"
)

// ============================================================================
// Treasury Operation Tests
// ============================================================================

func TestBorrowFromVault(t *testing.T) {
	keys := stellartesting.NewTestKeys()

	tests := []struct {
		name        string
		req         types.BorrowRequest
		setupMock   func(*stellartesting.MockRPCClient)
		wantErr     bool
		errContains string
	}{
		{
			name: "successful borrow",
			req: types.BorrowRequest{
				RecipientAddress: keys.UserPublic,
				Amount:           1000000000, // 100 USDC
			},
			setupMock: func(m *stellartesting.MockRPCClient) {
				m.SimulateTransactionFunc = func(ctx context.Context, req protocol.SimulateTransactionRequest) (protocol.SimulateTransactionResponse, error) {
					return stellartesting.NewSimulationResponse().
						WithI128Result(1000000000).
						WithTransactionData().
						WithAuth().
						WithMinResourceFee(10000).
						Build(), nil
				}
				m.SendTransactionFunc = func(ctx context.Context, req protocol.SendTransactionRequest) (protocol.SendTransactionResponse, error) {
					return stellartesting.NewSendTransactionResponse().
						WithHash("borrow_tx_hash_123").
						Build(), nil
				}
				m.GetTransactionFunc = func(ctx context.Context, req protocol.GetTransactionRequest) (protocol.GetTransactionResponse, error) {
					return stellartesting.NewGetTransactionResponse().
						WithStatus(protocol.TransactionStatusSuccess).
						Build(), nil
				}
			},
			wantErr: false,
		},
		{
			name: "invalid amount - zero",
			req: types.BorrowRequest{
				RecipientAddress: keys.UserPublic,
				Amount:           0,
			},
			setupMock:   func(m *stellartesting.MockRPCClient) {},
			wantErr:     true,
			errContains: "invalid transaction amount",
		},
		{
			name: "invalid amount - negative",
			req: types.BorrowRequest{
				RecipientAddress: keys.UserPublic,
				Amount:           -100,
			},
			setupMock:   func(m *stellartesting.MockRPCClient) {},
			wantErr:     true,
			errContains: "invalid transaction amount",
		},
		{
			name: "invalid recipient address",
			req: types.BorrowRequest{
				RecipientAddress: "invalid_address",
				Amount:           1000000000,
			},
			setupMock:   func(m *stellartesting.MockRPCClient) {},
			wantErr:     true,
			errContains: "invalid recipient address",
		},
		{
			name: "simulation error - vault paused",
			req: types.BorrowRequest{
				RecipientAddress: keys.UserPublic,
				Amount:           1000000000,
			},
			setupMock: func(m *stellartesting.MockRPCClient) {
				m.SimulateTransactionFunc = func(ctx context.Context, req protocol.SimulateTransactionRequest) (protocol.SimulateTransactionResponse, error) {
					return stellartesting.NewSimulationResponse().
						WithError("Error(Contract, #1000)"). // EnforcedPause
						Build(), nil
				}
			},
			wantErr:     true,
			errContains: "simulation error",
		},
		{
			name: "simulation error - insufficient liquidity",
			req: types.BorrowRequest{
				RecipientAddress: keys.UserPublic,
				Amount:           1000000000000, // Very large amount
			},
			setupMock: func(m *stellartesting.MockRPCClient) {
				m.SimulateTransactionFunc = func(ctx context.Context, req protocol.SimulateTransactionRequest) (protocol.SimulateTransactionResponse, error) {
					return stellartesting.NewSimulationResponse().
						WithError("Error(Contract, #10)"). // InsufficientLiquidity
						Build(), nil
				}
			},
			wantErr:     true,
			errContains: "simulation error",
		},
		{
			name: "transaction failed",
			req: types.BorrowRequest{
				RecipientAddress: keys.UserPublic,
				Amount:           1000000000,
			},
			setupMock: func(m *stellartesting.MockRPCClient) {
				m.SimulateTransactionFunc = func(ctx context.Context, req protocol.SimulateTransactionRequest) (protocol.SimulateTransactionResponse, error) {
					return stellartesting.NewSimulationResponse().
						WithI128Result(1000000000).
						WithTransactionData().
						WithAuth().
						Build(), nil
				}
				m.SendTransactionFunc = func(ctx context.Context, req protocol.SendTransactionRequest) (protocol.SendTransactionResponse, error) {
					return stellartesting.NewSendTransactionResponse().
						WithHash("failed_tx_hash").
						Build(), nil
				}
				m.GetTransactionFunc = func(ctx context.Context, req protocol.GetTransactionRequest) (protocol.GetTransactionResponse, error) {
					return stellartesting.NewGetTransactionResponse().
						WithStatus(protocol.TransactionStatusFailed).
						Build(), nil
				}
			},
			wantErr:     true,
			errContains: "transaction failed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockClient := stellartesting.NewMockRPCClient()
			tt.setupMock(mockClient)

			svc := newTestService(mockClient)
			resp, err := svc.BorrowFromVault(context.Background(), tt.req)

			if tt.wantErr {
				require.Error(t, err)
				if tt.errContains != "" {
					assert.Contains(t, err.Error(), tt.errContains)
				}
				return
			}

			require.NoError(t, err)
			require.NotNil(t, resp)
			assert.NotEmpty(t, resp.TxHash)
			assert.Equal(t, tt.req.Amount, resp.AmountBorrowed)
			assert.Equal(t, tt.req.RecipientAddress, resp.RecipientAddress)
		})
	}
}

func TestRepayToVault(t *testing.T) {
	tests := []struct {
		name        string
		req         types.RepayRequest
		setupMock   func(*stellartesting.MockRPCClient)
		wantErr     bool
		errContains string
	}{
		{
			name: "successful repay",
			req: types.RepayRequest{
				Amount: 500000000, // 50 USDC
			},
			setupMock: func(m *stellartesting.MockRPCClient) {
				m.SimulateTransactionFunc = func(ctx context.Context, req protocol.SimulateTransactionRequest) (protocol.SimulateTransactionResponse, error) {
					return stellartesting.NewSimulationResponse().
						WithI128Result(500000000).
						WithTransactionData().
						WithAuth().
						Build(), nil
				}
				m.SendTransactionFunc = func(ctx context.Context, req protocol.SendTransactionRequest) (protocol.SendTransactionResponse, error) {
					return stellartesting.NewSendTransactionResponse().
						WithHash("repay_tx_hash_456").
						Build(), nil
				}
				m.GetTransactionFunc = func(ctx context.Context, req protocol.GetTransactionRequest) (protocol.GetTransactionResponse, error) {
					return stellartesting.NewGetTransactionResponse().
						WithStatus(protocol.TransactionStatusSuccess).
						Build(), nil
				}
			},
			wantErr: false,
		},
		{
			name: "invalid amount - zero",
			req: types.RepayRequest{
				Amount: 0,
			},
			setupMock:   func(m *stellartesting.MockRPCClient) {},
			wantErr:     true,
			errContains: "invalid transaction amount",
		},
		{
			name: "invalid amount - negative",
			req: types.RepayRequest{
				Amount: -100,
			},
			setupMock:   func(m *stellartesting.MockRPCClient) {},
			wantErr:     true,
			errContains: "invalid transaction amount",
		},
		{
			name: "simulation error - repay exceeds debt",
			req: types.RepayRequest{
				Amount: 1000000000000, // More than borrowed
			},
			setupMock: func(m *stellartesting.MockRPCClient) {
				m.SimulateTransactionFunc = func(ctx context.Context, req protocol.SimulateTransactionRequest) (protocol.SimulateTransactionResponse, error) {
					return stellartesting.NewSimulationResponse().
						WithError("Error(Contract, #11)"). // RepayExceedsDebt
						Build(), nil
				}
			},
			wantErr:     true,
			errContains: "simulation error",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockClient := stellartesting.NewMockRPCClient()
			tt.setupMock(mockClient)

			svc := newTestService(mockClient)
			resp, err := svc.RepayToVault(context.Background(), tt.req)

			if tt.wantErr {
				require.Error(t, err)
				if tt.errContains != "" {
					assert.Contains(t, err.Error(), tt.errContains)
				}
				return
			}

			require.NoError(t, err)
			require.NotNil(t, resp)
			assert.NotEmpty(t, resp.TxHash)
			assert.Equal(t, tt.req.Amount, resp.AmountRepaid)
		})
	}
}

func TestAccrueInterest(t *testing.T) {
	tests := []struct {
		name        string
		setupMock   func(*stellartesting.MockRPCClient)
		wantErr     bool
		errContains string
	}{
		{
			name: "successful accrue",
			setupMock: func(m *stellartesting.MockRPCClient) {
				m.SimulateTransactionFunc = func(ctx context.Context, req protocol.SimulateTransactionRequest) (protocol.SimulateTransactionResponse, error) {
					return stellartesting.NewSimulationResponse().
						WithTransactionData().
						Build(), nil
				}
				m.SendTransactionFunc = func(ctx context.Context, req protocol.SendTransactionRequest) (protocol.SendTransactionResponse, error) {
					return stellartesting.NewSendTransactionResponse().
						WithHash("accrue_tx_hash").
						Build(), nil
				}
				m.GetTransactionFunc = func(ctx context.Context, req protocol.GetTransactionRequest) (protocol.GetTransactionResponse, error) {
					return stellartesting.NewGetTransactionResponse().
						WithStatus(protocol.TransactionStatusSuccess).
						Build(), nil
				}
			},
			wantErr: false,
		},
		{
			name: "simulation error",
			setupMock: func(m *stellartesting.MockRPCClient) {
				m.SimulateTransactionFunc = func(ctx context.Context, req protocol.SimulateTransactionRequest) (protocol.SimulateTransactionResponse, error) {
					return stellartesting.NewSimulationResponse().
						WithError("contract error").
						Build(), nil
				}
			},
			wantErr:     true,
			errContains: "simulation error",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockClient := stellartesting.NewMockRPCClient()
			tt.setupMock(mockClient)

			svc := newTestService(mockClient)
			err := svc.AccrueInterest(context.Background())

			if tt.wantErr {
				require.Error(t, err)
				if tt.errContains != "" {
					assert.Contains(t, err.Error(), tt.errContains)
				}
				return
			}

			require.NoError(t, err)
		})
	}
}
