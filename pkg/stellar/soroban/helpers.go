package soroban

import (
	"strings"

	"github.com/samber/lo"
	"github.com/samber/oops"

	pkgErrors "github.com/Shamba-Records-Limited/microvault/pkg/errors"
	"github.com/stellar/go-stellar-sdk/strkey"
	"github.com/stellar/go-stellar-sdk/xdr"

	"github.com/Shamba-Records-Limited/microvault/pkg/stellar/types"
)

// decodeErr starts an error builder for the XDR encode/decode helpers. These
// failures mean the ledger returned a shape we do not understand, which is a
// protocol-level fault rather than a contract rejection.
func decodeErr(op string) oops.OopsErrorBuilder {
	return oops.
		In(errDomain).
		Tags("soroban", "xdr").
		Code(pkgErrors.CodeDecodeFailed).
		With(pkgErrors.AttrOperation, op)
}

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

	// Address kept as an attribute rather than interpolated into the message,
	// so APM tools group every bad address under one error.
	return xdr.ScVal{}, oops.
		In(errDomain).
		Tags("soroban").
		Code(pkgErrors.CodeInvalidAddress).
		With(pkgErrors.AttrAddress, address).
		Wrapf(types.ErrInvalidStellarAddress, "address is neither an account nor a contract address")
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
		return 0, decodeErr("sc_val_i128").With("got_type", val.Type.String()).
			Errorf("expected i128")
	}
	return int64(val.I128.Lo), nil
}

// scValToBool extracts bool from ScVal
func scValToBool(val xdr.ScVal) (bool, error) {
	if val.Type != xdr.ScValTypeScvBool {
		return false, decodeErr("sc_val_bool").With("got_type", val.Type.String()).
			Errorf("expected bool")
	}
	return bool(*val.B), nil
}

// scValToU64 extracts uint64 from ScVal
func scValToU64(val xdr.ScVal) (uint64, error) {
	if val.Type != xdr.ScValTypeScvU64 {
		return 0, decodeErr("sc_val_u64").With("got_type", val.Type.String()).
			Errorf("expected u64")
	}
	return uint64(*val.U64), nil
}

// extractEventField parses the transaction result metadata XDR and returns the
// named field of a contract event emitted by the vault.
//
// Events declared with #[contractevent] publish the snake_cased struct name as
// topic[0] and their fields as data. The SDK's default data format is a map
// keyed by field name; vecIndex covers the vec format, where fields appear in
// declaration order. Supports both V3 (events in SorobanMeta) and V4 (events at
// top level) metadata.
func extractEventField(resultMetaXDR, contractID, eventName, fieldName string, vecIndex int) (xdr.ScVal, error) {
	errb := decodeErr("extract_event").With("event", eventName).With("field", fieldName)

	var meta xdr.TransactionMeta
	if err := xdr.SafeUnmarshalBase64(resultMetaXDR, &meta); err != nil {
		return xdr.ScVal{}, errb.Wrapf(err, "failed to decode result meta")
	}

	// Extract contract events from the appropriate metadata version.
	events := []xdr.ContractEvent{}
	switch meta.V {
	case 3:
		v3 := meta.MustV3()
		if v3.SorobanMeta == nil {
			return xdr.ScVal{}, errb.Errorf("no soroban metadata in V3 transaction")
		}
		events = v3.SorobanMeta.Events
	case 4:
		v4 := meta.MustV4()
		// V4 scatters contract events across per-operation metadata.
		events = lo.FlatMap(v4.Operations, func(op xdr.OperationMetaV2, _ int) []xdr.ContractEvent {
			return op.Events
		})
	default:
		return xdr.ScVal{}, errb.With("meta_version", meta.V).
			Errorf("unsupported transaction meta version")
	}

	// Decode the expected contract address for matching.
	contractBytes, err := strkey.Decode(strkey.VersionByteContract, contractID)
	if err != nil {
		return xdr.ScVal{}, errb.Code(pkgErrors.CodeInvalidAddress).
			With("contract_id", contractID).
			Wrapf(err, "invalid contract ID")
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

		// Check topic[0] == Symbol(eventName) case-insensitively.
		topics := event.Body.MustV0().Topics
		if len(topics) == 0 {
			continue
		}
		if topics[0].Type != xdr.ScValTypeScvSymbol || !strings.EqualFold(string(*topics[0].Sym), eventName) {
			continue
		}

		data := event.Body.MustV0().Data
		switch data.Type {
		case xdr.ScValTypeScvMap:
			scMap := data.MustMap()
			if scMap != nil {
				for _, entry := range *scMap {
					if entry.Key.Type == xdr.ScValTypeScvSymbol && string(*entry.Key.Sym) == fieldName {
						return entry.Val, nil
					}
				}
			}
		case xdr.ScValTypeScvVec:
			scVec := data.MustVec()
			if scVec != nil && len(*scVec) > vecIndex {
				return (*scVec)[vecIndex], nil
			}
		}
	}

	return xdr.ScVal{}, errb.Code(pkgErrors.CodeNotFound).
		Errorf("%s event not found in transaction metadata", eventName)
}

// extractBorrowedRecipient returns the recipient address from the Borrowed
// event. This serves as the on-chain memo linking the borrow to a child account.
func extractBorrowedRecipient(resultMetaXDR string, contractID string) (string, error) {
	val, err := extractEventField(resultMetaXDR, contractID, "borrowed", "recipient", 1)
	if err != nil {
		return "", err
	}
	return scValToAddress(val)
}

// extractRepaidBorrower returns the borrower address from the Repaid event.
// The field is an Option<Address>, so an unattributed repay yields the empty
// string rather than an error.
func extractRepaidBorrower(resultMetaXDR string, contractID string) (string, error) {
	val, err := extractEventField(resultMetaXDR, contractID, "repaid", "borrower", 1)
	if err != nil {
		return "", err
	}
	if val.Type == xdr.ScValTypeScvVoid {
		return "", nil
	}
	return scValToAddress(val)
}

// extractYieldBumpedTotalManaged returns the post-contribution managed assets
// from the YieldBumped event.
func extractYieldBumpedTotalManaged(resultMetaXDR string, contractID string) (int64, error) {
	val, err := extractEventField(resultMetaXDR, contractID, "yield_bumped", "total_managed", 2)
	if err != nil {
		return 0, err
	}
	return scValToI128(val)
}

// scValToAddress extracts address string from ScVal
func scValToAddress(val xdr.ScVal) (string, error) {
	if val.Type != xdr.ScValTypeScvAddress {
		return "", decodeErr("sc_val_address").With("got_type", val.Type.String()).
			Errorf("expected address")
	}

	addr := val.Address
	switch addr.Type {
	case xdr.ScAddressTypeScAddressTypeAccount:
		return addr.AccountId.Address(), nil
	case xdr.ScAddressTypeScAddressTypeContract:
		return strkey.Encode(strkey.VersionByteContract, addr.ContractId[:])
	default:
		return "", decodeErr("sc_val_address").With("got_type", addr.Type.String()).
			Errorf("unknown address type")
	}
}

// ExtractContractInfo parses the base64-encoded transaction envelope XDR
// and extracts the invoked contract ID and contract function name.
func ExtractContractInfo(envelopeXDR string) (contractID string, functionName string, err error) {
	errb := decodeErr("extract_contract_info")

	var env xdr.TransactionEnvelope
	if err := xdr.SafeUnmarshalBase64(envelopeXDR, &env); err != nil {
		return "", "", errb.Wrapf(err, "failed to decode envelope XDR")
	}

	var tx xdr.Transaction
	switch env.Type {
	case xdr.EnvelopeTypeEnvelopeTypeTx:
		tx = env.V1.Tx
	case xdr.EnvelopeTypeEnvelopeTypeTxFeeBump:
		if env.FeeBump.Tx.InnerTx.Type == xdr.EnvelopeTypeEnvelopeTypeTx {
			tx = env.FeeBump.Tx.InnerTx.V1.Tx
		} else {
			return "", "", errb.With("envelope_type", env.FeeBump.Tx.InnerTx.Type.String()).
				Errorf("unsupported inner transaction type")
		}
	default:
		return "", "", errb.With("envelope_type", env.Type.String()).
			Errorf("unsupported envelope type")
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
						return "", "", errb.Code(pkgErrors.CodeEncodeFailed).
							Wrapf(err, "failed to encode contract ID")
					}
					return contractID, string(invokeContract.FunctionName), nil
				}
			}
		}
	}

	return "", "", errb.Code(pkgErrors.CodeNotFound).
		Errorf("invoke contract host function not found in transaction")
}
