package soroban

import (
	"context"
	"testing"

	protocol "github.com/stellar/go-stellar-sdk/protocols/rpc"
	"github.com/stellar/go-stellar-sdk/xdr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	stellartesting "github.com/Shamba-Records-Limited/microvault/internal/core/services/stellar/testing"
)

// Verify MockRPCClient implements RPCClient interface
var _ RPCClient = (*stellartesting.MockRPCClient)(nil)

// ============================================================================
// Test Setup
// ============================================================================

func newTestService(mockClient *stellartesting.MockRPCClient) Service {
	keys := stellartesting.NewTestKeys()
	return NewServiceWithClient(
		mockClient,
		stellartesting.TestNetworkPassphrase,
		keys.TreasurySecret,
		keys.AdminSecret,
		keys.ContractID,
	)
}

// ============================================================================
// View Function Tests
// ============================================================================

func TestGetTreasuryAddress(t *testing.T) {
	keys := stellartesting.NewTestKeys()

	tests := []struct {
		name        string
		setupMock   func(*stellartesting.MockRPCClient)
		want        string
		wantErr     bool
		errContains string
	}{
		{
			name: "success",
			setupMock: func(m *stellartesting.MockRPCClient) {
				m.SimulateTransactionFunc = func(ctx context.Context, req protocol.SimulateTransactionRequest) (protocol.SimulateTransactionResponse, error) {
					return stellartesting.NewSimulationResponse().
						WithAddressResult(keys.TreasuryPublic).
						Build(), nil
				}
			},
			want:    keys.TreasuryPublic,
			wantErr: false,
		},
		{
			name: "simulation error",
			setupMock: func(m *stellartesting.MockRPCClient) {
				m.SimulateTransactionFunc = func(ctx context.Context, req protocol.SimulateTransactionRequest) (protocol.SimulateTransactionResponse, error) {
					return stellartesting.NewSimulationResponse().
						WithError("contract execution failed").
						Build(), nil
				}
			},
			wantErr:     true,
			errContains: "simulation error",
		},
		{
			name: "empty results",
			setupMock: func(m *stellartesting.MockRPCClient) {
				m.SimulateTransactionFunc = func(ctx context.Context, req protocol.SimulateTransactionRequest) (protocol.SimulateTransactionResponse, error) {
					return protocol.SimulateTransactionResponse{
						Results: []protocol.SimulateHostFunctionResult{},
					}, nil
				}
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockClient := stellartesting.NewMockRPCClient()
			tt.setupMock(mockClient)

			svc := newTestService(mockClient)
			result, err := svc.GetTreasuryAddress(context.Background())

			if tt.wantErr {
				require.Error(t, err)
				if tt.errContains != "" {
					assert.Contains(t, err.Error(), tt.errContains)
				}
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.want, result)
		})
	}
}

func TestGetTotalBorrowed(t *testing.T) {
	tests := []struct {
		name        string
		setupMock   func(*stellartesting.MockRPCClient)
		want        int64
		wantErr     bool
		errContains string
	}{
		{
			name: "success with positive amount",
			setupMock: func(m *stellartesting.MockRPCClient) {
				m.SimulateTransactionFunc = func(ctx context.Context, req protocol.SimulateTransactionRequest) (protocol.SimulateTransactionResponse, error) {
					return stellartesting.NewSimulationResponse().
						WithI128Result(1000000000). // 100 USDC in stroops
						Build(), nil
				}
			},
			want:    1000000000,
			wantErr: false,
		},
		{
			name: "success with zero",
			setupMock: func(m *stellartesting.MockRPCClient) {
				m.SimulateTransactionFunc = func(ctx context.Context, req protocol.SimulateTransactionRequest) (protocol.SimulateTransactionResponse, error) {
					return stellartesting.NewSimulationResponse().
						WithI128Result(0).
						Build(), nil
				}
			},
			want:    0,
			wantErr: false,
		},
		{
			name: "simulation error",
			setupMock: func(m *stellartesting.MockRPCClient) {
				m.SimulateTransactionFunc = func(ctx context.Context, req protocol.SimulateTransactionRequest) (protocol.SimulateTransactionResponse, error) {
					return stellartesting.NewSimulationResponse().
						WithError("insufficient balance").
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
			result, err := svc.GetTotalBorrowed(context.Background())

			if tt.wantErr {
				require.Error(t, err)
				if tt.errContains != "" {
					assert.Contains(t, err.Error(), tt.errContains)
				}
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.want, result)
		})
	}
}

func TestGetAvailableLiquidity(t *testing.T) {
	tests := []struct {
		name      string
		setupMock func(*stellartesting.MockRPCClient)
		want      int64
		wantErr   bool
	}{
		{
			name: "success",
			setupMock: func(m *stellartesting.MockRPCClient) {
				m.SimulateTransactionFunc = func(ctx context.Context, req protocol.SimulateTransactionRequest) (protocol.SimulateTransactionResponse, error) {
					return stellartesting.NewSimulationResponse().
						WithI128Result(5000000000). // 500 USDC
						Build(), nil
				}
			},
			want:    5000000000,
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockClient := stellartesting.NewMockRPCClient()
			tt.setupMock(mockClient)

			svc := newTestService(mockClient)
			result, err := svc.GetAvailableLiquidity(context.Background())

			if tt.wantErr {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.want, result)
		})
	}
}

func TestGetUtilizationRate(t *testing.T) {
	tests := []struct {
		name      string
		setupMock func(*stellartesting.MockRPCClient)
		want      int64
		wantErr   bool
	}{
		{
			name: "80% utilization",
			setupMock: func(m *stellartesting.MockRPCClient) {
				m.SimulateTransactionFunc = func(ctx context.Context, req protocol.SimulateTransactionRequest) (protocol.SimulateTransactionResponse, error) {
					return stellartesting.NewSimulationResponse().
						WithI128Result(8000). // 80% in basis points
						Build(), nil
				}
			},
			want:    8000,
			wantErr: false,
		},
		{
			name: "0% utilization",
			setupMock: func(m *stellartesting.MockRPCClient) {
				m.SimulateTransactionFunc = func(ctx context.Context, req protocol.SimulateTransactionRequest) (protocol.SimulateTransactionResponse, error) {
					return stellartesting.NewSimulationResponse().
						WithI128Result(0).
						Build(), nil
				}
			},
			want:    0,
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockClient := stellartesting.NewMockRPCClient()
			tt.setupMock(mockClient)

			svc := newTestService(mockClient)
			result, err := svc.GetUtilizationRate(context.Background())

			if tt.wantErr {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.want, result)
		})
	}
}

func TestGetBorrowAPR(t *testing.T) {
	tests := []struct {
		name      string
		setupMock func(*stellartesting.MockRPCClient)
		want      int64
		wantErr   bool
	}{
		{
			name: "5% APR",
			setupMock: func(m *stellartesting.MockRPCClient) {
				m.SimulateTransactionFunc = func(ctx context.Context, req protocol.SimulateTransactionRequest) (protocol.SimulateTransactionResponse, error) {
					return stellartesting.NewSimulationResponse().
						WithI128Result(500). // 5% in basis points
						Build(), nil
				}
			},
			want:    500,
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockClient := stellartesting.NewMockRPCClient()
			tt.setupMock(mockClient)

			svc := newTestService(mockClient)
			result, err := svc.GetBorrowAPR(context.Background())

			if tt.wantErr {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.want, result)
		})
	}
}

func TestIsUserLocked(t *testing.T) {
	keys := stellartesting.NewTestKeys()

	tests := []struct {
		name        string
		userAddress string
		setupMock   func(*stellartesting.MockRPCClient)
		want        bool
		wantErr     bool
		errContains string
	}{
		{
			name:        "user is locked",
			userAddress: keys.UserPublic,
			setupMock: func(m *stellartesting.MockRPCClient) {
				m.SimulateTransactionFunc = func(ctx context.Context, req protocol.SimulateTransactionRequest) (protocol.SimulateTransactionResponse, error) {
					return stellartesting.NewSimulationResponse().
						WithBoolResult(true).
						Build(), nil
				}
			},
			want:    true,
			wantErr: false,
		},
		{
			name:        "user is not locked",
			userAddress: keys.UserPublic,
			setupMock: func(m *stellartesting.MockRPCClient) {
				m.SimulateTransactionFunc = func(ctx context.Context, req protocol.SimulateTransactionRequest) (protocol.SimulateTransactionResponse, error) {
					return stellartesting.NewSimulationResponse().
						WithBoolResult(false).
						Build(), nil
				}
			},
			want:    false,
			wantErr: false,
		},
		{
			name:        "invalid address",
			userAddress: "invalid",
			setupMock:   func(m *stellartesting.MockRPCClient) {},
			wantErr:     true,
			errContains: "invalid user address",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockClient := stellartesting.NewMockRPCClient()
			tt.setupMock(mockClient)

			svc := newTestService(mockClient)
			result, err := svc.IsUserLocked(context.Background(), tt.userAddress)

			if tt.wantErr {
				require.Error(t, err)
				if tt.errContains != "" {
					assert.Contains(t, err.Error(), tt.errContains)
				}
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.want, result)
		})
	}
}

func TestGetLockPeriod(t *testing.T) {
	tests := []struct {
		name      string
		setupMock func(*stellartesting.MockRPCClient)
		want      uint64
		wantErr   bool
	}{
		{
			name: "30 days lock period",
			setupMock: func(m *stellartesting.MockRPCClient) {
				m.SimulateTransactionFunc = func(ctx context.Context, req protocol.SimulateTransactionRequest) (protocol.SimulateTransactionResponse, error) {
					return stellartesting.NewSimulationResponse().
						WithU64Result(2592000). // 30 days in seconds
						Build(), nil
				}
			},
			want:    2592000,
			wantErr: false,
		},
		{
			name: "no lock period",
			setupMock: func(m *stellartesting.MockRPCClient) {
				m.SimulateTransactionFunc = func(ctx context.Context, req protocol.SimulateTransactionRequest) (protocol.SimulateTransactionResponse, error) {
					return stellartesting.NewSimulationResponse().
						WithU64Result(0).
						Build(), nil
				}
			},
			want:    0,
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockClient := stellartesting.NewMockRPCClient()
			tt.setupMock(mockClient)

			svc := newTestService(mockClient)
			result, err := svc.GetLockPeriod(context.Background())

			if tt.wantErr {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.want, result)
		})
	}
}

func TestIsPaused(t *testing.T) {
	tests := []struct {
		name      string
		setupMock func(*stellartesting.MockRPCClient)
		want      bool
		wantErr   bool
	}{
		{
			name: "vault is paused",
			setupMock: func(m *stellartesting.MockRPCClient) {
				m.SimulateTransactionFunc = func(ctx context.Context, req protocol.SimulateTransactionRequest) (protocol.SimulateTransactionResponse, error) {
					return stellartesting.NewSimulationResponse().
						WithBoolResult(true).
						Build(), nil
				}
			},
			want:    true,
			wantErr: false,
		},
		{
			name: "vault is not paused",
			setupMock: func(m *stellartesting.MockRPCClient) {
				m.SimulateTransactionFunc = func(ctx context.Context, req protocol.SimulateTransactionRequest) (protocol.SimulateTransactionResponse, error) {
					return stellartesting.NewSimulationResponse().
						WithBoolResult(false).
						Build(), nil
				}
			},
			want:    false,
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockClient := stellartesting.NewMockRPCClient()
			tt.setupMock(mockClient)

			svc := newTestService(mockClient)
			result, err := svc.IsPaused(context.Background())

			if tt.wantErr {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.want, result)
		})
	}
}

// ============================================================================
// Internal Method Tests
// ============================================================================

func TestGetContractAddress(t *testing.T) {
	keys := stellartesting.NewTestKeys()
	mockClient := stellartesting.NewMockRPCClient()

	svc := NewServiceWithClient(
		mockClient,
		stellartesting.TestNetworkPassphrase,
		keys.TreasurySecret,
		keys.AdminSecret,
		keys.ContractID,
	).(*service)

	addr, err := svc.getContractAddress()
	require.NoError(t, err)
	assert.Equal(t, xdr.ScAddressTypeScAddressTypeContract, addr.Type)
}

func TestGetContractAddress_InvalidContractID(t *testing.T) {
	mockClient := stellartesting.NewMockRPCClient()
	keys := stellartesting.NewTestKeys()

	svc := NewServiceWithClient(
		mockClient,
		stellartesting.TestNetworkPassphrase,
		keys.TreasurySecret,
		keys.AdminSecret,
		"invalid-contract-id",
	).(*service)

	_, err := svc.getContractAddress()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid contract ID")
}

func TestBuildInvokeContractOp(t *testing.T) {
	keys := stellartesting.NewTestKeys()
	mockClient := stellartesting.NewMockRPCClient()

	svc := NewServiceWithClient(
		mockClient,
		stellartesting.TestNetworkPassphrase,
		keys.TreasurySecret,
		keys.AdminSecret,
		keys.ContractID,
	).(*service)

	tests := []struct {
		name         string
		functionName string
		args         []xdr.ScVal
		wantErr      bool
	}{
		{
			name:         "no args",
			functionName: "total_borrowed",
			args:         nil,
			wantErr:      false,
		},
		{
			name:         "with args",
			functionName: "borrow",
			args: []xdr.ScVal{
				i128ToScVal(1000),
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			op, err := svc.buildInvokeContractOp(tt.functionName, tt.args)

			if tt.wantErr {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.NotNil(t, op)
			assert.Equal(t, xdr.HostFunctionTypeHostFunctionTypeInvokeContract, op.HostFunction.Type)
			assert.Equal(t, xdr.ScSymbol(tt.functionName), op.HostFunction.InvokeContract.FunctionName)
		})
	}
}
