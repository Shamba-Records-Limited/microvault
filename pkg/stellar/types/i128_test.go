package types

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestI128FromPartsRoundTripsBeyondInt64(t *testing.T) {
	v := I128FromParts(1, 5)
	assert.Equal(t, "18446744073709551621", v.String())

	neg := I128FromParts(-1, ^uint64(0))
	assert.Equal(t, "-1", neg.String())
}

func TestI128ScanAndValue(t *testing.T) {
	var v I128
	require.NoError(t, v.Scan([]byte("12345678901234567890123")))
	assert.Equal(t, "12345678901234567890123", v.String())

	stored, err := v.Value()
	require.NoError(t, err)
	assert.Equal(t, "12345678901234567890123", stored)

	require.NoError(t, v.Scan(int64(7)))
	assert.Equal(t, "7", v.String())

	require.NoError(t, v.Scan(nil))
	assert.Equal(t, 0, v.Sign())

	assert.Error(t, v.Scan([]byte("1.5")))
	assert.ErrorIs(t, v.Scan("170141183460469231731687303715884105728"), ErrI128OutOfRange)
}

func TestI128JSON(t *testing.T) {
	v, err := ParseI128("1000000000000000000000")
	require.NoError(t, err)

	out, err := json.Marshal(v)
	require.NoError(t, err)
	assert.JSONEq(t, `"1000000000000000000000"`, string(out))

	var fromString, fromNumber, fromNull I128
	require.NoError(t, json.Unmarshal([]byte(`"42"`), &fromString))
	require.NoError(t, json.Unmarshal([]byte(`42`), &fromNumber))
	assert.Equal(t, 0, fromString.Cmp(fromNumber))
	require.NoError(t, json.Unmarshal([]byte(`null`), &fromNull))
	assert.Equal(t, 0, fromNull.Sign())
}

func TestDebtAtIndex(t *testing.T) {
	origin, err := ParseI128("1000000000000000000")
	require.NoError(t, err)
	current, err := ParseI128("1080000000000000001")
	require.NoError(t, err)

	down, err := DebtAtIndex(1_000_000_000, origin, current, false)
	require.NoError(t, err)
	up, err := DebtAtIndex(1_000_000_000, origin, current, true)
	require.NoError(t, err)
	assert.Equal(t, int64(1_080_000_000), down)
	assert.Equal(t, int64(1_080_000_001), up)

	large, err := ParseI128("50000000000000000000")
	require.NoError(t, err)
	got, err := DebtAtIndex(1_000_000_000, origin, large, false)
	require.NoError(t, err)
	assert.Equal(t, int64(50_000_000_000), got)

	_, err = DebtAtIndex(1, I128{}, current, false)
	assert.Error(t, err)
}
