package mobilesasa

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Shamba-Records-Limited/microvault/pkg/mobile/ussd"
)

func step(networkCode, serviceCode, text string) map[string]string {
	return map[string]string{
		"sessionId": "ATUid_8d2c31", "phoneNumber": "254712345678",
		"networkCode": networkCode, "serviceCode": serviceCode, "text": text,
	}
}

func TestParseRequestNormalisesTheStep(t *testing.T) {
	a := New()
	req, err := a.ParseRequest(t.Context(), step("1", "*657*45*1*2500#", "1*2500"))
	require.NoError(t, err)
	assert.Equal(t, "ATUid_8d2c31", req.SessionID)
	assert.Equal(t, "254712345678", req.PhoneNumber)
	assert.Equal(t, "2500", req.Input, "only the latest keypress")
	assert.Equal(t, "*657*45#", req.ServiceCode, "the dialled code without the spliced input")
	assert.Equal(t, "63902", req.NetworkCode)

	req, err = a.ParseRequest(t.Context(), step("2", "*657*45#", ""))
	require.NoError(t, err)
	assert.Empty(t, req.Input)
	assert.Equal(t, "63903", req.NetworkCode)

	req, err = a.ParseRequest(t.Context(), step("63902", "*657*45#", ""))
	require.NoError(t, err)
	assert.Equal(t, "63902", req.NetworkCode, "the test-callback payload already sends MCC+MNC")

	req, err = a.ParseRequest(t.Context(), step("9", "*657*45#", ""))
	require.NoError(t, err)
	assert.Equal(t, "9", req.NetworkCode, "an unknown code passes through")

	_, err = a.ParseRequest(t.Context(), map[string]string{"phoneNumber": "254712345678"})
	require.Error(t, err)
}

func TestValidateRequest(t *testing.T) {
	a := New()
	require.NoError(t, a.ValidateRequest(t.Context(), step("1", "*657*45#", "")))
	bad := step("1", "*657*45#", "")
	bad["phoneNumber"] = "12345"
	require.Error(t, a.ValidateRequest(t.Context(), bad))
	missing := step("1", "", "")
	require.Error(t, a.ValidateRequest(t.Context(), missing))
}

func TestFormatResponse(t *testing.T) {
	out, err := New().FormatResponse(t.Context(), &ussd.USSDResponse{Type: "END", Message: "Bye"})
	require.NoError(t, err)
	assert.Equal(t, "END Bye", out)
	_, err = New().FormatResponse(t.Context(), &ussd.USSDResponse{Type: "X"})
	require.Error(t, err)
	_, err = New().FormatResponse(t.Context(), nil)
	require.Error(t, err)
	assert.Equal(t, "mobilesasa", New().GetProviderName())
}
