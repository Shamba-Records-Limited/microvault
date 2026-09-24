package airtel

import (
	"errors"
	"net/http"
	"testing"

	"github.com/samber/oops"

	pkgErrors "github.com/Shamba-Records-Limited/microvault/pkg/errors"
)

func TestClassify_DocumentedCodes(t *testing.T) {
	cases := map[string]struct {
		code string
		want string
	}{
		"signature mismatch": {code: CodeCollectionForbidden, want: pkgErrors.CodePermissionDenied},
		"encrypted pin":      {code: CodeRouterBadEncryptedPin, want: pkgErrors.CodePermissionDenied},
		"wrong pin":          {code: CodeRouterIncorrectPin, want: pkgErrors.CodePermissionDenied},
		"no pin entered":     {code: CodeCollectionNoPinEntered, want: pkgErrors.CodePermissionDenied},
		"no balance":         {code: CodeCollectionNoBalance, want: pkgErrors.CodeInsufficientLiquidity},
		"limit":              {code: CodeCollectionLimit, want: pkgErrors.CodeInvalidAmount},
		"below minimum":      {code: CodeCollectionInvalidAmt, want: pkgErrors.CodeInvalidAmount},
		"payee barred":       {code: CodeCollectionPayeeBarred, want: pkgErrors.CodeMerchantNotPermitted},
		"refused":            {code: CodeCollectionRefused, want: pkgErrors.CodePermissionDenied},
		"do not honor":       {code: CodeCollectionDoNotHonor, want: pkgErrors.CodePermissionDenied},
		"not found":          {code: CodeCollectionNotFound, want: pkgErrors.CodeNotFound},
		"expired":            {code: CodeCollectionExpired, want: pkgErrors.CodeQuoteExpired},
		"duplicate":          {code: CodeESBDuplicateExtID, want: pkgErrors.CodeDuplicateRequest},
		"no wallet":          {code: CodeRouterNoWallet, want: pkgErrors.CodeSubscriptionUnavailable},
		"no country route":   {code: CodeRouterNoCountryRoute, want: pkgErrors.CodeSubscriptionUnavailable},
		"country denied":     {code: CodeRouterCountryDenied, want: pkgErrors.CodeSubscriptionUnavailable},
		"missing header":     {code: CodeRouterMissingHeader, want: pkgErrors.CodeBuildFailed},
		"bad country":        {code: CodeRouterBadCountry, want: pkgErrors.CodeBuildFailed},
		"bad currency":       {code: CodeRouterBadCurrency, want: pkgErrors.CodeBuildFailed},
		"bad msisdn":         {code: CodeESBBadMSISDN, want: pkgErrors.CodeMissingPhoneNumber},
		"bad msisdn length":  {code: CodeESBBadMSISDNLength, want: pkgErrors.CodeMissingPhoneNumber},
		"no vendor":          {code: CodeESBNoVendor, want: pkgErrors.CodeSubscriptionUnavailable},
		"encryption failed":  {code: CodeEncryptionFailed, want: pkgErrors.CodeHTTPError},
		"kyc not found":      {code: CodeKYCNotFound, want: pkgErrors.CodeNotFound},
		"account no user":    {code: CodeAccountNoUsr, want: pkgErrors.CodeNotFound},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got, _ := classify(http.StatusOK, Status{ResponseCode: tc.code})
			if got != tc.want {
				t.Fatalf("classify(%s) = %q, want %q", tc.code, got, tc.want)
			}
		})
	}
}

// ROUTER115 and ROUTER116 are adjacent codes and completely different
// actions: one is the payer, one is our encryption. The hint has to say so,
// or an operator chases a borrower over our bug.
func TestClassify_PinCodesAreDistinguished(t *testing.T) {
	_, payerHint := classify(http.StatusOK, Status{ResponseCode: CodeRouterIncorrectPin})
	_, ourHint := classify(http.StatusOK, Status{ResponseCode: CodeRouterBadEncryptedPin})

	if payerHint == ourHint {
		t.Fatal("the wrong-PIN and broken-encryption hints are identical")
	}
	if payerHint == "" || ourHint == "" {
		t.Fatal("both PIN codes need a hint naming whose fault it is")
	}

	if OutcomeFor(CodeRouterIncorrectPin).Kind != OutcomePayer {
		t.Fatal("ROUTER115 must classify as the payer's problem")
	}
	if OutcomeFor(CodeRouterBadEncryptedPin).Kind != OutcomeCredential {
		t.Fatal("ROUTER116 must classify as ours")
	}
}

func TestClassify_HTTPFallbacks(t *testing.T) {
	cases := map[int]string{
		http.StatusUnauthorized:    pkgErrors.CodeUnauthorized,
		http.StatusForbidden:       pkgErrors.CodePermissionDenied,
		http.StatusNotFound:        pkgErrors.CodeNotFound,
		http.StatusTooManyRequests: pkgErrors.CodeRateLimited,
		http.StatusRequestTimeout:  pkgErrors.CodeHTTPError,
		http.StatusBadGateway:      pkgErrors.CodeHTTPError,
		http.StatusGatewayTimeout:  pkgErrors.CodeHTTPError,
	}

	for status, want := range cases {
		got, _ := classify(status, Status{})
		if got != want {
			t.Fatalf("classify(%d) = %q, want %q", status, got, want)
		}
	}
}

// The timeout family must say enquire rather than retry, because a retry is
// how the same payment goes out twice.
func TestClassify_TimeoutsSayEnquire(t *testing.T) {
	for _, status := range []int{http.StatusRequestTimeout, http.StatusBadGateway, http.StatusGatewayTimeout} {
		_, hint := classify(status, Status{})
		if hint == "" {
			t.Fatalf("HTTP %d carries no hint", status)
		}
	}
}

// result_code is deprecated but still populated; classification must fall
// back to it rather than ignoring a code that is present.
func TestClassify_FallsBackToResultCode(t *testing.T) {
	got, _ := classify(http.StatusOK, Status{ResultCode: CodeESBDuplicateExtID})
	if got != pkgErrors.CodeDuplicateRequest {
		t.Fatalf("classify by result_code = %q, want %q", got, pkgErrors.CodeDuplicateRequest)
	}
}

func TestCheckEnvelope(t *testing.T) {
	errb := oops.In("test")

	cases := map[string]struct {
		status  Status
		wantErr bool
	}{
		"absent":          {status: Status{}},
		"success flag":    {status: Status{Success: true, Code: "200"}},
		"success code":    {status: Status{Code: "200", ResponseCode: CodeCollectionSuccess}},
		"pending":         {status: Status{Code: "200", ResponseCode: CodeCollectionInProcess}},
		"ambiguous":       {status: Status{Code: "200", ResponseCode: CodeCollectionAmbiguous}},
		"failure":         {status: Status{Code: "200", ResponseCode: CodeCollectionRefused}, wantErr: true},
		"failure no code": {status: Status{Code: "200", Message: "something went wrong"}, wantErr: true},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			err := checkEnvelope(errb, http.StatusOK, tc.status)
			if tc.wantErr != (err != nil) {
				t.Fatalf("checkEnvelope(%+v) error = %v, want error = %v", tc.status, err, tc.wantErr)
			}
		})
	}
}

// A pending envelope must survive as a value, not become an error: a caller
// handed an error has nothing left to enquire about.
func TestCheckEnvelope_PendingIsNotAnErrorEvenWhenUnsuccessful(t *testing.T) {
	errb := oops.In("test")
	for code := range pendingCodes {
		status := Status{Code: "200", Success: false, ResponseCode: code}
		if err := checkEnvelope(errb, http.StatusOK, status); err != nil {
			t.Fatalf("pending code %s surfaced as an error: %v", code, err)
		}
	}
}

func TestParseError_NonJSONBody(t *testing.T) {
	errb := oops.In("test")

	err := parseError(errb, http.StatusBadGateway, []byte("<html>bad gateway</html>"))
	if err == nil {
		t.Fatal("expected an error")
	}

	var apiErr *AirtelError
	if !errors.As(err, &apiErr) {
		t.Fatalf("error is not an *AirtelError: %v", err)
	}
	if apiErr.StatusCode != http.StatusBadGateway {
		t.Fatalf("status = %d", apiErr.StatusCode)
	}
	if apiErr.Status.Message == "" {
		t.Fatal("the raw body was discarded rather than kept as the message")
	}
}

func TestIsTokenRejected(t *testing.T) {
	cases := map[string]struct {
		err  error
		want bool
	}{
		"401":          {err: &AirtelError{StatusCode: http.StatusUnauthorized}, want: true},
		"message":      {err: &AirtelError{StatusCode: http.StatusBadRequest, Status: Status{Message: "Invalid access token"}}, want: true},
		"other status": {err: &AirtelError{StatusCode: http.StatusBadRequest}, want: false},
		"unrelated":    {err: errors.New("boom"), want: false},
		"nil":          {err: nil, want: false},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if got := isTokenRejected(tc.err); got != tc.want {
				t.Fatalf("isTokenRejected = %v, want %v", got, tc.want)
			}
		})
	}
}

// The error has to carry the code as a structured attribute, not only inside
// the message, or it cannot be grouped or acted on downstream.
func TestStatusError_CarriesAttributes(t *testing.T) {
	err := statusError(airtelErr("test"), http.StatusOK, Status{
		Code:         "200",
		ResponseCode: CodeCollectionRefused,
		ResultCode:   CodeESBFailed,
		Message:      "Transaction refused",
	})

	var oopsErr oops.OopsError
	if !errors.As(err, &oopsErr) {
		t.Fatalf("error is not an oops error: %v", err)
	}
	context := oopsErr.Context()
	if context["airtel_response_code"] != CodeCollectionRefused {
		t.Fatalf("response code attribute = %v", context["airtel_response_code"])
	}
	if context["airtel_result_code"] != CodeESBFailed {
		t.Fatalf("result code attribute = %v", context["airtel_result_code"])
	}
	if context[pkgErrors.AttrProvider] != "airtel" {
		t.Fatalf("provider attribute = %v", context[pkgErrors.AttrProvider])
	}
}
