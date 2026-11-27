package yellowcard

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/samber/oops"

	pkgErrors "github.com/Shamba-Records-Limited/microvault/pkg/errors"
)

const testTreasury = "GBBD47IF6LWK7P7MDEVSCWR7DPUWV3NY3DTQEVFL4NAT4AQH3ZLLFLA5"

func fullPayer() Party {
	return Party{
		Name: "Jane Wanjiru", Country: "KE", Phone: "+254711111111", Email: "jane@example.com",
		Address: "Nairobi", DOB: "02/01/1990", IDType: "NIN", IDNumber: "12345678",
	}
}

func validReceive() StellarReceive {
	return StellarReceive{
		SequenceID: "seq-1", CustomerUID: "user-1", ChannelID: "ch-1", NetworkID: "net-1",
		PayerPhone: "+254711111111", Payer: fullPayer(), LocalAmount: 2500,
		Currency: CurrencyKES, Country: CountryKenya, Destination: testTreasury, Memo: "2802699348",
	}
}

func codeOf(err error) string {
	if oe, ok := oops.AsOops(err); ok {
		code, _ := oe.Code().(string)
		return code
	}
	return ""
}

func TestStellarReceive_Request(t *testing.T) {
	req, err := validReceive().Request()
	if err != nil {
		t.Fatalf("Request: %v", err)
	}
	if !req.DirectSettlement || req.SettlementInfo == nil {
		t.Fatalf("direct settlement not set: %+v", req)
	}
	si := req.SettlementInfo
	if si.WalletAddress != testTreasury || si.WalletTag != "2802699348" ||
		si.CryptoCurrency != CryptoCurrencyUSDC || si.CryptoNetwork != CryptoNetworkXLM {
		t.Errorf("settlementInfo = %+v", si)
	}
	if req.CustomerType != CustomerTypeRetail || req.ChannelType != ChannelTypeMomo ||
		req.Source.AccountType != ChannelTypeMomo || req.Source.AccountNumber != "+254711111111" {
		t.Errorf("request = %+v", req)
	}
	if req.Reason != "other" || req.Amount != 0 || req.LocalAmount != 2500 {
		t.Errorf("amounts/reason = %v %v %q", req.Amount, req.LocalAmount, req.Reason)
	}
}

func TestStellarReceive_RequestRejects(t *testing.T) {
	cases := map[string]struct {
		mutate func(*StellarReceive)
		code   string
	}{
		"missing email":       {func(r *StellarReceive) { r.Payer.Email = "" }, pkgErrors.CodeMissingPayerKYC},
		"missing dob":         {func(r *StellarReceive) { r.Payer.DOB = " " }, pkgErrors.CodeMissingPayerKYC},
		"bad destination":     {func(r *StellarReceive) { r.Destination = "GABC" }, pkgErrors.CodeInvalidAddress},
		"text memo":           {func(r *StellarReceive) { r.Memo = "loan-1" }, pkgErrors.CodeInvalidMemo},
		"zero amount":         {func(r *StellarReceive) { r.LocalAmount = 0 }, pkgErrors.CodeInvalidAmount},
		"no phone":            {func(r *StellarReceive) { r.PayerPhone = "" }, pkgErrors.CodeMissingPhoneNumber},
		"no network":          {func(r *StellarReceive) { r.NetworkID = "" }, pkgErrors.CodeMissingDependency},
		"no currency":         {func(r *StellarReceive) { r.Currency = "" }, pkgErrors.CodeMissingDependency},
		"missing customer id": {func(r *StellarReceive) { r.CustomerUID = "" }, pkgErrors.CodeMissingDependency},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			r := validReceive()
			tc.mutate(&r)
			_, err := r.Request()
			if got := codeOf(err); got != tc.code {
				t.Errorf("code = %q, want %q (err %v)", got, tc.code, err)
			}
		})
	}
}

func TestMissingRetailFields(t *testing.T) {
	got := MissingRetailFields(Party{Name: "A B", Country: "KE"})
	want := []string{"phone", "email", "address", "dob", "idType", "idNumber"}
	if len(got) != len(want) {
		t.Fatalf("missing = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("missing[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestSubmitStellarReceive_Wire(t *testing.T) {
	var gotBody map[string]any
	a, srv := newTestAdapter(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &gotBody)
		w.Write([]byte(`{"id":"col-1","status":"processing","directSettlement":true,
			"settlementInfo":{"walletAddress":"` + testTreasury + `","cryptoCurrency":"USDC","cryptoNetwork":"XLM","cryptoAmount":19.2}}`))
	})
	defer srv.Close()

	got, err := a.SubmitStellarReceive(context.Background(), validReceive())
	if err != nil {
		t.Fatalf("SubmitStellarReceive: %v", err)
	}
	if !got.DirectSettlement || got.SettlementInfo == nil || got.SettlementInfo.CryptoAmount != 19.2 {
		t.Errorf("collection = %+v", got)
	}
	si, _ := gotBody["settlementInfo"].(map[string]any)
	if gotBody["directSettlement"] != true || si["walletTag"] != "2802699348" || si["cryptoNetwork"] != "XLM" {
		t.Errorf("body = %v", gotBody)
	}
	if gotBody["forceAccept"] != true {
		t.Errorf("forceAccept = %v", gotBody["forceAccept"])
	}
}

func TestSubmitStellarReceive_ValidatesBeforeCalling(t *testing.T) {
	called := false
	a, srv := newTestAdapter(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.Write([]byte(`{}`))
	})
	defer srv.Close()

	r := validReceive()
	r.Payer.Email = ""
	if _, err := a.SubmitStellarReceive(context.Background(), r); err == nil {
		t.Fatal("expected an error")
	}
	if called {
		t.Error("YellowCard was called with incomplete payer kyc")
	}
}

func TestWebhookEvent_ExecutedAtForms(t *testing.T) {
	want := time.Date(2023, 2, 20, 14, 25, 30, 459000000, time.UTC)
	cases := map[string]struct {
		raw  string
		want time.Time
	}{
		"documented iso string": {`"2023-02-20T14:25:30.459Z"`, want},
		"unix seconds":          {`1735689600`, time.Unix(1735689600, 0).UTC()},
		"seconds as string":     {`"1735689600"`, time.Unix(1735689600, 0).UTC()},
		"null":                  {`null`, time.Time{}},
		"unreadable string":     {`"yesterday"`, time.Time{}},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			body := `{"id":"p-1","sequenceId":"s-1","status":"complete","event":"SEND.COMPLETE","executedAt":` + tc.raw + `}`
			var e WebhookEvent
			if err := json.Unmarshal([]byte(body), &e); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			if !e.ExecutedAt.Equal(tc.want) {
				t.Errorf("executedAt = %v, want %v", e.ExecutedAt.Time, tc.want)
			}
			if e.Event != EventSendComplete || e.PaymentID != "p-1" {
				t.Errorf("event = %+v", e)
			}
		})
	}
}

// The payload exactly as YellowCard documents it must decode.
func TestWebhookEvent_DocumentedPayload(t *testing.T) {
	body := `{
	  "id": "00e97bc4-1429-4ce7-acb5-841f9d9ed059",
	  "sequenceId": "1a051f68-5c55-44c8-b4a0-366800daaf19",
	  "status": "failed",
	  "apiKey": "513fc4c3aaeb2a8f292a740ea178d830",
	  "event": "RECEIVE.FAILED",
	  "errorCode": "REFUSED",
	  "sessionId":"a4cf97cd-8f1a-4e38-842b-e9597affb199",
	  "executedAt": "2023-02-20T14:25:30.459Z"
	}`
	var e WebhookEvent
	if err := json.Unmarshal([]byte(body), &e); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if e.Event != EventReceiveFailed || e.ErrorCode != "REFUSED" || e.ExecutedAt.IsZero() {
		t.Errorf("event = %+v", e)
	}
}

func TestEventTime_MarshalRoundTrip(t *testing.T) {
	in := EventTime{time.Date(2026, 11, 27, 9, 0, 0, 0, time.UTC)}
	raw, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	var out EventTime
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	if !out.Equal(in.Time) {
		t.Errorf("round trip = %v, want %v", out.Time, in.Time)
	}
	if raw, _ := json.Marshal(EventTime{}); string(raw) != "null" {
		t.Errorf("zero = %s, want null", raw)
	}
}
