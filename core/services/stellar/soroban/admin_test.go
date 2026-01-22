package soroban

import (
	"context"
	"testing"

	protocol "github.com/stellar/go-stellar-sdk/protocols/rpc"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	stellartesting "github.com/Shamba-Records-Limited/Microvault/internal/core/services/stellar/testing"
)

// ============================================================================
// Admin Operation Tests
// ============================================================================

func TestPauseVault(t *testing.T) {
	tests := []struct {
		name        string
		setupMock   func(*stellartesting.MockRPCClient)
		wantErr     bool
		errContains string
	}{
		{
			name: "successful pause",
			setupMock: func(m *stellartesting.MockRPCClient) {
				m.SimulateTransactionFunc = func(ctx context.Context, req protocol.SimulateTransactionRequest) (protocol.SimulateTransactionResponse, error) {
					return stellartesting.NewSimulationResponse().
						WithTransactionData().
						WithAuth().
						Build(), nil
				}
				m.SendTransactionFunc = func(ctx context.Context, req protocol.SendTransactionRequest) (protocol.SendTransactionResponse, error) {
					return stellartesting.NewSendTransactionResponse().
						WithHash("pause_tx_hash").
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
			name: "already paused",
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
			name: "unauthorized caller",
			setupMock: func(m *stellartesting.MockRPCClient) {
				m.SimulateTransactionFunc = func(ctx context.Context, req protocol.SimulateTransactionRequest) (protocol.SimulateTransactionResponse, error) {
					return stellartesting.NewSimulationResponse().
						WithError("Error(Contract, #1)"). // Unauthorized
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
			err := svc.PauseVault(context.Background())

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

func TestUnpauseVault(t *testing.T) {
	tests := []struct {
		name        string
		setupMock   func(*stellartesting.MockRPCClient)
		wantErr     bool
		errContains string
	}{
		{
			name: "successful unpause",
			setupMock: func(m *stellartesting.MockRPCClient) {
				m.SimulateTransactionFunc = func(ctx context.Context, req protocol.SimulateTransactionRequest) (protocol.SimulateTransactionResponse, error) {
					return stellartesting.NewSimulationResponse().
						WithTransactionData().
						WithAuth().
						Build(), nil
				}
				m.SendTransactionFunc = func(ctx context.Context, req protocol.SendTransactionRequest) (protocol.SendTransactionResponse, error) {
					return stellartesting.NewSendTransactionResponse().
						WithHash("unpause_tx_hash").
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
			name: "not paused",
			setupMock: func(m *stellartesting.MockRPCClient) {
				m.SimulateTransactionFunc = func(ctx context.Context, req protocol.SimulateTransactionRequest) (protocol.SimulateTransactionResponse, error) {
					return stellartesting.NewSimulationResponse().
						WithError("Error(Contract, #1001)"). // ExpectedPause
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
			err := svc.UnpauseVault(context.Background())

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

func TestSetMaxDeposit(t *testing.T) {
	tests := []struct {
		name        string
		limit       int64
		setupMock   func(*stellartesting.MockRPCClient)
		wantErr     bool
		errContains string
	}{
		{
			name:  "successful set max deposit",
			limit: 10000000000, // 1000 USDC
			setupMock: func(m *stellartesting.MockRPCClient) {
				m.SimulateTransactionFunc = func(ctx context.Context, req protocol.SimulateTransactionRequest) (protocol.SimulateTransactionResponse, error) {
					return stellartesting.NewSimulationResponse().
						WithTransactionData().
						WithAuth().
						Build(), nil
				}
				m.SendTransactionFunc = func(ctx context.Context, req protocol.SendTransactionRequest) (protocol.SendTransactionResponse, error) {
					return stellartesting.NewSendTransactionResponse().
						WithHash("set_max_deposit_tx").
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
			name:  "set to zero (no limit)",
			limit: 0,
			setupMock: func(m *stellartesting.MockRPCClient) {
				m.SimulateTransactionFunc = func(ctx context.Context, req protocol.SimulateTransactionRequest) (protocol.SimulateTransactionResponse, error) {
					return stellartesting.NewSimulationResponse().
						WithTransactionData().
						WithAuth().
						Build(), nil
				}
				m.SendTransactionFunc = func(ctx context.Context, req protocol.SendTransactionRequest) (protocol.SendTransactionResponse, error) {
					return stellartesting.NewSendTransactionResponse().Build(), nil
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
			name:  "unauthorized",
			limit: 10000000000,
			setupMock: func(m *stellartesting.MockRPCClient) {
				m.SimulateTransactionFunc = func(ctx context.Context, req protocol.SimulateTransactionRequest) (protocol.SimulateTransactionResponse, error) {
					return stellartesting.NewSimulationResponse().
						WithError("Error(Contract, #1)"). // Unauthorized
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
			err := svc.SetMaxDeposit(context.Background(), tt.limit)

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

func TestSetMaxWithdraw(t *testing.T) {
	tests := []struct {
		name        string
		limit       int64
		setupMock   func(*stellartesting.MockRPCClient)
		wantErr     bool
		errContains string
	}{
		{
			name:  "successful set max withdraw",
			limit: 5000000000, // 500 USDC
			setupMock: func(m *stellartesting.MockRPCClient) {
				m.SimulateTransactionFunc = func(ctx context.Context, req protocol.SimulateTransactionRequest) (protocol.SimulateTransactionResponse, error) {
					return stellartesting.NewSimulationResponse().
						WithTransactionData().
						WithAuth().
						Build(), nil
				}
				m.SendTransactionFunc = func(ctx context.Context, req protocol.SendTransactionRequest) (protocol.SendTransactionResponse, error) {
					return stellartesting.NewSendTransactionResponse().
						WithHash("set_max_withdraw_tx").
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
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockClient := stellartesting.NewMockRPCClient()
			tt.setupMock(mockClient)

			svc := newTestService(mockClient)
			err := svc.SetMaxWithdraw(context.Background(), tt.limit)

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

func TestSetLockPeriod(t *testing.T) {
	tests := []struct {
		name          string
		periodSeconds uint64
		setupMock     func(*stellartesting.MockRPCClient)
		wantErr       bool
		errContains   string
	}{
		{
			name:          "set 30 day lock period",
			periodSeconds: 2592000, // 30 days
			setupMock: func(m *stellartesting.MockRPCClient) {
				m.SimulateTransactionFunc = func(ctx context.Context, req protocol.SimulateTransactionRequest) (protocol.SimulateTransactionResponse, error) {
					return stellartesting.NewSimulationResponse().
						WithTransactionData().
						WithAuth().
						Build(), nil
				}
				m.SendTransactionFunc = func(ctx context.Context, req protocol.SendTransactionRequest) (protocol.SendTransactionResponse, error) {
					return stellartesting.NewSendTransactionResponse().
						WithHash("set_lock_period_tx").
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
			name:          "set no lock period",
			periodSeconds: 0,
			setupMock: func(m *stellartesting.MockRPCClient) {
				m.SimulateTransactionFunc = func(ctx context.Context, req protocol.SimulateTransactionRequest) (protocol.SimulateTransactionResponse, error) {
					return stellartesting.NewSimulationResponse().
						WithTransactionData().
						WithAuth().
						Build(), nil
				}
				m.SendTransactionFunc = func(ctx context.Context, req protocol.SendTransactionRequest) (protocol.SendTransactionResponse, error) {
					return stellartesting.NewSendTransactionResponse().Build(), nil
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
			name:          "unauthorized",
			periodSeconds: 2592000,
			setupMock: func(m *stellartesting.MockRPCClient) {
				m.SimulateTransactionFunc = func(ctx context.Context, req protocol.SimulateTransactionRequest) (protocol.SimulateTransactionResponse, error) {
					return stellartesting.NewSimulationResponse().
						WithError("Error(Contract, #1)"). // Unauthorized
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
			err := svc.SetLockPeriod(context.Background(), tt.periodSeconds)

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
