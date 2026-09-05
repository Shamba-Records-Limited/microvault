package mpesa

import (
	"errors"
	"testing"

	"github.com/samber/oops"

	pkgErrors "github.com/Shamba-Records-Limited/microvault/pkg/errors"
)

// The token retry in call depends on errors.As reaching through the oops
// wrapper. If oops ever stops unwrapping, every rejected token becomes a hard
// failure instead of a re-mint, so this is asserted rather than assumed.
func TestParseError_UnwrapsToDarajaError(t *testing.T) {
	err := parseError(mpesaErr("test"), 404, []byte(`{"requestId":"r-1","errorCode":"404.001.03","errorMessage":"Invalid Access Token"}`))

	var daraja *DarajaError
	if !errors.As(err, &daraja) {
		t.Fatalf("errors.As did not reach *DarajaError through the oops wrapper: %v", err)
	}
	if daraja.Code != "404.001.03" || daraja.RequestID != "r-1" || daraja.StatusCode != 404 {
		t.Errorf("DarajaError = %+v", daraja)
	}

	var oopsErr oops.OopsError
	if !errors.As(err, &oopsErr) {
		t.Fatal("error is not an oops error")
	}
	if oopsErr.Code() != pkgErrors.CodeUnauthorized {
		t.Errorf("code = %q, want %q", oopsErr.Code(), pkgErrors.CodeUnauthorized)
	}
	if oopsErr.Domain() != pkgErrors.DomainRepaymentCashIn {
		t.Errorf("domain = %q", oopsErr.Domain())
	}
}

// Daraja spells a rejected access token four different ways depending on which
// API rejected it. Matching only one leaves three APIs unable to recover.
func TestIsTokenRejected_AllFourSpellings(t *testing.T) {
	for _, code := range []string{"404.001.03", "400.003.01", "401.002.01", "401.001"} {
		err := parseError(mpesaErr("test"), 401, []byte(`{"errorCode":"`+code+`","errorMessage":"Invalid Access Token"}`))
		if !isTokenRejected(err) {
			t.Errorf("code %s not recognised as a rejected token", code)
		}
	}

	other := parseError(mpesaErr("test"), 400, []byte(`{"errorCode":"400.002.02","errorMessage":"Bad Request - Invalid Amount"}`))
	if isTokenRejected(other) {
		t.Error("a bad request was mistaken for a rejected token")
	}
	if isTokenRejected(nil) {
		t.Error("nil was mistaken for a rejected token")
	}
}

func TestClassify(t *testing.T) {
	cases := []struct {
		name    string
		status  int
		code    string
		message string
		want    string
	}{
		{"invalid token", 404, "404.001.03", "Invalid Access Token", pkgErrors.CodeUnauthorized},
		{"token by message only", 401, "", "Error Occurred - Invalid Access Token - foo", pkgErrors.CodeUnauthorized},
		{"resource not found", 404, "404.001.01", "Resource not found", pkgErrors.CodeNotFound},
		{"resource not found alt", 404, "404.002.01", "Resource not found", pkgErrors.CodeNotFound},
		{"resource not found alt2", 404, "404.003.01", "Resource not found", pkgErrors.CodeNotFound},
		{"auth header", 404, "404.001.04", "Invalid Authentication Header", pkgErrors.CodeUnauthorized},
		{"grant type", 400, "400.008.02", "Invalid grant type passed", pkgErrors.CodeUnauthorized},
		{"auth type", 400, "400.008.01", "Invalid authentication type passed", pkgErrors.CodeUnauthorized},
		{"bad request", 400, "400.002.02", "Bad Request - Invalid Amount", pkgErrors.CodeBuildFailed},
		{"bad request alt", 400, "400.003.02", "Bad Request", pkgErrors.CodeBuildFailed},
		{"invalid payload", 400, "400.002.05", "Invalid Request Payload", pkgErrors.CodeBuildFailed},
		{"method not allowed", 405, "405.001", "GET Method Not Allowed", pkgErrors.CodeBuildFailed},
		{"spike arrest", 500, "500.003.02", "Error Occurred: Spike Arrest Violation", pkgErrors.CodeHTTPError},
		{"quota", 500, "500.003.03", "Quota Violation", pkgErrors.CodeHTTPError},
		{"duplicate originator", 500, "500.002.1001", "Duplicate OriginatorConversationID", pkgErrors.CodeDuplicateRequest},
		{"urls already registered", 500, "500.003.1001", "Urls are already registered.", pkgErrors.CodeDuplicateRequest},
		{"duplicate notification", 500, "500.003.1001", "Duplicate notification info, SP ID is xxx", pkgErrors.CodeDuplicateRequest},
		{"internal server", 500, "500.003.1001", "Internal Server Error", pkgErrors.CodeHTTPError},
		{"unmapped falls back on status", 403, "", "forbidden", pkgErrors.CodeUnauthorized},
		{"unmapped 500", 500, "", "boom", pkgErrors.CodeHTTPError},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, _ := classify(tc.status, tc.code, tc.message)
			if got != tc.want {
				t.Errorf("classify(%d, %q, %q) = %q, want %q", tc.status, tc.code, tc.message, got, tc.want)
			}
		})
	}
}

// Safaricom overloads 500.001.1001 across three unrelated conditions that are
// actioned differently, so the code alone cannot classify it.
func TestClassify_STKInternalIsOverloaded(t *testing.T) {
	cases := map[string]string{
		"Unable to lock subscriber, a transaction is already in process for the current subscriber": pkgErrors.CodeDuplicateRequest,
		"Merchant does not exist":                    pkgErrors.CodeUnauthorized,
		"Wrong credentials":                          pkgErrors.CodeUnauthorized,
		"The Password parameter provided is invalid": pkgErrors.CodeUnauthorized,
		"Something Safaricom has not documented yet": pkgErrors.CodeHTTPError,
	}
	for message, want := range cases {
		got, _ := classify(500, errCodeSTKInternal, message)
		if got != want {
			t.Errorf("classify(500.001.1001, %q) = %q, want %q", message, got, want)
		}
	}
}

func TestParseError_NonJSONBody(t *testing.T) {
	err := parseError(mpesaErr("test"), 502, []byte("<html>bad gateway</html>"))

	var daraja *DarajaError
	if !errors.As(err, &daraja) {
		t.Fatal("expected a DarajaError")
	}
	if daraja.Message != "<html>bad gateway</html>" {
		t.Errorf("message = %q", daraja.Message)
	}
	if daraja.Code != "" {
		t.Errorf("code = %q, want empty", daraja.Code)
	}
}

func TestResultError_Unwraps(t *testing.T) {
	err := resultError(mpesaErr("test"), pkgErrors.CodeUnauthorized, &ResultError{
		ResultCode: 2001,
		ResultDesc: "The initiator information is invalid.",
	})

	var result *ResultError
	if !errors.As(err, &result) {
		t.Fatal("errors.As did not reach *ResultError")
	}
	if result.ResultCode != 2001 {
		t.Errorf("result code = %d", result.ResultCode)
	}
}
