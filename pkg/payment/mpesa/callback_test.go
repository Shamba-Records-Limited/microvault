package mpesa

import "testing"

// A queue timeout is never a failure, whatever its body reports. This is the
// single rule that stops a retry from disbursing twice.
func TestParseCallback_TimeoutIsAlwaysUnknown(t *testing.T) {
	for _, name := range []string{"b2b_success_result.json", "b2b_failure_result.json", "b2c_success_result.json"} {
		callback, err := ParseCallback(CallbackTimeout, fixture(t, name))
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if callback.Outcome != OutcomeUnknown {
			t.Errorf("%s: timeout classified as %q, want unknown", name, callback.Outcome)
		}
		if callback.Outcome.Terminal() {
			t.Errorf("%s: an unknown outcome reported itself terminal", name)
		}
	}
}

func TestParseCallback_ResultClassification(t *testing.T) {
	success, err := ParseCallback(CallbackResult, fixture(t, "b2b_success_result.json"))
	if err != nil {
		t.Fatalf("success: %v", err)
	}
	if success.Outcome != OutcomeSucceeded || !success.Outcome.Terminal() {
		t.Errorf("success classified as %q", success.Outcome)
	}

	failure, err := ParseCallback(CallbackResult, fixture(t, "b2b_failure_result.json"))
	if err != nil {
		t.Fatalf("failure: %v", err)
	}
	if failure.Outcome != OutcomeFailed || !failure.Outcome.Terminal() {
		t.Errorf("failure classified as %q", failure.Outcome)
	}
	if failure.Result.ResultCode != 2001 {
		t.Errorf("result code = %d", failure.Result.ResultCode)
	}
}

// The two deliveries carry the same envelope, so a caller that sniffed the body
// instead of trusting the route would classify both identically.
func TestParseCallback_SameBodyDiffersByKind(t *testing.T) {
	raw := fixture(t, "b2b_failure_result.json")

	asResult, _ := ParseCallback(CallbackResult, raw)
	asTimeout, _ := ParseCallback(CallbackTimeout, raw)

	if asResult.Outcome == asTimeout.Outcome {
		t.Fatal("the same body classified identically under both kinds")
	}
	if asResult.Outcome != OutcomeFailed || asTimeout.Outcome != OutcomeUnknown {
		t.Errorf("result = %q, timeout = %q", asResult.Outcome, asTimeout.Outcome)
	}
}

func TestParseCallback_Malformed(t *testing.T) {
	if _, err := ParseCallback(CallbackResult, []byte(`{not json`)); err == nil {
		t.Error("expected a decode error")
	}
}
