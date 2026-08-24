package soroban

import (
	"testing"

	"github.com/stellar/go-stellar-sdk/strkey"
	"github.com/stellar/go-stellar-sdk/xdr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	stellartesting "github.com/Shamba-Records-Limited/microvault/pkg/stellar/testing"
)

// ============================================================================
// Encoding Tests (Go to Soroban)
// ============================================================================

func TestAddressToScVal(t *testing.T) {
	tests := []struct {
		name        string
		address     string
		wantType    xdr.ScAddressType
		wantErr     bool
		errContains string
	}{
		{
			name:     "valid account address",
			address:  "GCUUFSK3P7YMFMYVA3L7LVQ7TCWJK7WUOFMXLX2HYSRCFB37ZGZD4CCX",
			wantType: xdr.ScAddressTypeScAddressTypeAccount,
			wantErr:  false,
		},
		{
			name:     "valid contract address",
			address:  "CA7HVINR2AY532D7UPOE7MMOZAN2ZBZ327VWIO33K7JM7C4G6XYX5NOT",
			wantType: xdr.ScAddressTypeScAddressTypeContract,
			wantErr:  false,
		},
		{
			name:        "invalid address",
			address:     "invalid",
			wantErr:     true,
			errContains: "invalid stellar address",
		},
		{
			name:        "empty address",
			address:     "",
			wantErr:     true,
			errContains: "invalid stellar address",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := addressToScVal(tt.address)

			if tt.wantErr {
				require.Error(t, err)
				if tt.errContains != "" {
					assert.Contains(t, err.Error(), tt.errContains)
				}
				return
			}

			require.NoError(t, err)
			assert.Equal(t, xdr.ScValTypeScvAddress, result.Type)
			assert.NotNil(t, result.Address)
			assert.Equal(t, tt.wantType, result.Address.Type)
		})
	}
}

func TestAddressToScVal_RoundTrip(t *testing.T) {
	// Test that we can encode and decode back to the same address
	tests := []struct {
		name    string
		address string
	}{
		{
			name:    "account address roundtrip",
			address: "GCUUFSK3P7YMFMYVA3L7LVQ7TCWJK7WUOFMXLX2HYSRCFB37ZGZD4CCX",
		},
		{
			name:    "contract address roundtrip",
			address: "CA7HVINR2AY532D7UPOE7MMOZAN2ZBZ327VWIO33K7JM7C4G6XYX5NOT",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			scVal, err := addressToScVal(tt.address)
			require.NoError(t, err)

			decoded, err := scValToAddress(scVal)
			require.NoError(t, err)
			assert.Equal(t, tt.address, decoded)
		})
	}
}

func TestI128ToScVal(t *testing.T) {
	tests := []struct {
		name   string
		input  int64
		wantHi int64
		wantLo uint64
	}{
		{
			name:   "positive value",
			input:  1000,
			wantHi: 0,
			wantLo: 1000,
		},
		{
			name:   "zero",
			input:  0,
			wantHi: 0,
			wantLo: 0,
		},
		{
			name:   "negative value",
			input:  -1,
			wantHi: -1,
			wantLo: 0xFFFFFFFFFFFFFFFF,
		},
		{
			name:   "negative value -100",
			input:  -100,
			wantHi: -1,
			wantLo: 0xFFFFFFFFFFFFFF9C, // Two's complement of -100
		},
		{
			name:   "max int64",
			input:  9223372036854775807,
			wantHi: 0,
			wantLo: 9223372036854775807,
		},
		{
			name:   "min int64",
			input:  -9223372036854775808,
			wantHi: -1,
			wantLo: 0x8000000000000000,
		},
		{
			name:   "typical loan amount in stroops (100 USDC)",
			input:  1000000000, // 100 * 10^7
			wantHi: 0,
			wantLo: 1000000000,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := i128ToScVal(tt.input)

			assert.Equal(t, xdr.ScValTypeScvI128, result.Type)
			require.NotNil(t, result.I128)
			assert.Equal(t, xdr.Int64(tt.wantHi), result.I128.Hi)
			assert.Equal(t, xdr.Uint64(tt.wantLo), result.I128.Lo)
		})
	}
}

func TestI128ToScVal_RoundTrip(t *testing.T) {
	tests := []int64{
		0,
		1,
		-1,
		100,
		-100,
		1000000000,
		9223372036854775807,
		-9223372036854775808,
	}

	for _, input := range tests {
		t.Run("roundtrip", func(t *testing.T) {
			scVal := i128ToScVal(input)
			decoded, err := scValToI128(scVal)
			require.NoError(t, err)
			assert.Equal(t, input, decoded)
		})
	}
}

func TestU64ToScVal(t *testing.T) {
	tests := []struct {
		name  string
		input uint64
	}{
		{
			name:  "zero",
			input: 0,
		},
		{
			name:  "positive value",
			input: 1000,
		},
		{
			name:  "max uint64",
			input: 18446744073709551615,
		},
		{
			name:  "typical lock period (30 days in seconds)",
			input: 2592000,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := u64ToScVal(tt.input)

			assert.Equal(t, xdr.ScValTypeScvU64, result.Type)
			require.NotNil(t, result.U64)
			assert.Equal(t, xdr.Uint64(tt.input), *result.U64)
		})
	}
}

func TestU64ToScVal_RoundTrip(t *testing.T) {
	tests := []uint64{
		0,
		1,
		1000,
		2592000,
		18446744073709551615,
	}

	for _, input := range tests {
		t.Run("roundtrip", func(t *testing.T) {
			scVal := u64ToScVal(input)
			decoded, err := scValToU64(scVal)
			require.NoError(t, err)
			assert.Equal(t, input, decoded)
		})
	}
}

// ============================================================================
// Decoding Tests (Soroban to Go)
// ============================================================================

func TestScValToI128(t *testing.T) {
	tests := []struct {
		name        string
		input       xdr.ScVal
		want        int64
		wantErr     bool
		errContains string
	}{
		{
			name: "valid i128 positive",
			input: xdr.ScVal{
				Type: xdr.ScValTypeScvI128,
				I128: &xdr.Int128Parts{Hi: 0, Lo: 1000},
			},
			want:    1000,
			wantErr: false,
		},
		{
			name: "valid i128 zero",
			input: xdr.ScVal{
				Type: xdr.ScValTypeScvI128,
				I128: &xdr.Int128Parts{Hi: 0, Lo: 0},
			},
			want:    0,
			wantErr: false,
		},
		{
			name: "wrong type - bool",
			input: xdr.ScVal{
				Type: xdr.ScValTypeScvBool,
			},
			wantErr:     true,
			errContains: "expected i128",
		},
		{
			name: "wrong type - u64",
			input: xdr.ScVal{
				Type: xdr.ScValTypeScvU64,
			},
			wantErr:     true,
			errContains: "expected i128",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := scValToI128(tt.input)

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

func TestScValToBool(t *testing.T) {
	trueVal := true
	falseVal := false

	tests := []struct {
		name        string
		input       xdr.ScVal
		want        bool
		wantErr     bool
		errContains string
	}{
		{
			name: "true value",
			input: xdr.ScVal{
				Type: xdr.ScValTypeScvBool,
				B:    &trueVal,
			},
			want:    true,
			wantErr: false,
		},
		{
			name: "false value",
			input: xdr.ScVal{
				Type: xdr.ScValTypeScvBool,
				B:    &falseVal,
			},
			want:    false,
			wantErr: false,
		},
		{
			name: "wrong type - i128",
			input: xdr.ScVal{
				Type: xdr.ScValTypeScvI128,
			},
			wantErr:     true,
			errContains: "expected bool",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := scValToBool(tt.input)

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

func TestScValToU64(t *testing.T) {
	val1000 := xdr.Uint64(1000)
	val0 := xdr.Uint64(0)
	valMax := xdr.Uint64(18446744073709551615)

	tests := []struct {
		name        string
		input       xdr.ScVal
		want        uint64
		wantErr     bool
		errContains string
	}{
		{
			name: "positive value",
			input: xdr.ScVal{
				Type: xdr.ScValTypeScvU64,
				U64:  &val1000,
			},
			want:    1000,
			wantErr: false,
		},
		{
			name: "zero value",
			input: xdr.ScVal{
				Type: xdr.ScValTypeScvU64,
				U64:  &val0,
			},
			want:    0,
			wantErr: false,
		},
		{
			name: "max uint64",
			input: xdr.ScVal{
				Type: xdr.ScValTypeScvU64,
				U64:  &valMax,
			},
			want:    18446744073709551615,
			wantErr: false,
		},
		{
			name: "wrong type - bool",
			input: xdr.ScVal{
				Type: xdr.ScValTypeScvBool,
			},
			wantErr:     true,
			errContains: "expected u64",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := scValToU64(tt.input)

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

func TestScValToAddress(t *testing.T) {
	// Create valid account address ScVal
	accountAddr := "GCUUFSK3P7YMFMYVA3L7LVQ7TCWJK7WUOFMXLX2HYSRCFB37ZGZD4CCX"
	accountID := xdr.MustAddress(accountAddr)
	accountScAddr, _ := xdr.NewScAddress(xdr.ScAddressTypeScAddressTypeAccount, accountID)

	// Create valid contract address ScVal
	contractAddr := "CA7HVINR2AY532D7UPOE7MMOZAN2ZBZ327VWIO33K7JM7C4G6XYX5NOT"
	contractBytes, _ := strkey.Decode(strkey.VersionByteContract, contractAddr)
	var contractId xdr.ContractId
	copy(contractId[:], contractBytes)
	contractScAddr, _ := xdr.NewScAddress(xdr.ScAddressTypeScAddressTypeContract, contractId)

	tests := []struct {
		name        string
		input       xdr.ScVal
		want        string
		wantErr     bool
		errContains string
	}{
		{
			name: "valid account address",
			input: xdr.ScVal{
				Type:    xdr.ScValTypeScvAddress,
				Address: &accountScAddr,
			},
			want:    accountAddr,
			wantErr: false,
		},
		{
			name: "valid contract address",
			input: xdr.ScVal{
				Type:    xdr.ScValTypeScvAddress,
				Address: &contractScAddr,
			},
			want:    contractAddr,
			wantErr: false,
		},
		{
			name: "wrong type - bool",
			input: xdr.ScVal{
				Type: xdr.ScValTypeScvBool,
			},
			wantErr:     true,
			errContains: "expected address",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := scValToAddress(tt.input)

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

// ============================================================================
// Contract Event Extraction Tests
// ============================================================================

// buildEventMetaXDR encodes a V3 transaction meta carrying a single contract
// event whose data is a map, the format #[contractevent] emits by default.
func buildEventMetaXDR(t *testing.T, contractID, eventName string, fields map[string]xdr.ScVal) string {
	t.Helper()

	contractBytes, err := strkey.Decode(strkey.VersionByteContract, contractID)
	require.NoError(t, err)
	var id xdr.ContractId
	copy(id[:], contractBytes)

	entries := make(xdr.ScMap, 0, len(fields))
	for name, val := range fields {
		sym := xdr.ScSymbol(name)
		entries = append(entries, xdr.ScMapEntry{
			Key: xdr.ScVal{Type: xdr.ScValTypeScvSymbol, Sym: &sym},
			Val: val,
		})
	}

	mapPtr := &entries
	topic := xdr.ScSymbol(eventName)
	event := xdr.ContractEvent{
		ContractId: &id,
		Type:       xdr.ContractEventTypeContract,
		Body: xdr.ContractEventBody{
			V: 0,
			V0: &xdr.ContractEventV0{
				Topics: []xdr.ScVal{{Type: xdr.ScValTypeScvSymbol, Sym: &topic}},
				Data:   xdr.ScVal{Type: xdr.ScValTypeScvMap, Map: &mapPtr},
			},
		},
	}

	meta := xdr.TransactionMeta{
		V: 3,
		V3: &xdr.TransactionMetaV3{
			SorobanMeta: &xdr.SorobanTransactionMeta{
				Events: []xdr.ContractEvent{event},
				// The zero ScVal is a bool with a nil pointer and will not encode.
				ReturnValue: xdr.ScVal{Type: xdr.ScValTypeScvVoid},
			},
		},
	}

	encoded, err := xdr.MarshalBase64(meta)
	require.NoError(t, err)
	return encoded
}

func addressScVal(t *testing.T, address string) xdr.ScVal {
	t.Helper()
	val, err := addressToScVal(address)
	require.NoError(t, err)
	return val
}

func TestExtractRepaidBorrower(t *testing.T) {
	keys := stellartesting.NewTestKeys()

	t.Run("repay_for carries the borrower", func(t *testing.T) {
		meta := buildEventMetaXDR(t, keys.ContractID, "repaid", map[string]xdr.ScVal{
			"treasury":       addressScVal(t, keys.TreasuryPublic),
			"borrower":       addressScVal(t, keys.UserPublic),
			"amount":         i128ToScVal(500000000),
			"total_borrowed": i128ToScVal(1000000000),
		})

		borrower, err := extractRepaidBorrower(meta, keys.ContractID)
		require.NoError(t, err)
		assert.Equal(t, keys.UserPublic, borrower)
	})

	t.Run("unattributed repay yields an empty borrower", func(t *testing.T) {
		// Option<Address>::None encodes as Void.
		meta := buildEventMetaXDR(t, keys.ContractID, "repaid", map[string]xdr.ScVal{
			"treasury":       addressScVal(t, keys.TreasuryPublic),
			"borrower":       {Type: xdr.ScValTypeScvVoid},
			"amount":         i128ToScVal(500000000),
			"total_borrowed": i128ToScVal(1000000000),
		})

		borrower, err := extractRepaidBorrower(meta, keys.ContractID)
		require.NoError(t, err)
		assert.Empty(t, borrower)
	})

	t.Run("event from a different contract is ignored", func(t *testing.T) {
		otherContract := "CDLZFC3SYJYDZT7K67VZ75HPJVIEUVNIXF47ZG2FB2RMQQVU2HHGCYSC"
		meta := buildEventMetaXDR(t, otherContract, "repaid", map[string]xdr.ScVal{
			"borrower": addressScVal(t, keys.UserPublic),
		})

		_, err := extractRepaidBorrower(meta, keys.ContractID)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "repaid event not found")
	})

	t.Run("no Repaid event", func(t *testing.T) {
		meta := buildEventMetaXDR(t, keys.ContractID, "borrowed", map[string]xdr.ScVal{
			"recipient": addressScVal(t, keys.UserPublic),
		})

		_, err := extractRepaidBorrower(meta, keys.ContractID)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "repaid event not found")
	})

	t.Run("undecodable metadata", func(t *testing.T) {
		_, err := extractRepaidBorrower("not-base64-xdr", keys.ContractID)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to decode result meta")
	})
}

func TestExtractBorrowedRecipient(t *testing.T) {
	keys := stellartesting.NewTestKeys()

	meta := buildEventMetaXDR(t, keys.ContractID, "borrowed", map[string]xdr.ScVal{
		"treasury":       addressScVal(t, keys.TreasuryPublic),
		"recipient":      addressScVal(t, keys.UserPublic),
		"amount":         i128ToScVal(1000000000),
		"total_borrowed": i128ToScVal(1000000000),
	})

	recipient, err := extractBorrowedRecipient(meta, keys.ContractID)
	require.NoError(t, err)
	assert.Equal(t, keys.UserPublic, recipient)
}

func TestExtractYieldBumpedTotalManaged(t *testing.T) {
	keys := stellartesting.NewTestKeys()

	t.Run("reads total_managed", func(t *testing.T) {
		meta := buildEventMetaXDR(t, keys.ContractID, "yield_bumped", map[string]xdr.ScVal{
			"from":          addressScVal(t, keys.TreasuryPublic),
			"amount":        i128ToScVal(250000000),
			"total_managed": i128ToScVal(9750000000),
		})

		total, err := extractYieldBumpedTotalManaged(meta, keys.ContractID)
		require.NoError(t, err)
		assert.Equal(t, int64(9750000000), total)
	})

	t.Run("no YieldBumped event", func(t *testing.T) {
		meta := buildEventMetaXDR(t, keys.ContractID, "repaid", map[string]xdr.ScVal{
			"amount": i128ToScVal(1),
		})

		_, err := extractYieldBumpedTotalManaged(meta, keys.ContractID)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "yield_bumped event not found")
	})
}
