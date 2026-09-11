package elliptic

import (
	"errors"
	"net/http"
	"testing"

	"github.com/samber/oops"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	pkgErrors "github.com/Shamba-Records-Limited/microvault/pkg/errors"
)

func TestClassifyStatus(t *testing.T) {
	cases := []struct {
		name         string
		statusCode   int
		wantNil      bool
		wantCode     string
		wantSentinel error
	}{
		{name: "200 is success", statusCode: http.StatusOK, wantNil: true},
		{name: "400 bad request maps to build failed", statusCode: http.StatusBadRequest, wantCode: pkgErrors.CodeBuildFailed},
		{name: "401 maps to unauthorized", statusCode: http.StatusUnauthorized, wantCode: pkgErrors.CodeUnauthorized},
		{name: "403 maps to merchant not permitted", statusCode: http.StatusForbidden, wantCode: pkgErrors.CodeMerchantNotPermitted},
		{
			name:         "404 is ErrNotInBlockchain, not a generic error",
			statusCode:   http.StatusNotFound,
			wantCode:     pkgErrors.CodeNotFound,
			wantSentinel: ErrNotInBlockchain,
		},
		{
			name:       "429 maps to http error",
			statusCode: http.StatusTooManyRequests,
			wantCode:   pkgErrors.CodeHTTPError,
		},
		{name: "500 maps to http error", statusCode: http.StatusInternalServerError, wantCode: pkgErrors.CodeHTTPError},
		{name: "unrecognized status maps to http error", statusCode: 418, wantCode: pkgErrors.CodeHTTPError},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := classifyStatus("test_op", tc.statusCode, []byte(`{"error":"detail"}`), http.Header{})

			if tc.wantNil {
				assert.NoError(t, err)
				return
			}
			is := assert.New(t)
			is.Error(err)

			var oopsErr oops.OopsError
			require.True(t, errors.As(err, &oopsErr), "classifyStatus must return an oops error")
			is.Equal(tc.wantCode, oopsErr.Code())

			if tc.wantSentinel != nil {
				is.True(errors.Is(err, tc.wantSentinel))
			}
		})
	}
}

func TestClassifyStatus_429CapturesRateLimitReset(t *testing.T) {
	header := http.Header{}
	header.Set("X-RateLimit-Reset", "1700000000")

	err := classifyStatus("test_op", http.StatusTooManyRequests, nil, header)
	require.Error(t, err)

	var oopsErr oops.OopsError
	require.True(t, errors.As(err, &oopsErr))
	assert.Equal(t, "1700000000", oopsErr.Context()["rate_limit_reset"])
}
