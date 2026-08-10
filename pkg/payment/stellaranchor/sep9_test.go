package stellaranchor

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSplitFullName(t *testing.T) {
	tests := []struct {
		name     string
		in       string
		wantFst  string
		wantLast string
	}{
		{"empty", "", "", ""},
		{"whitespace only", "   ", "", ""},
		{"single token", "Mwangi", "Mwangi", ""},
		{"two tokens", "Jane Doe", "Jane", "Doe"},
		{"three tokens — middle joins last", "Jane Mary Doe", "Jane", "Mary Doe"},
		{"leading whitespace trimmed", "  Jane Doe", "Jane", "Doe"},
		{"trailing whitespace trimmed", "Jane Doe  ", "Jane", "Doe"},
		{"tab separator", "Jane\tDoe", "Jane", "Doe"},
		{"hyphenated stays together", "Mary-Jane Doe", "Mary-Jane", "Doe"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			gotFst, gotLast := SplitFullName(tc.in)
			assert.Equal(t, tc.wantFst, gotFst)
			assert.Equal(t, tc.wantLast, gotLast)
		})
	}
}

func TestCustomer_OmitsEmptyFields(t *testing.T) {
	// We only serialise non-empty fields so MoneyGram's webview defaults are
	// preserved for fields we don't know.
	c := Customer{
		FirstName:          "Jane",
		MobileNumber:       "+254712345678",
		AddressCountryCode: "KEN",
	}

	b, err := json.Marshal(c)
	require.NoError(t, err)

	got := string(b)
	assert.Contains(t, got, `"first_name":"Jane"`)
	assert.Contains(t, got, `"mobile_number":"+254712345678"`)
	assert.Contains(t, got, `"address_country_code":"KEN"`)
	assert.NotContains(t, got, `"last_name"`)
	assert.NotContains(t, got, `"birth_date"`)
	assert.NotContains(t, got, `"city"`)
}
