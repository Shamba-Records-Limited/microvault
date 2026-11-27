package yellowcard

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"testing"

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
		"phone without plus":  {func(r *StellarReceive) { r.PayerPhone = "254711111111" }, pkgErrors.CodeInvalidPhoneNumber},
		"phone with letters":  {func(r *StellarReceive) { r.PayerPhone = "+2547ABC1111" }, pkgErrors.CodeInvalidPhoneNumber},
		"phone too short":     {func(r *StellarReceive) { r.PayerPhone = "+2547" }, pkgErrors.CodeInvalidPhoneNumber},
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

func TestSubmitStellarReceive_RejectionIsOnRamp(t *testing.T) {
	a, srv := newTestAdapter(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"code":"PaymentValidationError","message":"invalid"}`))
	})
	defer srv.Close()

	_, err := a.SubmitStellarReceive(context.Background(), validReceive())
	if err == nil {
		t.Fatal("expected an error")
	}
	oe, ok := oops.AsOops(err)
	if !ok {
		t.Fatalf("not an oops error: %v", err)
	}
	if oe.Domain() != pkgErrors.DomainOnRamp {
		t.Errorf("domain = %q, want %q", oe.Domain(), pkgErrors.DomainOnRamp)
	}
	if oe.Context()[pkgErrors.AttrSequenceID] != "seq-1" {
		t.Errorf("context = %v, want the sequence id", oe.Context())
	}
}
