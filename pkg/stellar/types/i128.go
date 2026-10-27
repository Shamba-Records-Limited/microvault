package types

import (
	"database/sql/driver"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
)

// I128 holds a Soroban i128 exactly, for values that outgrow int64 such as
// the WAD-scaled borrow index. The zero value is 0. Values are immutable.
type I128 struct {
	n *big.Int
}

var (
	minI128 = new(big.Int).Neg(new(big.Int).Lsh(big.NewInt(1), 127))
	maxI128 = new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 127), big.NewInt(1))

	// ErrI128OutOfRange is returned when a value does not fit in an i128.
	ErrI128OutOfRange = errors.New("value is outside the i128 range")
)

// NewI128 returns x as an I128.
func NewI128(x int64) I128 {
	return I128{n: big.NewInt(x)}
}

// I128FromParts assembles an i128 from its XDR hi and lo halves.
func I128FromParts(hi int64, lo uint64) I128 {
	n := new(big.Int).Lsh(big.NewInt(hi), 64)
	return I128{n: n.Or(n, new(big.Int).SetUint64(lo))}
}

// ParseI128 parses a base-10 integer.
func ParseI128(s string) (I128, error) {
	n, ok := new(big.Int).SetString(s, 10)
	if !ok {
		return I128{}, fmt.Errorf("parse i128 %q: not a base-10 integer", s)
	}
	if n.Cmp(minI128) < 0 || n.Cmp(maxI128) > 0 {
		return I128{}, fmt.Errorf("parse i128 %q: %w", s, ErrI128OutOfRange)
	}
	return I128{n: n}, nil
}

// Big returns a copy of the value.
func (i I128) Big() *big.Int {
	if i.n == nil {
		return new(big.Int)
	}
	return new(big.Int).Set(i.n)
}

// Sign returns -1, 0 or 1.
func (i I128) Sign() int {
	if i.n == nil {
		return 0
	}
	return i.n.Sign()
}

// Cmp compares i and o.
func (i I128) Cmp(o I128) int {
	return i.Big().Cmp(o.Big())
}

// String returns the base-10 form.
func (i I128) String() string {
	return i.Big().String()
}

// Value stores the value as a base-10 string, which Postgres numeric accepts.
func (i I128) Value() (driver.Value, error) {
	return i.String(), nil
}

// Scan reads a numeric, text or integer column.
func (i *I128) Scan(src any) error {
	switch v := src.(type) {
	case nil:
		*i = I128{}
		return nil
	case int64:
		*i = NewI128(v)
		return nil
	case []byte:
		return i.parseInto(string(v))
	case string:
		return i.parseInto(v)
	default:
		return fmt.Errorf("scan i128: unsupported type %T", src)
	}
}

func (i *I128) parseInto(s string) error {
	v, err := ParseI128(s)
	if err != nil {
		return err
	}
	*i = v
	return nil
}

// MarshalJSON writes a JSON string, so clients without 128-bit integers
// do not lose precision.
func (i I128) MarshalJSON() ([]byte, error) {
	return json.Marshal(i.String())
}

// UnmarshalJSON accepts a JSON string or number; null leaves the value zero.
func (i *I128) UnmarshalJSON(data []byte) error {
	if string(data) == "null" {
		return nil
	}
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		var num json.Number
		if err := json.Unmarshal(data, &num); err != nil {
			return fmt.Errorf("unmarshal i128: %w", err)
		}
		s = num.String()
	}
	return i.parseInto(s)
}

// DebtAtIndex returns principal × current / origin, the debt of a loan drawn
// at borrow index origin, rounded up or down. It is the per-loan share of the
// vault's TotalBorrowed.
func DebtAtIndex(principal int64, origin, current I128, roundUp bool) (int64, error) {
	if origin.Sign() <= 0 || current.Sign() <= 0 {
		return 0, fmt.Errorf("debt at index: indices must be positive (origin %s, current %s)", origin, current)
	}
	num := new(big.Int).Mul(big.NewInt(principal), current.Big())
	q, r := new(big.Int).QuoRem(num, origin.Big(), new(big.Int))
	if roundUp && r.Sign() > 0 {
		q.Add(q, big.NewInt(1))
	}
	if !q.IsInt64() {
		return 0, fmt.Errorf("debt at index: %s overflows int64", q)
	}
	return q.Int64(), nil
}
