package soroban

import (
	"context"
	"testing"

	protocol "github.com/stellar/go-stellar-sdk/protocols/rpc"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	stellartesting "github.com/Shamba-Records-Limited/microvault/pkg/stellar/testing"
)

func newTestServiceWithComplianceRole(mockClient *stellartesting.MockRPCClient) Service {
	keys := stellartesting.NewTestKeys()
	return NewServiceWithClient(
		mockClient,
		stellartesting.TestNetworkPassphrase,
		keys.TreasurySecret,
		keys.AdminSecret,
		keys.ContractID,
	).WithComplianceRole(keys.UserSecret)
}

func TestAllowDepositor(t *testing.T) {
	tests := []struct {
		name        string
		withRole    bool
		setupMock   func(*stellartesting.MockRPCClient)
		wantErr     bool
		errContains string
	}{
		{
			name:     "successful allow",
			withRole: true,
			setupMock: func(m *stellartesting.MockRPCClient) {
				m.SimulateTransactionFunc = func(ctx context.Context, req protocol.SimulateTransactionRequest) (protocol.SimulateTransactionResponse, error) {
					return stellartesting.NewSimulationResponse().WithTransactionData().WithAuth().Build(), nil
				}
				m.SendTransactionFunc = func(ctx context.Context, req protocol.SendTransactionRequest) (protocol.SendTransactionResponse, error) {
					return stellartesting.NewSendTransactionResponse().WithHash("allow_tx_hash").Build(), nil
				}
				m.GetTransactionFunc = func(ctx context.Context, req protocol.GetTransactionRequest) (protocol.GetTransactionResponse, error) {
					return stellartesting.NewGetTransactionResponse().WithStatus(protocol.TransactionStatusSuccess).Build(), nil
				}
			},
			wantErr: false,
		},
		{
			name:        "no compliance role configured",
			withRole:    false,
			setupMock:   func(m *stellartesting.MockRPCClient) {},
			wantErr:     true,
			errContains: "no compliance role key configured",
		},
		{
			name:     "wrong caller rejected on-chain",
			withRole: true,
			setupMock: func(m *stellartesting.MockRPCClient) {
				m.SimulateTransactionFunc = func(ctx context.Context, req protocol.SimulateTransactionRequest) (protocol.SimulateTransactionResponse, error) {
					return stellartesting.NewSimulationResponse().
						WithError("Error(Contract, #1)"). // Unauthorized
						Build(), nil
				}
			},
			wantErr:     true,
			errContains: "simulation rejected",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockClient := stellartesting.NewMockRPCClient()
			tt.setupMock(mockClient)

			var svc Service
			if tt.withRole {
				svc = newTestServiceWithComplianceRole(mockClient)
			} else {
				svc = newTestService(mockClient)
			}

			depositor := stellartesting.NewTestKeys().UserPublic
			err := svc.AllowDepositor(context.Background(), depositor)

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

func TestDisallowDepositor(t *testing.T) {
	mockClient := stellartesting.NewMockRPCClient()
	mockClient.SimulateTransactionFunc = func(ctx context.Context, req protocol.SimulateTransactionRequest) (protocol.SimulateTransactionResponse, error) {
		return stellartesting.NewSimulationResponse().WithTransactionData().WithAuth().Build(), nil
	}
	mockClient.SendTransactionFunc = func(ctx context.Context, req protocol.SendTransactionRequest) (protocol.SendTransactionResponse, error) {
		return stellartesting.NewSendTransactionResponse().WithHash("disallow_tx_hash").Build(), nil
	}
	mockClient.GetTransactionFunc = func(ctx context.Context, req protocol.GetTransactionRequest) (protocol.GetTransactionResponse, error) {
		return stellartesting.NewGetTransactionResponse().WithStatus(protocol.TransactionStatusSuccess).Build(), nil
	}

	svc := newTestServiceWithComplianceRole(mockClient)
	depositor := stellartesting.NewTestKeys().UserPublic
	require.NoError(t, svc.DisallowDepositor(context.Background(), depositor))
}

func TestAllowDepositor_InvalidAddress(t *testing.T) {
	mockClient := stellartesting.NewMockRPCClient()
	svc := newTestServiceWithComplianceRole(mockClient)
	err := svc.AllowDepositor(context.Background(), "not-a-valid-address")
	require.Error(t, err)
}
