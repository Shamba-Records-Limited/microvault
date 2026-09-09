package mpesa

import "testing"

// A string result code like a reversal's R000002 must parse to a result with
// the code preserved verbatim, and classify — folding it into a number would
// read it as success 0, and erroring at parse would lose the result entirely.
func TestOutcome_StringResultCode(t *testing.T) {
	result, err := ParseResult(fixture(t, "reversal_stringcode_result.json"))
	if err != nil {
		t.Fatalf("ParseResult: %v", err)
	}
	if result.Succeeded() {
		t.Fatal("R000002 read as success")
	}
	if result.ResultCode != "R000002" {
		t.Errorf("result code = %q, want R000002", result.ResultCode)
	}

	outcome := result.Outcome(FamilyReversal)
	if outcome.Kind != OutcomeConfig || outcome.Retryable {
		t.Errorf("R000002 → %+v", outcome)
	}
}

func TestAsyncOutcomeFor(t *testing.T) {
	cases := []struct {
		family    ResultFamily
		code      string
		wantKind  OutcomeKind
		wantRetry bool
	}{
		{FamilyReversal, "0", OutcomeSuccess, false},
		{FamilyReversal, "1", OutcomeOperational, false},
		{FamilyReversal, "21", OutcomePermission, false},
		{FamilyReversal, "2001", OutcomeCredential, false},
		{FamilyReversal, "8006", OutcomeCredential, false},
		{FamilyReversal, "R000001", OutcomeOperational, false},
		{FamilyReversal, "R000002", OutcomeConfig, false},
		// Shared initiator set applies to every family.
		{FamilyStatus, "15", OutcomeConfig, false},
		{FamilyStatus, "18", OutcomeCredential, false},
		{FamilyStatus, "20", OutcomeCredential, false},
		{FamilyStatus, "22", OutcomePermission, false},
		{FamilyBalance, "26", OutcomeTransient, true},
		{FamilyBalance, "100000001", OutcomeTransient, true},
		{FamilyBalance, "00.002.1001", OutcomeTransient, true},
		// Family-specific overrides the shared set: 21 is permission both ways
		// but reversal documents it as the missing Org Reversals Initiator role.
		{FamilyReversal, "21", OutcomePermission, false},
		// Undocumented: never blindly retry.
		{FamilyStatus, "99999", OutcomeOperational, false},
	}
	for _, tc := range cases {
		got := AsyncOutcomeFor(tc.family, tc.code)
		if got.Kind != tc.wantKind || got.Retryable != tc.wantRetry {
			t.Errorf("AsyncOutcomeFor(%q, %q) = %+v, want kind %q retry %v",
				tc.family, tc.code, got, tc.wantKind, tc.wantRetry)
		}
		if got.Message == "" {
			t.Errorf("AsyncOutcomeFor(%q, %q) has no operator message", tc.family, tc.code)
		}
	}
}

// Success is "0" and nothing else — a string code that sorts before "0" must
// not slip into success.
func TestAsyncOutcomeFor_SuccessIsExact(t *testing.T) {
	if got := AsyncOutcomeFor(FamilyStatus, "0"); got.Kind != OutcomeSuccess {
		t.Errorf("0 = %+v", got)
	}
	for _, code := range []string{"1", "15", "R000002", "00.002.1001", "2001"} {
		if got := AsyncOutcomeFor(FamilyStatus, code); got.Kind == OutcomeSuccess {
			t.Errorf("%q read as success", code)
		}
	}
}
