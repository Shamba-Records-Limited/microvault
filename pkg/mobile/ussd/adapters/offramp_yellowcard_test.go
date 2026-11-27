package adapters

import (
	"encoding/json"
	"testing"

	"github.com/Shamba-Records-Limited/microvault/pkg/payment/offramp"
	"github.com/Shamba-Records-Limited/microvault/pkg/payment/yellowcard"
)

// YellowCard requires channelType, country and currency at the top level of
// a send; channelId alone is no longer enough.
func TestBuildPaymentRequest_RequiredTopLevelFields(t *testing.T) {
	a := NewYellowCardOffRampAdapter(YellowCardOffRampConfig{BusinessID: "biz-1", BusinessName: "Microvault"})
	req := a.buildPaymentRequest(&disbursementParams{
		req: offramp.Request{
			LoanID: "loan-1", UserID: "user-1", RecipientName: "Jane Wanjiru",
			AmountUSD: 20, DestinationPhone: "+254711111111", CountryCode: "KE",
		},
		momoChannel:    yellowcard.Channel{ID: "ch-1", Currency: "KES", Country: "KE"},
		networkID:      "net-1",
		networkName:    "M-PESA",
		idempotencyKey: "loan-1_abc",
	})

	raw, err := json.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}
	var body map[string]any
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatal(err)
	}
	for field, want := range map[string]any{
		"channelType": yellowcard.ChannelTypeMomo,
		"country":     "KE",
		"currency":    "KES",
		"channelId":   "ch-1",
		"sequenceId":  "loan-1_abc",
	} {
		if body[field] != want {
			t.Errorf("%s = %v, want %v", field, body[field], want)
		}
	}
}
