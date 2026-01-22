package soroban

import (
	"fmt"

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
