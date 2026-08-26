package africastalking

import (
	"context"
	"testing"

	"github.com/samber/oops"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	pkgErrors "github.com/Shamba-Records-Limited/microvault/pkg/errors"
	"github.com/Shamba-Records-Limited/microvault/pkg/mobile/ussd"
)

func codeOf(t *testing.T, err error) string {
	t.Helper()
	var oopsErr oops.OopsError
	require.ErrorAs(t, err, &oopsErr)
	code, _ := oopsErr.Code().(string)
	return code
}

func contextOf(t *testing.T, err error) map[string]any {
	t.Helper()
	var oopsErr oops.OopsError
	require.ErrorAs(t, err, &oopsErr)
	return oopsErr.Context()
}

func TestParseRequest(t *testing.T) {
	a := NewAfricasTalkingUSSDAdapter("sandbox", "key")

	t.Run("takes only the last segment of the accumulated text", func(t *testing.T) {
		// The gateway resends the whole session as "1*2*3" on every turn.
		// Treating that as the input would feed the handler a menu path
		// rather than a keypress.
		req, err := a.ParseRequest(context.Background(), map[string]string{
			"sessionId":   "ATUid_1",
			"phoneNumber": "+254711222111",
			"text":        "1*2*3",
		})

		require.NoError(t, err)
		assert.Equal(t, "3", req.Input)
		assert.Equal(t, "254711222111", req.PhoneNumber, "the leading + is trimmed")
	})

	t.Run("empty text is a first turn", func(t *testing.T) {
		req, err := a.ParseRequest(context.Background(), map[string]string{
			"sessionId":   "ATUid_1",
			"phoneNumber": "254711222111",
		})

		require.NoError(t, err)
		assert.Empty(t, req.Input)
	})

	t.Run("missing sessionId", func(t *testing.T) {
		_, err := a.ParseRequest(context.Background(), map[string]string{
			"phoneNumber": "254711222111",
		})

		require.Error(t, err)
		assert.Equal(t, pkgErrors.CodeIncompleteResponse, codeOf(t, err))
		assert.Equal(t, "sessionId", contextOf(t, err)["field"])
	})

	t.Run("missing phoneNumber", func(t *testing.T) {
		_, err := a.ParseRequest(context.Background(), map[string]string{
			"sessionId": "ATUid_1",
		})

		require.Error(t, err)
		assert.Equal(t, pkgErrors.CodeMissingPhoneNumber, codeOf(t, err))
	})
}

func TestFormatResponse(t *testing.T) {
	a := NewAfricasTalkingUSSDAdapter("sandbox", "key")

	t.Run("renders the gateway prefix", func(t *testing.T) {
		out, err := a.FormatResponse(context.Background(), &ussd.USSDResponse{
			Type: "END", Message: "Thank you.",
		})

		require.NoError(t, err)
		assert.Equal(t, "END Thank you.", out)
	})

	t.Run("nil response", func(t *testing.T) {
		_, err := a.FormatResponse(context.Background(), nil)

		require.Error(t, err)
		assert.Equal(t, pkgErrors.CodeMissingDependency, codeOf(t, err))
	})

	t.Run("a type the gateway does not understand", func(t *testing.T) {
		_, err := a.FormatResponse(context.Background(), &ussd.USSDResponse{Type: "CONT"})

		require.Error(t, err)
		assert.Equal(t, pkgErrors.CodeIncompleteResponse, codeOf(t, err))
		assert.Equal(t, "CONT", contextOf(t, err)["response_type"])
	})
}

func TestValidateRequest(t *testing.T) {
	a := NewAfricasTalkingUSSDAdapter("sandbox", "key")

	valid := map[string]string{
		"sessionId":   "ATUid_1",
		"phoneNumber": "+254711222111",
		"serviceCode": "*384*52203#",
	}

	t.Run("accepts a well-formed request", func(t *testing.T) {
		require.NoError(t, a.ValidateRequest(context.Background(), valid))
	})

	t.Run("names the missing field", func(t *testing.T) {
		for _, field := range []string{"sessionId", "phoneNumber", "serviceCode"} {
			data := map[string]string{}
			for k, v := range valid {
				data[k] = v
			}
			data[field] = ""

			err := a.ValidateRequest(context.Background(), data)

			require.Error(t, err)
			assert.Equal(t, field, contextOf(t, err)["field"])
		}
	})

	t.Run("the phone number is redacted in the error", func(t *testing.T) {
		data := map[string]string{}
		for k, v := range valid {
			data[k] = v
		}
		data["phoneNumber"] = "0711222111"

		err := a.ValidateRequest(context.Background(), data)

		require.Error(t, err)
		assert.Equal(t, pkgErrors.CodeInvalidAddress, codeOf(t, err))
		assert.Equal(t, "071122***111", contextOf(t, err)["phone_number"],
			"a subscriber number must not reach an APM attribute in full")
	})
}
