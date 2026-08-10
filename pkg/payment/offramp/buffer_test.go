package offramp

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestRateBuffer_Apply(t *testing.T) {
	tests := []struct {
		name            string
		rate, buf, want float64
	}{
		{"no buffer passes through", 100, 0, 100},
		{"one percent", 100, 0.01, 99},
		{"five percent", 100, 0.05, 95},
		{"zero rate stays zero", 0, 0.5, 0},
		{"over-unity buffer clamps at zero", 100, 1.5, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := NewRateBuffer(Fraction(tt.buf), 0.99)
			assert.InDelta(t, tt.want, b.Apply(tt.rate), 0.0001)
		})
	}
}

// Nil means "not configured" and takes the default; an explicit zero means no
// margin. Collapsing the two would make a deliberate opt-out unexpressible.
func TestNewRateBuffer_NilVersusZero(t *testing.T) {
	assert.Equal(t, 0.02, NewRateBuffer(nil, 0.02).Pct())
	assert.Equal(t, 0.0, NewRateBuffer(Fraction(0), 0.02).Pct())
	assert.Equal(t, 0.05, NewRateBuffer(Fraction(0.05), 0.02).Pct())
}
