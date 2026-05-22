package soroban

import (
	"fmt"
	"strings"

	"github.com/stellar/go-stellar-sdk/strkey"
	"github.com/stellar/go-stellar-sdk/xdr"
)

// ============================================================================
// Encoding Helpers (Go to Soroban)
// ============================================================================

// addressToScVal converts a Stellar address (G... or C...) to ScVal
func addressToScVal(address string) (xdr.ScVal, error) {
	if strkey.IsValidEd25519PublicKey(address) {
		accountID := xdr.MustAddress(address)
		scAddr, err := xdr.NewScAddress(xdr.ScAddressTypeScAddressTypeAccount, accountID)
		if err != nil {
			return xdr.ScVal{}, err
		}
		return xdr.ScVal{
			Type:    xdr.ScValTypeScvAddress,
			Address: &scAddr,
		}, nil
	}

	if strkey.IsValidContractAddress(address) {
		contractBytes, err := strkey.Decode(strkey.VersionByteContract, address)
		if err != nil {
			return xdr.ScVal{}, err
		}
		var contractId xdr.ContractId
		copy(contractId[:], contractBytes)
		scAddr, err := xdr.NewScAddress(xdr.ScAddressTypeScAddressTypeContract, contractId)
		if err != nil {
			return xdr.ScVal{}, err
		}
		return xdr.ScVal{
			Type:    xdr.ScValTypeScvAddress,
			Address: &scAddr,
		}, nil
	}

	return xdr.ScVal{}, fmt.Errorf("invalid address: %s", address)
}

// i128ToScVal converts an int64 to ScVal i128
func i128ToScVal(value int64) xdr.ScVal {
	var hi int64 = 0
	if value < 0 {
		hi = -1
	}

	return xdr.ScVal{
		Type: xdr.ScValTypeScvI128,
		I128: &xdr.Int128Parts{
			Hi: xdr.Int64(hi),
			Lo: xdr.Uint64(value),
		},
	}
}

// u64ToScVal converts uint64 to ScVal u64
func u64ToScVal(value uint64) xdr.ScVal {
	return xdr.ScVal{
		Type: xdr.ScValTypeScvU64,
		U64:  (*xdr.Uint64)(&value),
	}
}

// ============================================================================
// Decoding Helpers (Soroban to Go)
// ============================================================================

// scValToI128 extracts int64 from ScVal i128 result
func scValToI128(val xdr.ScVal) (int64, error) {
	if val.Type != xdr.ScValTypeScvI128 {
		return 0, fmt.Errorf("expected i128, got %v", val.Type)
	}
	return int64(val.I128.Lo), nil
}

// scValToBool extracts bool from ScVal
func scValToBool(val xdr.ScVal) (bool, error) {
	if val.Type != xdr.ScValTypeScvBool {
		return false, fmt.Errorf("expected bool, got %v", val.Type)
	}
	return bool(*val.B), nil
}

// scValToU64 extracts uint64 from ScVal
func scValToU64(val xdr.ScVal) (uint64, error) {
	if val.Type != xdr.ScValTypeScvU64 {
		return 0, fmt.Errorf("expected u64, got %v", val.Type)
	}
	return uint64(*val.U64), nil
}

// extractBorrowedRecipient parses the transaction result metadata XDR and
// extracts the recipient address from the Borrowed contract event.
// The Borrowed event has topics [symbol("Borrowed")] and data containing
// {treasury, recipient, amount, total_borrowed} as a struct/map.
// Supports both V3 (events in SorobanMeta) and V4 (events at top level) metadata.
func extractBorrowedRecipient(resultMetaXDR string, contractID string) (string, error) {
	var meta xdr.TransactionMeta
	if err := xdr.SafeUnmarshalBase64(resultMetaXDR, &meta); err != nil {
		return "", fmt.Errorf("failed to decode result meta: %w", err)
	}

	// Extract contract events from the appropriate metadata version.
	var events []xdr.ContractEvent
	switch meta.V {
	case 3:
		v3 := meta.MustV3()
		if v3.SorobanMeta == nil {
			return "", fmt.Errorf("no soroban metadata in V3 transaction")
		}
		events = v3.SorobanMeta.Events
	case 4:
		v4 := meta.MustV4()
		// Retrieve contract events from per-operation metadata
		for _, op := range v4.Operations {
			events = append(events, op.Events...)
		}
	default:
		return "", fmt.Errorf("unsupported transaction meta version: %d", meta.V)
	}

	// Decode the expected contract address for matching.
	contractBytes, err := strkey.Decode(strkey.VersionByteContract, contractID)
	if err != nil {
		return "", fmt.Errorf("invalid contract ID: %w", err)
	}
	var expectedContractID xdr.ContractId
	copy(expectedContractID[:], contractBytes)

	for _, event := range events {
		// Only match events from our vault contract.
		if event.ContractId == nil || *event.ContractId != expectedContractID {
			continue
		}
		if event.Type != xdr.ContractEventTypeContract {
			continue
		}

		// Check topic[0] == Symbol("borrowed") case-insensitively.
		topics := event.Body.MustV0().Topics
		if len(topics) == 0 {
			continue
		}
		if topics[0].Type != xdr.ScValTypeScvSymbol || !strings.EqualFold(string(*topics[0].Sym), "borrowed") {
			continue
		}

		// The event data is either a Vec (standard Soroban order) or a Map.
		data := event.Body.MustV0().Data
		if data.Type == xdr.ScValTypeScvVec {
			scVec := data.MustVec()
			if scVec != nil && len(*scVec) > 1 {
				return scValToAddress((*scVec)[1]) // Index 1 is "recipient" Address
			}
		} else if data.Type == xdr.ScValTypeScvMap {
			scMap := data.MustMap()
			if scMap != nil {
				for _, entry := range *scMap {
					if entry.Key.Type == xdr.ScValTypeScvSymbol && string(*entry.Key.Sym) == "recipient" {
						return scValToAddress(entry.Val)
					}
				}
			}
		}
	}

	return "", fmt.Errorf("Borrowed event not found in transaction metadata")
}

// scValToAddress extracts address string from ScVal
func scValToAddress(val xdr.ScVal) (string, error) {
	if val.Type != xdr.ScValTypeScvAddress {
		return "", fmt.Errorf("expected address, got %v", val.Type)
	}

	addr := val.Address
	switch addr.Type {
	case xdr.ScAddressTypeScAddressTypeAccount:
		return addr.AccountId.Address(), nil
	case xdr.ScAddressTypeScAddressTypeContract:
		return strkey.Encode(strkey.VersionByteContract, addr.ContractId[:])
	default:
		return "", fmt.Errorf("unknown address type: %v", addr.Type)
	}
}

// ExtractContractInfo parses the base64-encoded transaction envelope XDR
// and extracts the invoked contract ID and contract function name.
func ExtractContractInfo(envelopeXDR string) (contractID string, functionName string, err error) {
	var env xdr.TransactionEnvelope
	if err := xdr.SafeUnmarshalBase64(envelopeXDR, &env); err != nil {
		return "", "", fmt.Errorf("failed to decode envelope XDR: %w", err)
	}

	var tx xdr.Transaction
	switch env.Type {
	case xdr.EnvelopeTypeEnvelopeTypeTx:
		tx = env.V1.Tx
	case xdr.EnvelopeTypeEnvelopeTypeTxFeeBump:
		if env.FeeBump.Tx.InnerTx.Type == xdr.EnvelopeTypeEnvelopeTypeTx {
			tx = env.FeeBump.Tx.InnerTx.V1.Tx
		} else {
			return "", "", fmt.Errorf("unsupported inner transaction type")
		}
	default:
		return "", "", fmt.Errorf("unsupported envelope type: %v", env.Type)
	}

	for _, op := range tx.Operations {
		if op.Body.Type == xdr.OperationTypeInvokeHostFunction {
			hostFn := op.Body.InvokeHostFunctionOp.HostFunction
			if hostFn.Type == xdr.HostFunctionTypeHostFunctionTypeInvokeContract {
				invokeContract := hostFn.InvokeContract
				if invokeContract.ContractAddress.Type == xdr.ScAddressTypeScAddressTypeContract {
					contractIDBytes := *invokeContract.ContractAddress.ContractId
					contractID, err := strkey.Encode(strkey.VersionByteContract, contractIDBytes[:])
					if err != nil {
						return "", "", fmt.Errorf("failed to encode contract ID: %w", err)
					}
					return contractID, string(invokeContract.FunctionName), nil
				}
			}
		}
	}

	return "", "", fmt.Errorf("invoke contract host function not found in transaction")
}
