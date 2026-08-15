package classic

import (
	"context"
	"testing"

	"github.com/stellar/go-stellar-sdk/keypair"
	protocol "github.com/stellar/go-stellar-sdk/protocols/rpc"
	"github.com/stellar/go-stellar-sdk/txnbuild"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	stellartesting "github.com/Shamba-Records-Limited/microvault/pkg/stellar/testing"
	"github.com/Shamba-Records-Limited/microvault/pkg/stellar/types"
)

// Verify MockRPCClient implements RPCClient interface
var _ RPCClient = (*stellartesting.MockRPCClient)(nil)

// ============================================================================
// Test Setup
// ============================================================================

const testUSDCIssuer = "GBBD47IF6LWK7P7MDEVSCWR7DPUWV3NY3DTQEVFL4NAT4AQH3ZLLFLA5"

func newTestService(mockClient *stellartesting.MockRPCClient) Service {
	keys := stellartesting.NewTestKeys()
	return NewServiceWithClient(
		mockClient,
		stellartesting.TestNetworkPassphrase,
		keys.TreasurySecret,
		testUSDCIssuer,
	)
}

// ============================================================================
// CreateSponsoredAccount Tests
// ============================================================================

func TestCreateSponsoredAccount(t *testing.T) {
	keys := stellartesting.NewTestKeys()
	childKP := keypair.MustParseFull(keys.UserSecret)

	tests := []struct {
		name        string
		req         types.CreateAccountRequest
		setupMock   func(*stellartesting.MockRPCClient)
		wantErr     bool
		errContains string
	}{
		{
			name: "successful account creation without multisig",
			req: types.CreateAccountRequest{
				ChildKeypair:    childKP,
				StartingBalance: "0",
				EnableMultiSig:  false,
			},
			setupMock: func(m *stellartesting.MockRPCClient) {
				m.SendTransactionFunc = func(ctx context.Context, req protocol.SendTransactionRequest) (protocol.SendTransactionResponse, error) {
					return stellartesting.NewSendTransactionResponse().
						WithHash("create_account_tx_hash").
						WithStatus("PENDING").
						Build(), nil
				}
				m.GetTransactionFunc = func(ctx context.Context, req protocol.GetTransactionRequest) (protocol.GetTransactionResponse, error) {
					return stellartesting.NewGetTransactionResponse().
						WithStatus(protocol.TransactionStatusSuccess).
						WithLedger(12345).
						Build(), nil
				}
			},
			wantErr: false,
		},
		{
			name: "successful account creation with multisig",
			req: types.CreateAccountRequest{
				ChildKeypair:    childKP,
				StartingBalance: "0",
				EnableMultiSig:  true,
				MultiSigConfig: &types.MultiSigConfig{
					TreasurySigner: keys.TreasuryPublic,
					TreasuryWeight: 1,
					ChildWeight:    0,
					LowThreshold:   1,
					MedThreshold:   1,
					HighThreshold:  1,
				},
			},
			setupMock: func(m *stellartesting.MockRPCClient) {
				m.SendTransactionFunc = func(ctx context.Context, req protocol.SendTransactionRequest) (protocol.SendTransactionResponse, error) {
					return stellartesting.NewSendTransactionResponse().
						WithHash("create_multisig_account_tx_hash").
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
			name: "failed to load sponsor account",
			req: types.CreateAccountRequest{
				ChildKeypair:    childKP,
				StartingBalance: "0",
			},
			setupMock: func(m *stellartesting.MockRPCClient) {
				m.LoadAccountFunc = func(ctx context.Context, address string) (txnbuild.Account, error) {
					return nil, assert.AnError
				}
			},
			wantErr: true,
		},
		{
			name: "transaction rejected",
			req: types.CreateAccountRequest{
				ChildKeypair:    childKP,
				StartingBalance: "0",
			},
			setupMock: func(m *stellartesting.MockRPCClient) {
				m.SendTransactionFunc = func(ctx context.Context, req protocol.SendTransactionRequest) (protocol.SendTransactionResponse, error) {
					return protocol.SendTransactionResponse{
						Status: "ERROR",
					}, nil
				}
			},
			wantErr: true,
		},
		{
			name: "transaction failed on ledger",
			req: types.CreateAccountRequest{
				ChildKeypair:    childKP,
				StartingBalance: "0",
			},
			setupMock: func(m *stellartesting.MockRPCClient) {
				m.SendTransactionFunc = func(ctx context.Context, req protocol.SendTransactionRequest) (protocol.SendTransactionResponse, error) {
					return stellartesting.NewSendTransactionResponse().
						WithHash("failed_tx").
						Build(), nil
				}
				m.GetTransactionFunc = func(ctx context.Context, req protocol.GetTransactionRequest) (protocol.GetTransactionResponse, error) {
					return stellartesting.NewGetTransactionResponse().
						WithStatus(protocol.TransactionStatusFailed).
						Build(), nil
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
			err := svc.CreateSponsoredAccount(context.Background(), tt.req)

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

// ============================================================================
// EstablishSponsoredTrustline Tests
// ============================================================================

func TestEstablishSponsoredTrustline(t *testing.T) {
	keys := stellartesting.NewTestKeys()
	childKP := keypair.MustParseFull(keys.UserSecret)

	tests := []struct {
		name        string
		req         types.EstablishTrustlineRequest
		setupMock   func(*stellartesting.MockRPCClient)
		wantErr     bool
		errContains string
	}{
		{
			name: "successful trustline establishment",
			req:  types.EstablishTrustlineRequest{ChildKeypair: childKP},
			setupMock: func(m *stellartesting.MockRPCClient) {
				m.SendTransactionFunc = func(ctx context.Context, req protocol.SendTransactionRequest) (protocol.SendTransactionResponse, error) {
					return stellartesting.NewSendTransactionResponse().
						WithHash("establish_trustline_tx_hash").
						WithStatus("PENDING").
						Build(), nil
				}
				m.GetTransactionFunc = func(ctx context.Context, req protocol.GetTransactionRequest) (protocol.GetTransactionResponse, error) {
					return stellartesting.NewGetTransactionResponse().
						WithStatus(protocol.TransactionStatusSuccess).
						WithLedger(12345).
						Build(), nil
				}
			},
			wantErr: false,
		},
		{
			name: "failed to load sponsor account",
			req:  types.EstablishTrustlineRequest{ChildKeypair: childKP},
			setupMock: func(m *stellartesting.MockRPCClient) {
				m.LoadAccountFunc = func(ctx context.Context, address string) (txnbuild.Account, error) {
					return nil, assert.AnError
				}
			},
			wantErr: true,
		},
		{
			name: "transaction rejected",
			req:  types.EstablishTrustlineRequest{ChildKeypair: childKP},
			setupMock: func(m *stellartesting.MockRPCClient) {
				m.SendTransactionFunc = func(ctx context.Context, req protocol.SendTransactionRequest) (protocol.SendTransactionResponse, error) {
					return protocol.SendTransactionResponse{
						Status: "ERROR",
					}, nil
				}
			},
			wantErr: true,
		},
		{
			name: "transaction failed on ledger",
			req:  types.EstablishTrustlineRequest{ChildKeypair: childKP},
			setupMock: func(m *stellartesting.MockRPCClient) {
				m.SendTransactionFunc = func(ctx context.Context, req protocol.SendTransactionRequest) (protocol.SendTransactionResponse, error) {
					return stellartesting.NewSendTransactionResponse().
						WithHash("failed_tx").
						Build(), nil
				}
				m.GetTransactionFunc = func(ctx context.Context, req protocol.GetTransactionRequest) (protocol.GetTransactionResponse, error) {
					return stellartesting.NewGetTransactionResponse().
						WithStatus(protocol.TransactionStatusFailed).
						Build(), nil
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
			err := svc.EstablishSponsoredTrustline(context.Background(), tt.req)

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

// ============================================================================
// SponsoredPaymentTransaction Tests
// ============================================================================

func TestSponsoredPaymentTransaction(t *testing.T) {
	keys := stellartesting.NewTestKeys()

	tests := []struct {
		name        string
		req         types.SponsoredPaymentTransactionRequest
		setupMock   func(*stellartesting.MockRPCClient)
		wantErr     bool
		errContains string
	}{
		{
			name: "successful sponsored payment",
			req: types.SponsoredPaymentTransactionRequest{
				Destination: keys.UserPublic,
				Amount:      1000000000, // 100 USDC
				Source:      keys.TreasuryPublic,
				AssetCode:   "USDC",
				AssetIssuer: testUSDCIssuer,
			},
			setupMock: func(m *stellartesting.MockRPCClient) {
				// Mock trustline check
				m.GetLedgerEntriesFunc = func(ctx context.Context, req protocol.GetLedgerEntriesRequest) (protocol.GetLedgerEntriesResponse, error) {
					return protocol.GetLedgerEntriesResponse{
						Entries: []protocol.LedgerEntryResult{{}}, // Has trustline
					}, nil
				}
				m.SendTransactionFunc = func(ctx context.Context, req protocol.SendTransactionRequest) (protocol.SendTransactionResponse, error) {
					return stellartesting.NewSendTransactionResponse().
						WithHash("payment_tx_hash").
						Build(), nil
				}
				m.GetTransactionFunc = func(ctx context.Context, req protocol.GetTransactionRequest) (protocol.GetTransactionResponse, error) {
					return stellartesting.NewGetTransactionResponse().
						WithStatus(protocol.TransactionStatusSuccess).
						WithLedger(12345).
						Build(), nil
				}
			},
			wantErr: false,
		},
		{
			name: "invalid destination address",
			req: types.SponsoredPaymentTransactionRequest{
				Destination: "invalid_address",
				Amount:      1000000000,
				Source:      keys.TreasuryPublic,
				AssetCode:   "USDC",
				AssetIssuer: testUSDCIssuer,
			},
			setupMock: func(m *stellartesting.MockRPCClient) {},
			wantErr:   true,
		},
		{
			name: "invalid amount - zero",
			req: types.SponsoredPaymentTransactionRequest{
				Destination: keys.UserPublic,
				Amount:      0,
				Source:      keys.TreasuryPublic,
				AssetCode:   "USDC",
				AssetIssuer: testUSDCIssuer,
			},
			setupMock: func(m *stellartesting.MockRPCClient) {},
			wantErr:   true,
		},
		{
			name: "invalid amount - negative",
			req: types.SponsoredPaymentTransactionRequest{
				Destination: keys.UserPublic,
				Amount:      -100,
				Source:      keys.TreasuryPublic,
				AssetCode:   "USDC",
				AssetIssuer: testUSDCIssuer,
			},
			setupMock: func(m *stellartesting.MockRPCClient) {},
			wantErr:   true,
		},
		{
			name: "missing trustline",
			req: types.SponsoredPaymentTransactionRequest{
				Destination: keys.UserPublic,
				Amount:      1000000000,
				Source:      keys.TreasuryPublic,
				AssetCode:   "USDC",
				AssetIssuer: testUSDCIssuer,
			},
			setupMock: func(m *stellartesting.MockRPCClient) {
				m.GetLedgerEntriesFunc = func(ctx context.Context, req protocol.GetLedgerEntriesRequest) (protocol.GetLedgerEntriesResponse, error) {
					return protocol.GetLedgerEntriesResponse{
						Entries: []protocol.LedgerEntryResult{}, // No trustline
					}, nil
				}
			},
			wantErr: true,
		},
		{
			name: "transaction rejected",
			req: types.SponsoredPaymentTransactionRequest{
				Destination: keys.UserPublic,
				Amount:      1000000000,
				Source:      keys.TreasuryPublic,
				AssetCode:   "USDC",
				AssetIssuer: testUSDCIssuer,
			},
			setupMock: func(m *stellartesting.MockRPCClient) {
				m.GetLedgerEntriesFunc = func(ctx context.Context, req protocol.GetLedgerEntriesRequest) (protocol.GetLedgerEntriesResponse, error) {
					return protocol.GetLedgerEntriesResponse{
						Entries: []protocol.LedgerEntryResult{{}},
					}, nil
				}
				m.SendTransactionFunc = func(ctx context.Context, req protocol.SendTransactionRequest) (protocol.SendTransactionResponse, error) {
					return protocol.SendTransactionResponse{
						Status: "ERROR",
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
			resp, err := svc.SponsoredPaymentTransaction(context.Background(), tt.req)

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
			assert.Equal(t, protocol.TransactionStatusSuccess, resp.Status)
		})
	}
}

// ============================================================================
// Mock Call Tracking Tests
// ============================================================================

func TestMockCallTracking(t *testing.T) {
	mockClient := stellartesting.NewMockRPCClient()
	keys := stellartesting.NewTestKeys()

	// Configure successful responses
	mockClient.SendTransactionFunc = func(ctx context.Context, req protocol.SendTransactionRequest) (protocol.SendTransactionResponse, error) {
		return stellartesting.NewSendTransactionResponse().Build(), nil
	}
	mockClient.GetTransactionFunc = func(ctx context.Context, req protocol.GetTransactionRequest) (protocol.GetTransactionResponse, error) {
		return stellartesting.NewGetTransactionResponse().
			WithStatus(protocol.TransactionStatusSuccess).
			Build(), nil
	}

	svc := newTestService(mockClient)
	childKP := keypair.MustParseFull(keys.UserSecret)

	err := svc.CreateSponsoredAccount(context.Background(), types.CreateAccountRequest{
		ChildKeypair:    childKP,
		StartingBalance: "0",
	})
	require.NoError(t, err)

	// Verify calls were tracked
	assert.Len(t, mockClient.LoadAccountCalls, 1)
	assert.Equal(t, keys.TreasuryPublic, mockClient.LoadAccountCalls[0])
	assert.Len(t, mockClient.SendTransactionCalls, 1)
	assert.Len(t, mockClient.GetTransactionCalls, 1)
}
