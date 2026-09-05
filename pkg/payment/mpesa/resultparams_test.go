package mpesa

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return raw
}

// Every fixture is the literal envelope from Safaricom's documentation. If one
// stops parsing, the decoder has drifted from the wire rather than the other
// way round.
func TestParseResult_AllFixturesDecode(t *testing.T) {
	entries, err := os.ReadDir("testdata")
	if err != nil {
		t.Fatalf("read testdata: %v", err)
	}
	seen := 0
	for _, entry := range entries {
		if filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		seen++
		t.Run(entry.Name(), func(t *testing.T) {
			result, err := ParseResult(fixture(t, entry.Name()))
			if err != nil {
				t.Fatalf("ParseResult: %v", err)
			}
			if result.ConversationID == "" {
				t.Error("ConversationID did not decode")
			}
		})
	}
	if seen == 0 {
		t.Fatal("no fixtures found")
	}
}

// ReferenceItem is an array on a successful B2B result and an object on a
// failed one. The reference SDK declares it object-only and cannot decode the
// first of these at all.
func TestParseResult_ReferenceItemBothShapes(t *testing.T) {
	asArray, err := ParseResult(fixture(t, "b2b_success_result.json"))
	if err != nil {
		t.Fatalf("array form: %v", err)
	}
	if got, _ := asArray.Reference.Get("BillReferenceNumber"); got != "19008" {
		t.Errorf("array form BillReferenceNumber = %q", got)
	}
	if len(asArray.Reference) != 2 {
		t.Errorf("array form decoded %d reference items, want 2", len(asArray.Reference))
	}

	asObject, err := ParseResult(fixture(t, "b2b_failure_result.json"))
	if err != nil {
		t.Fatalf("object form: %v", err)
	}
	if got, ok := asObject.Reference.Get("QueueTimeoutURL"); !ok || got == "" {
		t.Errorf("object form QueueTimeoutURL = %q", got)
	}
}

// ResultParameter is an array on success and an object on a B2B failure. The
// reference SDK declares it array-only.
func TestParseResult_ResultParameterBothShapes(t *testing.T) {
	asArray, _ := ParseResult(fixture(t, "b2b_success_result.json"))
	if got, _ := asArray.Parameters.Get("Currency"); got != "KES" {
		t.Errorf("array form Currency = %q", got)
	}

	asObject, _ := ParseResult(fixture(t, "b2b_failure_result.json"))
	if got, ok := asObject.Parameters.Int("BOCompletedTime"); !ok || got != 20200120164825 {
		t.Errorf("object form BOCompletedTime = %d, ok %v", got, ok)
	}
}

// ResultCode is a string on the B2B success sample and a number on the failure
// sample. A plain int64 field fails exactly when something has gone wrong.
func TestParseResult_ResultCodeBothTypes(t *testing.T) {
	asString, _ := ParseResult(fixture(t, "b2b_success_result.json"))
	if asString.ResultCode != 0 || !asString.Succeeded() {
		t.Errorf("quoted result code = %d", asString.ResultCode)
	}

	asNumber, _ := ParseResult(fixture(t, "b2b_failure_result.json"))
	if asNumber.ResultCode != 2001 || asNumber.Succeeded() {
		t.Errorf("numeric result code = %d", asNumber.ResultCode)
	}
}

func TestFlexibleInt64(t *testing.T) {
	cases := map[string]int64{`0`: 0, `"0"`: 0, `2001`: 2001, `"2001"`: 2001, `""`: 0, `null`: 0, `"-1"`: -1}
	for raw, want := range cases {
		var got FlexibleInt64
		if err := json.Unmarshal([]byte(raw), &got); err != nil {
			t.Errorf("unmarshal %s: %v", raw, err)
			continue
		}
		if got.Int64() != want {
			t.Errorf("unmarshal %s = %d, want %d", raw, got.Int64(), want)
		}
	}
	var bad FlexibleInt64
	if err := json.Unmarshal([]byte(`"soon"`), &bad); err == nil {
		t.Error("expected an error for a non-numeric string")
	}
}

// One concept, three renderings across the endpoints in scope.
func TestParseTimestamp_AllDarajaLayouts(t *testing.T) {
	cases := map[string]string{
		"20221110110717":      "2022-11-10T11:07:17Z",
		"20191219102115":      "2019-12-19T10:21:15Z",
		"06.07.2024 22:48:52": "2024-07-06T22:48:52Z",
		"2024-07-06 22:48:52": "2024-07-06T22:48:52Z",
	}
	for value, want := range cases {
		parsed, ok := ParseTimestamp(value)
		if !ok {
			t.Errorf("ParseTimestamp(%q) failed", value)
			continue
		}
		if got := parsed.UTC().Format("2006-01-02T15:04:05Z"); got != want {
			t.Errorf("ParseTimestamp(%q) = %s, want %s", value, got, want)
		}
	}
	if _, ok := ParseTimestamp("not a time"); ok {
		t.Error("expected failure on unparseable input")
	}
	if _, ok := ParseTimestamp(""); ok {
		t.Error("expected failure on empty input")
	}
}

func TestParameters_TimeFromFixtures(t *testing.T) {
	b2b, _ := ParseResult(fixture(t, "b2b_success_result.json"))
	if _, ok := b2b.Parameters.Time("TransCompletedTime"); !ok {
		t.Error("B2B TransCompletedTime did not parse")
	}

	b2c, _ := ParseResult(fixture(t, "b2c_success_result.json"))
	if _, ok := b2c.Parameters.Time("TransactionCompletedDateTime"); !ok {
		t.Error("B2C TransactionCompletedDateTime did not parse")
	}
}

func TestParseBalances(t *testing.T) {
	single := ParseBalances("Working Account|KES|346568.83|6186.83|340382.00|0.00")
	if len(single) != 1 {
		t.Fatalf("single: got %d accounts", len(single))
	}
	if single[0].Name != "Working Account" || single[0].Currency != "KES" {
		t.Errorf("single: %+v", single[0])
	}
	if single[0].Available != 34_656_883 || single[0].Uncleared != 618_683 || single[0].Reserved != 34_038_200 {
		t.Errorf("single amounts: %+v", single[0])
	}

	multiple := ParseBalances("Working Account|KES|700000.00|700000.00|0.00|0.00&Utility Account|KES|228037.00|228037.00|0.00|0.00&Charges Paid Account|KES|-1540.00|-1540.00|0.00|0.00")
	if len(multiple) != 3 {
		t.Fatalf("multiple: got %d accounts", len(multiple))
	}
	if multiple[2].Name != "Charges Paid Account" || multiple[2].Available != -154_000 {
		t.Errorf("charges paid: %+v", multiple[2])
	}

	// Leniency: this arrives after the money has moved, so a shape Safaricom
	// changes must degrade rather than fail.
	if got := ParseBalances(""); got != nil {
		t.Errorf("empty input gave %+v", got)
	}
	if got := ParseBalances("Working Account|KES"); len(got) != 1 || got[0].Available != 0 {
		t.Errorf("truncated input gave %+v", got)
	}
	if got := ParseBalances("Working Account|KES|not-a-number"); len(got) != 1 || got[0].Available != 0 {
		t.Errorf("unparseable amount gave %+v", got)
	}
}

func TestParseBalances_FromFixture(t *testing.T) {
	result, _ := ParseResult(fixture(t, "balance_success_result.json"))
	balances, ok := result.Parameters.Balances("AccountBalance")
	if !ok || len(balances) != 3 {
		t.Fatalf("balances = %+v, ok %v", balances, ok)
	}
	if balances[1].Name != "Utility Account" || balances[1].Available != 22_803_700 {
		t.Errorf("utility = %+v", balances[1])
	}
}

func TestParseWrappedAmount(t *testing.T) {
	amount, ok := ParseWrappedAmount("{Amount={CurrencyCode=KES, MinimumAmount=618683, BasicAmount=6186.83}}")
	if !ok {
		t.Fatal("did not parse")
	}
	if amount.CurrencyCode != "KES" || amount.Minor != 618683 {
		t.Errorf("amount = %+v", amount)
	}

	if _, ok := ParseWrappedAmount(""); ok {
		t.Error("empty input parsed")
	}
	if _, ok := ParseWrappedAmount("garbage"); ok {
		t.Error("garbage parsed")
	}
}

func TestParseWrappedAmount_FromFixture(t *testing.T) {
	result, _ := ParseResult(fixture(t, "b2b_success_result.json"))
	amount, ok := result.Parameters.WrappedAmount("DebitAccountBalance")
	if !ok || amount.Minor != 618683 || amount.CurrencyCode != "KES" {
		t.Errorf("amount = %+v, ok %v", amount, ok)
	}
}

// Money never goes through a float.
func TestParseMinor(t *testing.T) {
	cases := map[string]int64{
		"0": 0, "1": 100, "6186.83": 618683, "6186.8": 618680, "0.05": 5,
		"-1540.00": -154000, "8959269.60": 895926960, ".5": 50, "190.00": 19000,
	}
	for value, want := range cases {
		got, err := parseMinor(value)
		if err != nil {
			t.Errorf("parseMinor(%q): %v", value, err)
			continue
		}
		if got != want {
			t.Errorf("parseMinor(%q) = %d, want %d", value, got, want)
		}
	}
	for _, bad := range []string{"", "abc", "1.2.3"} {
		if _, err := parseMinor(bad); err == nil {
			t.Errorf("parseMinor(%q) did not fail", bad)
		}
	}
}

func TestParameters_MissingKeys(t *testing.T) {
	result, _ := ParseResult(fixture(t, "b2b_success_result.json"))
	if _, ok := result.Parameters.Get("Nope"); ok {
		t.Error("Get found a missing key")
	}
	if _, ok := result.Parameters.Int("Nope"); ok {
		t.Error("Int found a missing key")
	}
	if _, ok := result.Parameters.Minor("Nope"); ok {
		t.Error("Minor found a missing key")
	}
	if _, ok := result.Parameters.Time("Nope"); ok {
		t.Error("Time found a missing key")
	}
	if _, ok := result.Parameters.Balances("Nope"); ok {
		t.Error("Balances found a missing key")
	}
	if _, ok := result.Parameters.WrappedAmount("Nope"); ok {
		t.Error("WrappedAmount found a missing key")
	}
	// An empty value is present but unusable, which is not the same as absent.
	if value, ok := result.Parameters.Get("DebitPartyCharges"); !ok || value != "" {
		t.Errorf("DebitPartyCharges = %q, ok %v", value, ok)
	}
}

func TestParseResult_Malformed(t *testing.T) {
	if _, err := ParseResult([]byte(`{not json`)); err == nil {
		t.Error("expected a decode error")
	}
	// A result with neither block present must decode to empty maps, not nil,
	// so callers can index without a guard.
	result, err := ParseResult([]byte(`{"Result":{"ResultCode":0,"ConversationID":"c"}}`))
	if err != nil {
		t.Fatalf("ParseResult: %v", err)
	}
	if result.Parameters == nil || result.Reference == nil {
		t.Error("absent blocks produced nil maps")
	}
}

// Daraja repeats keys, so parameters are a multi-map.
func TestParameters_RepeatedKeys(t *testing.T) {
	result, err := ParseResult([]byte(`{"Result":{"ConversationID":"c","ResultParameters":{"ResultParameter":[
		{"Key":"Account","Value":"one"},{"Key":"Account","Value":"two"}]}}}`))
	if err != nil {
		t.Fatalf("ParseResult: %v", err)
	}
	if got := result.Parameters.All("Account"); len(got) != 2 || got[0] != "one" || got[1] != "two" {
		t.Errorf("All(Account) = %v", got)
	}
	if first, _ := result.Parameters.Get("Account"); first != "one" {
		t.Errorf("Get returned %q, want the first value", first)
	}
}
