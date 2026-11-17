package sms

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseFormReport(t *testing.T) {
	r, err := ParseFormReport(nil, []byte("id=ATXid_1&status=Failed&phoneNumber=%2B254712345678&networkCode=63902&failureReason=UserInBlacklist"))
	require.NoError(t, err)
	assert.Equal(t, DeliveryReport{ID: "ATXid_1", Status: "Failed", PhoneNumber: "+254712345678", NetworkCode: "63902", FailureReason: "UserInBlacklist"}, r)

	_, err = ParseFormReport(nil, []byte("status=Success"))
	require.Error(t, err)
}
