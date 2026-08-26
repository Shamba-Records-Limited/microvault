package soroban

import (
	"context"
	"testing"

	"github.com/samber/oops"
	protocol "github.com/stellar/go-stellar-sdk/protocols/rpc"
	"github.com/stellar/go-stellar-sdk/xdr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	stellartesting "github.com/Shamba-Records-Limited/microvault/pkg/stellar/testing"
	"github.com/Shamba-Records-Limited/microvault/pkg/stellar/types"
)

// oopsCode reads the machine-readable code off a structured error. Asserting on
// the code rather than the message is what keeps these tests from breaking every
// time the wording of an error changes.
func oopsCode(t *testing.T, err error) string {
	t.Helper()

	var oopsErr oops.OopsError
	require.ErrorAs(t, err, &oopsErr, "error is not an oops error: %v", err)

	code, _ := oopsErr.Code().(string)
	return code
}

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
		wantErrIs   error
		wantCode    string
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
			setupMock: func(m *stellartesting.MockRPCClient) {},
			wantErr:   true,
			wantErrIs: types.ErrInvalidTransactionAmount,
			wantCode:  "invalid_amount",
		},
		{
			name: "invalid amount - negative",
			req: types.BorrowRequest{
				RecipientAddress: keys.UserPublic,
				Amount:           -100,
			},
			setupMock: func(m *stellartesting.MockRPCClient) {},
			wantErr:   true,
			wantErrIs: types.ErrInvalidTransactionAmount,
			wantCode:  "invalid_amount",
		},
		{
			name: "invalid recipient address",
			req: types.BorrowRequest{
				RecipientAddress: "invalid_address",
				Amount:           1000000000,
			},
			setupMock: func(m *stellartesting.MockRPCClient) {},
			wantErr:   true,
			wantErrIs: types.ErrInvalidStellarAddress,
			wantCode:  "invalid_address",
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
			wantErr:   true,
			wantErrIs: types.ErrSimulationFailed,
			wantCode:  "simulation_rejected",
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
			wantErr:   true,
			wantErrIs: types.ErrSimulationFailed,
			wantCode:  "simulation_rejected",
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
				if tt.wantErrIs != nil {
					assert.ErrorIs(t, err, tt.wantErrIs)
				}
				if tt.wantCode != "" {
					assert.Equal(t, tt.wantCode, oopsCode(t, err))
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
		wantErrIs   error
		wantCode    string
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
			setupMock: func(m *stellartesting.MockRPCClient) {},
			wantErr:   true,
			wantErrIs: types.ErrInvalidTransactionAmount,
			wantCode:  "invalid_amount",
		},
		{
			name: "invalid amount - negative",
			req: types.RepayRequest{
				Amount: -100,
			},
			setupMock: func(m *stellartesting.MockRPCClient) {},
			wantErr:   true,
			wantErrIs: types.ErrInvalidTransactionAmount,
			wantCode:  "invalid_amount",
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
			wantErr:   true,
			wantErrIs: types.ErrSimulationFailed,
			wantCode:  "simulation_rejected",
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
				if tt.wantErrIs != nil {
					assert.ErrorIs(t, err, tt.wantErrIs)
				}
				if tt.wantCode != "" {
					assert.Equal(t, tt.wantCode, oopsCode(t, err))
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
		wantErrIs   error
		wantCode    string
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
			wantErr:   true,
			wantErrIs: types.ErrSimulationFailed,
			wantCode:  "simulation_rejected",
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
				if tt.wantErrIs != nil {
					assert.ErrorIs(t, err, tt.wantErrIs)
				}
				if tt.wantCode != "" {
					assert.Equal(t, tt.wantCode, oopsCode(t, err))
				}
				return
			}

			require.NoError(t, err)
		})
	}
}

func TestRepayForVault(t *testing.T) {
	keys := stellartesting.NewTestKeys()

	// Captures the submitted envelope so tests can assert which contract
	// function was actually invoked, rather than trusting the response's
	// fallback value.
	newMockCapturingEnvelope := func(t *testing.T, envelope *string) *stellartesting.MockRPCClient {
		t.Helper()
		m := stellartesting.NewMockRPCClient()
		m.SimulateTransactionFunc = func(ctx context.Context, req protocol.SimulateTransactionRequest) (protocol.SimulateTransactionResponse, error) {
			return stellartesting.NewSimulationResponse().
				WithTransactionData().
				WithAuth().
				Build(), nil
		}
		m.SendTransactionFunc = func(ctx context.Context, req protocol.SendTransactionRequest) (protocol.SendTransactionResponse, error) {
			*envelope = req.Transaction
			return stellartesting.NewSendTransactionResponse().
				WithHash("repay_for_tx_hash_789").
				Build(), nil
		}
		m.GetTransactionFunc = func(ctx context.Context, req protocol.GetTransactionRequest) (protocol.GetTransactionResponse, error) {
			return stellartesting.NewGetTransactionResponse().
				WithStatus(protocol.TransactionStatusSuccess).
				WithResultMetaXDR(buildEventMetaXDR(t, keys.ContractID, "repaid", map[string]xdr.ScVal{
					"treasury":       addressScVal(t, keys.TreasuryPublic),
					"borrower":       addressScVal(t, keys.UserPublic),
					"amount":         i128ToScVal(500000000),
					"total_borrowed": i128ToScVal(500000000),
				})).
				Build(), nil
		}
		return m
	}

	t.Run("successful attributed repay", func(t *testing.T) {
		var envelope string
		svc := newTestService(newMockCapturingEnvelope(t, &envelope))

		resp, err := svc.RepayForVault(context.Background(), types.RepayForRequest{
			BorrowerAddress: keys.UserPublic,
			Amount:          500000000, // 50 USDC
		})

		require.NoError(t, err)
		require.NotNil(t, resp)
		assert.Equal(t, "repay_for_tx_hash_789", resp.TxHash)
		assert.Equal(t, int64(500000000), resp.AmountRepaid)
		assert.Equal(t, keys.UserPublic, resp.BorrowerAddress)
		assert.Equal(t, keys.UserPublic, resp.EventBorrower)

		// repay_for, not repay, and with the borrower as the second argument.
		contractID, fnName, err := ExtractContractInfo(envelope)
		require.NoError(t, err)
		assert.Equal(t, keys.ContractID, contractID)
		assert.Equal(t, "repay_for", fnName)
		assert.Equal(t, []xdr.ScVal{
			addressScVal(t, keys.TreasuryPublic),
			addressScVal(t, keys.UserPublic),
			i128ToScVal(500000000),
		}, invokeArgs(t, envelope))
	})

	t.Run("falls back to the requested borrower when the event is missing", func(t *testing.T) {
		var envelope string
		mockClient := newMockCapturingEnvelope(t, &envelope)
		mockClient.GetTransactionFunc = func(ctx context.Context, req protocol.GetTransactionRequest) (protocol.GetTransactionResponse, error) {
			return stellartesting.NewGetTransactionResponse().
				WithStatus(protocol.TransactionStatusSuccess).
				Build(), nil
		}

		svc := newTestService(mockClient)
		resp, err := svc.RepayForVault(context.Background(), types.RepayForRequest{
			BorrowerAddress: keys.UserPublic,
			Amount:          500000000,
		})

		require.NoError(t, err)
		assert.Equal(t, keys.UserPublic, resp.EventBorrower)
	})

	t.Run("missing borrower address", func(t *testing.T) {
		svc := newTestService(stellartesting.NewMockRPCClient())

		_, err := svc.RepayForVault(context.Background(), types.RepayForRequest{
			Amount: 500000000,
		})

		require.ErrorIs(t, err, types.ErrInvalidStellarAddress)
	})

	t.Run("invalid borrower address", func(t *testing.T) {
		svc := newTestService(stellartesting.NewMockRPCClient())

		_, err := svc.RepayForVault(context.Background(), types.RepayForRequest{
			BorrowerAddress: "not_an_address",
			Amount:          500000000,
		})

		require.Error(t, err)
		assert.ErrorIs(t, err, types.ErrInvalidStellarAddress)
		assert.Equal(t, "invalid_address", oopsCode(t, err))
	})

	t.Run("invalid amount", func(t *testing.T) {
		svc := newTestService(stellartesting.NewMockRPCClient())

		_, err := svc.RepayForVault(context.Background(), types.RepayForRequest{
			BorrowerAddress: keys.UserPublic,
			Amount:          0,
		})

		require.ErrorIs(t, err, types.ErrInvalidTransactionAmount)
	})

	t.Run("simulation error - repay exceeds debt", func(t *testing.T) {
		mockClient := stellartesting.NewMockRPCClient()
		mockClient.SimulateTransactionFunc = func(ctx context.Context, req protocol.SimulateTransactionRequest) (protocol.SimulateTransactionResponse, error) {
			return stellartesting.NewSimulationResponse().
				WithError("Error(Contract, #11)"). // RepayExceedsDebt
				Build(), nil
		}

		svc := newTestService(mockClient)
		_, err := svc.RepayForVault(context.Background(), types.RepayForRequest{
			BorrowerAddress: keys.UserPublic,
			Amount:          1000000000000,
		})

		require.Error(t, err)
		assert.ErrorIs(t, err, types.ErrSimulationFailed)
		assert.Equal(t, "simulation_rejected", oopsCode(t, err))
	})
}

// TestRepayToVaultInvokesUnattributedRepay guards the split between the two
// entrypoints: an unattributed repay must still call "repay" with two arguments.
func TestRepayToVaultInvokesUnattributedRepay(t *testing.T) {
	keys := stellartesting.NewTestKeys()

	var envelope string
	mockClient := stellartesting.NewMockRPCClient()
	mockClient.SimulateTransactionFunc = func(ctx context.Context, req protocol.SimulateTransactionRequest) (protocol.SimulateTransactionResponse, error) {
		return stellartesting.NewSimulationResponse().WithTransactionData().WithAuth().Build(), nil
	}
	mockClient.SendTransactionFunc = func(ctx context.Context, req protocol.SendTransactionRequest) (protocol.SendTransactionResponse, error) {
		envelope = req.Transaction
		return stellartesting.NewSendTransactionResponse().WithHash("repay_tx_hash").Build(), nil
	}

	svc := newTestService(mockClient)
	resp, err := svc.RepayToVault(context.Background(), types.RepayRequest{Amount: 500000000})
	require.NoError(t, err)
	assert.Empty(t, resp.BorrowerAddress)
	assert.Empty(t, resp.EventBorrower)

	_, fnName, err := ExtractContractInfo(envelope)
	require.NoError(t, err)
	assert.Equal(t, "repay", fnName)
	assert.Equal(t, []xdr.ScVal{
		addressScVal(t, keys.TreasuryPublic),
		i128ToScVal(500000000),
	}, invokeArgs(t, envelope))
}

func TestBumpYield(t *testing.T) {
	keys := stellartesting.NewTestKeys()

	t.Run("successful contribution", func(t *testing.T) {
		var envelope string
		mockClient := stellartesting.NewMockRPCClient()
		mockClient.SimulateTransactionFunc = func(ctx context.Context, req protocol.SimulateTransactionRequest) (protocol.SimulateTransactionResponse, error) {
			return stellartesting.NewSimulationResponse().WithTransactionData().WithAuth().Build(), nil
		}
		mockClient.SendTransactionFunc = func(ctx context.Context, req protocol.SendTransactionRequest) (protocol.SendTransactionResponse, error) {
			envelope = req.Transaction
			return stellartesting.NewSendTransactionResponse().WithHash("bump_yield_tx_hash").Build(), nil
		}
		mockClient.GetTransactionFunc = func(ctx context.Context, req protocol.GetTransactionRequest) (protocol.GetTransactionResponse, error) {
			return stellartesting.NewGetTransactionResponse().
				WithStatus(protocol.TransactionStatusSuccess).
				WithResultMetaXDR(buildEventMetaXDR(t, keys.ContractID, "yield_bumped", map[string]xdr.ScVal{
					"from":          addressScVal(t, keys.TreasuryPublic),
					"amount":        i128ToScVal(250000000),
					"total_managed": i128ToScVal(9750000000),
				})).
				Build(), nil
		}

		svc := newTestService(mockClient)
		resp, err := svc.BumpYield(context.Background(), types.BumpYieldRequest{Amount: 250000000})

		require.NoError(t, err)
		require.NotNil(t, resp)
		assert.Equal(t, "bump_yield_tx_hash", resp.TxHash)
		assert.Equal(t, int64(250000000), resp.AmountContributed)
		assert.Equal(t, int64(9750000000), resp.TotalManagedAssets)

		// The treasury is the source of the funds, so "from" is the treasury.
		_, fnName, err := ExtractContractInfo(envelope)
		require.NoError(t, err)
		assert.Equal(t, "bump_yield", fnName)
		assert.Equal(t, []xdr.ScVal{
			addressScVal(t, keys.TreasuryPublic),
			i128ToScVal(250000000),
		}, invokeArgs(t, envelope))
	})

	t.Run("missing YieldBumped event is not fatal", func(t *testing.T) {
		mockClient := stellartesting.NewMockRPCClient()
		mockClient.SimulateTransactionFunc = func(ctx context.Context, req protocol.SimulateTransactionRequest) (protocol.SimulateTransactionResponse, error) {
			return stellartesting.NewSimulationResponse().WithTransactionData().WithAuth().Build(), nil
		}

		svc := newTestService(mockClient)
		resp, err := svc.BumpYield(context.Background(), types.BumpYieldRequest{Amount: 250000000})

		require.NoError(t, err)
		assert.Equal(t, int64(250000000), resp.AmountContributed)
		assert.Zero(t, resp.TotalManagedAssets)
	})

	t.Run("invalid amount", func(t *testing.T) {
		svc := newTestService(stellartesting.NewMockRPCClient())

		_, err := svc.BumpYield(context.Background(), types.BumpYieldRequest{Amount: -1})

		require.ErrorIs(t, err, types.ErrInvalidTransactionAmount)
	})

	t.Run("simulation error - vault paused", func(t *testing.T) {
		mockClient := stellartesting.NewMockRPCClient()
		mockClient.SimulateTransactionFunc = func(ctx context.Context, req protocol.SimulateTransactionRequest) (protocol.SimulateTransactionResponse, error) {
			return stellartesting.NewSimulationResponse().
				WithError("Error(Contract, #1000)"). // EnforcedPause
				Build(), nil
		}

		svc := newTestService(mockClient)
		_, err := svc.BumpYield(context.Background(), types.BumpYieldRequest{Amount: 250000000})

		require.Error(t, err)
		assert.ErrorIs(t, err, types.ErrSimulationFailed)
		assert.Equal(t, "simulation_rejected", oopsCode(t, err))
	})
}

// invokeArgs returns the arguments of the InvokeHostFunction operation in a
// base64-encoded transaction envelope.
func invokeArgs(t *testing.T, envelopeXDR string) []xdr.ScVal {
	t.Helper()

	var env xdr.TransactionEnvelope
	require.NoError(t, xdr.SafeUnmarshalBase64(envelopeXDR, &env))

	for _, op := range env.V1.Tx.Operations {
		if op.Body.Type == xdr.OperationTypeInvokeHostFunction {
			return op.Body.InvokeHostFunctionOp.HostFunction.InvokeContract.Args
		}
	}

	t.Fatal("no invoke host function operation in envelope")
	return nil
}
