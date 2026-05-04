package moneygram

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestCountryISO3(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"KE", "KEN"},
		{"UG", "UGA"},
		{"NG", "NGA"},
		{"US", "USA"},
		{"GB", "GBR"},
		// fall-through: unknown alpha-2 returns "" so the field gets omitted
		{"XX", ""},
		{"", ""},
		// case-sensitivity is intentional — callers should normalise upstream
		{"ke", ""},
	}

	for _, tc := range tests {
		t.Run(tc.in, func(t *testing.T) {
			assert.Equal(t, tc.want, CountryISO3(tc.in))
		})
	}
}
