package soroban

import (
	"encoding/hex"
	"fmt"
	"math/big"

	"github.com/stellar/go-stellar-sdk/xdr"

	"github.com/Shamba-Records-Limited/microvault/pkg/stellar/types"
)

// ScAddress encodes a G… account or C… contract address.
func ScAddress(address string) (xdr.ScVal, error) { return addressToScVal(address) }

// ScI128 encodes an exact i128.
func ScI128(v types.I128) xdr.ScVal {
	n := new(big.Int).Set(v.Big())
	if n.Sign() < 0 {
		n.Add(n, new(big.Int).Lsh(big.NewInt(1), 128))
	}
	mask := new(big.Int).SetUint64(^uint64(0))
	lo := new(big.Int).And(n, mask).Uint64()
	hi := new(big.Int).Rsh(n, 64).Uint64()
	return xdr.ScVal{Type: xdr.ScValTypeScvI128, I128: &xdr.Int128Parts{Hi: xdr.Int64(int64(hi)), Lo: xdr.Uint64(lo)}}
}

// ScU32 encodes a u32.
func ScU32(v uint32) xdr.ScVal {
	u := xdr.Uint32(v)
	return xdr.ScVal{Type: xdr.ScValTypeScvU32, U32: &u}
}

// ScU64 encodes a u64.
func ScU64(v uint64) xdr.ScVal { return u64ToScVal(v) }

// ScBool encodes a bool.
func ScBool(b bool) xdr.ScVal {
	v := b
	return xdr.ScVal{Type: xdr.ScValTypeScvBool, B: &v}
}

// ScSymbol encodes a symbol.
func ScSymbol(s string) xdr.ScVal {
	sym := xdr.ScSymbol(s)
	return xdr.ScVal{Type: xdr.ScValTypeScvSymbol, Sym: &sym}
}

// ScBytes32 encodes a BytesN<32> from 64 hex characters.
func ScBytes32(hexValue string) (xdr.ScVal, error) {
	b, err := hex.DecodeString(hexValue)
	if err != nil || len(b) != 32 {
		return xdr.ScVal{}, fmt.Errorf("want 32 bytes as 64 hex characters, got %q", hexValue)
	}
	bytes := xdr.ScBytes(b)
	return xdr.ScVal{Type: xdr.ScValTypeScvBytes, Bytes: &bytes}, nil
}

// ScVec encodes a Vec<Val>.
func ScVec(vals ...xdr.ScVal) xdr.ScVal {
	vec := xdr.ScVec(vals)
	p := &vec
	return xdr.ScVal{Type: xdr.ScValTypeScvVec, Vec: &p}
}

// ScVoid encodes unit, also used for Option::None.
func ScVoid() xdr.ScVal { return xdr.ScVal{Type: xdr.ScValTypeScvVoid} }

// DecodeBool reads a bool.
func DecodeBool(v xdr.ScVal) (bool, error) { return scValToBool(v) }

// DecodeU32 reads a u32.
func DecodeU32(v xdr.ScVal) (uint32, error) {
	if v.Type != xdr.ScValTypeScvU32 || v.U32 == nil {
		return 0, fmt.Errorf("expected u32, got %s", v.Type)
	}
	return uint32(*v.U32), nil
}

// DecodeAddress reads an address, or "" for unit (Option::None).
func DecodeAddress(v xdr.ScVal) (string, error) {
	if v.Type == xdr.ScValTypeScvVoid {
		return "", nil
	}
	return scValToAddress(v)
}

// DecodeBytesHex reads bytes as lowercase hex.
func DecodeBytesHex(v xdr.ScVal) (string, error) {
	if v.Type != xdr.ScValTypeScvBytes || v.Bytes == nil {
		return "", fmt.Errorf("expected bytes, got %s", v.Type)
	}
	return hex.EncodeToString(*v.Bytes), nil
}

// DecodeI128 reads an exact i128, or nil for unit (Option::None).
func DecodeI128(v xdr.ScVal) (*types.I128, error) {
	if v.Type == xdr.ScValTypeScvVoid {
		return nil, nil //nolint:nilnil // None is a valid answer
	}
	i, err := scValToExactI128(v)
	if err != nil {
		return nil, err
	}
	return &i, nil
}

// DecodeEnum reads a unit enum variant, which Soroban encodes as a vec
// holding one symbol, and returns the variant name.
func DecodeEnum(v xdr.ScVal) (string, error) {
	if v.Type == xdr.ScValTypeScvSymbol && v.Sym != nil {
		return string(*v.Sym), nil
	}
	if v.Type != xdr.ScValTypeScvVec || v.Vec == nil || *v.Vec == nil || len(**v.Vec) == 0 {
		return "", fmt.Errorf("expected an enum variant, got %s", v.Type)
	}
	first := (**v.Vec)[0]
	if first.Type != xdr.ScValTypeScvSymbol || first.Sym == nil {
		return "", fmt.Errorf("expected an enum variant symbol, got %s", first.Type)
	}
	return string(*first.Sym), nil
}
