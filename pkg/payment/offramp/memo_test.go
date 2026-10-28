package offramp

import (
	"encoding/base64"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestMemo_Validate(t *testing.T) {
	hash := base64.StdEncoding.EncodeToString(make([]byte, 32))
	tests := []struct {
		name string
		memo Memo
		ok   bool
	}{
		{"no memo", Memo{}, true},
		{"text", Memo{Value: "abc", Type: MemoTypeText}, true},
		{"text at limit", Memo{Value: strings.Repeat("a", 28), Type: MemoTypeText}, true},
		{"text over limit", Memo{Value: strings.Repeat("a", 29), Type: MemoTypeText}, false},
		{"id", Memo{Value: "18446744073709551615", Type: MemoTypeID}, true},
		{"id overflows uint64", Memo{Value: "18446744073709551616", Type: MemoTypeID}, false},
		{"id not numeric", Memo{Value: "MG-1", Type: MemoTypeID}, false},
		{"hash", Memo{Value: hash, Type: MemoTypeHash}, true},
		{"hash wrong length", Memo{Value: base64.StdEncoding.EncodeToString([]byte("short")), Type: MemoTypeHash}, false},
		{"value without type", Memo{Value: "4242"}, false},
		{"type without value", Memo{Type: MemoTypeID}, false},
		{"unsupported type", Memo{Value: "4242", Type: "return"}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.memo.Validate()
			if tt.ok {
				assert.NoError(t, err)
			} else {
				assert.ErrorIs(t, err, ErrInvalidMemo)
			}
		})
	}
}

func TestTextMemo(t *testing.T) {
	assert.True(t, TextMemo("").IsZero())
	assert.Equal(t, Memo{Value: "m", Type: MemoTypeText}, TextMemo("m"))
}
