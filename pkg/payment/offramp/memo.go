package offramp

import (
	"encoding/base64"
	"errors"
	"strconv"

	"github.com/samber/oops"

	pkgErrors "github.com/Shamba-Records-Limited/microvault/pkg/errors"
)

// MemoType is a Stellar memo kind, spelled as a SEP-24 memo_type.
type MemoType string

const (
	MemoTypeText MemoType = "text"
	MemoTypeID   MemoType = "id"
	MemoTypeHash MemoType = "hash"
)

// ErrInvalidMemo is a memo whose value cannot be sent as its declared kind.
var ErrInvalidMemo = errors.New("invalid stellar memo")

// Memo is a Stellar payment memo and its kind. The zero value sends no memo.
type Memo struct {
	Value string
	Type  MemoType
}

// TextMemo is a text memo, or no memo when value is empty.
func TextMemo(value string) Memo {
	if value == "" {
		return Memo{}
	}
	return Memo{Value: value, Type: MemoTypeText}
}

// IsZero reports whether no memo is to be sent.
func (m Memo) IsZero() bool { return m.Value == "" }

// Validate reports whether the value can be sent as its kind.
func (m Memo) Validate() error {
	errb := func() oops.OopsErrorBuilder {
		return oops.In(pkgErrors.DomainOffRamp).Tags("memo").
			With("memo_type", m.Type).Code(pkgErrors.CodeInvalidMemo)
	}
	if m.IsZero() {
		if m.Type != "" {
			return errb().Wrapf(ErrInvalidMemo, "memo type is set but the memo has no value")
		}
		return nil
	}
	switch m.Type {
	case MemoTypeText:
		if len(m.Value) > 28 {
			return errb().With("memo_bytes", len(m.Value)).Wrapf(ErrInvalidMemo, "text memo exceeds 28 bytes")
		}
	case MemoTypeID:
		if _, err := strconv.ParseUint(m.Value, 10, 64); err != nil {
			return errb().With("memo", m.Value).Wrapf(ErrInvalidMemo, "id memo is not an unsigned 64-bit integer")
		}
	case MemoTypeHash:
		raw, err := base64.StdEncoding.DecodeString(m.Value)
		if err != nil || len(raw) != 32 {
			return errb().Wrapf(ErrInvalidMemo, "hash memo is not 32 base64-encoded bytes")
		}
	default:
		return errb().Wrapf(ErrInvalidMemo, "unsupported memo type")
	}
	return nil
}
