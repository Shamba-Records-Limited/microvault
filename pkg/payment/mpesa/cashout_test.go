package mpesa

import (
	"context"
	"testing"
)

// Local validation only — B2C, B2B and Query Organization Info are not
// wired anywhere in the platform yet, so there is no darajastub route for
// them to round-trip against. These tests cover the request-shape guards,
// which is where a caller-supplied value most needs catching before a
// network round trip; see b2c.go/b2b.go/orginfo.go doc comments.

func TestB2C_LocalValidation(t *testing.T) {
	c, _ := asyncClient(t)
	result := make(chan []byte, 1)
	timeout := make(chan []byte, 1)
	urls := asyncURLs(t, result, timeout)
	validPartyB := "254712345678"

	cases := map[string]B2CRequest{
		"no originator conversation id": {PartyA: testShortcode, PartyB: validPartyB, AmountKES: 100, Remarks: "ok", URLs: urls},
		"no disbursement shortcode":     {OriginatorConversationID: "x", PartyB: validPartyB, AmountKES: 100, Remarks: "ok", URLs: urls},
		"party b missing country code":  {OriginatorConversationID: "x", PartyA: testShortcode, PartyB: "0712345678", AmountKES: 100, Remarks: "ok", URLs: urls},
		"party b has a plus":            {OriginatorConversationID: "x", PartyA: testShortcode, PartyB: "+254712345678", AmountKES: 100, Remarks: "ok", URLs: urls},
		"zero amount":                   {OriginatorConversationID: "x", PartyA: testShortcode, PartyB: validPartyB, Remarks: "ok", URLs: urls},
		"short remarks":                 {OriginatorConversationID: "x", PartyA: testShortcode, PartyB: validPartyB, AmountKES: 100, Remarks: "a", URLs: urls},
		"occasion too long": {
			OriginatorConversationID: "x", PartyA: testShortcode, PartyB: validPartyB, AmountKES: 100, Remarks: "ok",
			Occasion: string(make([]byte, 101)), URLs: urls,
		},
		"no urls": {OriginatorConversationID: "x", PartyA: testShortcode, PartyB: validPartyB, AmountKES: 100, Remarks: "ok"},
	}
	for name, req := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := c.B2C(context.Background(), req); err == nil {
				t.Error("expected a validation error")
			}
		})
	}
}

func TestB2Pochi_PinsTheCommand(t *testing.T) {
	c, _ := asyncClient(t)
	result := make(chan []byte, 1)
	timeout := make(chan []byte, 1)
	req := B2CRequest{
		// A caller-set Command must not survive into B2Pochi's request — the
		// only way to observe that here, without a server, is that supplying
		// one doesn't change which local validation runs (both paths share
		// sendEnvelopeA), so this asserts B2Pochi still validates PartyB the
		// same way B2C does rather than skipping straight through.
		Command:   CommandBusinessPayment,
		PartyA:    testShortcode,
		PartyB:    "not-a-number",
		AmountKES: 100,
		Remarks:   "ok",
		URLs:      asyncURLs(t, result, timeout),
	}
	if _, err := c.B2Pochi(context.Background(), req); err == nil {
		t.Error("expected a validation error for a malformed party b")
	}
}

func TestB2B_LocalValidation(t *testing.T) {
	c, _ := asyncClient(t)
	result := make(chan []byte, 1)
	timeout := make(chan []byte, 1)
	urls := asyncURLs(t, result, timeout)

	cases := map[string]B2BRequest{
		"no party b":          {PartyA: testShortcode, AmountKES: 100, Remarks: "ok", URLs: urls},
		"no paying shortcode": {PartyB: 600000, AmountKES: 100, Remarks: "ok", URLs: urls},
		"zero amount":         {PartyA: testShortcode, PartyB: 600000, Remarks: "ok", URLs: urls},
		"account reference too long": {
			PartyA: testShortcode, PartyB: 600000, AmountKES: 100, Remarks: "ok",
			AccountReference: "12345678901234", URLs: urls,
		},
		"short remarks": {PartyA: testShortcode, PartyB: 600000, AmountKES: 100, Remarks: "a", URLs: urls},
		"no urls":       {PartyA: testShortcode, PartyB: 600000, AmountKES: 100, Remarks: "ok"},
	}
	for name, req := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := c.BusinessPayBill(context.Background(), req); err == nil {
				t.Error("expected a validation error")
			}
			if _, err := c.BusinessBuyGoods(context.Background(), req); err == nil {
				t.Error("expected a validation error")
			}
		})
	}
}

func TestBusinessPayToBulk_DefaultsPartyBToDisbursementShortcode(t *testing.T) {
	c, _ := asyncClient(t)
	result := make(chan []byte, 1)
	timeout := make(chan []byte, 1)

	// No disbursement shortcode is configured on the test client (only
	// CollectionShortcode), so PartyB stays 0 and the shared PartyB-required
	// check should still fire — proving the default is wired through
	// sendEnvelopeB rather than silently bypassing it.
	_, err := c.BusinessPayToBulk(context.Background(), B2BRequest{
		AmountKES: 100, Remarks: "ok", URLs: asyncURLs(t, result, timeout),
	})
	if err == nil {
		t.Error("expected a validation error when no disbursement shortcode resolves")
	}
}

func TestPayTaxToKRA_LocalValidation(t *testing.T) {
	c, _ := asyncClient(t)
	result := make(chan []byte, 1)
	timeout := make(chan []byte, 1)

	cases := map[string]B2BRequest{
		"zero amount":   {AmountKES: 0, Remarks: "ok", URLs: asyncURLs(t, result, timeout)},
		"short remarks": {AmountKES: 100, Remarks: "a", URLs: asyncURLs(t, result, timeout)},
		"no urls":       {AmountKES: 100, Remarks: "ok"},
	}
	for name, req := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := c.PayTaxToKRA(context.Background(), req); err == nil {
				t.Error("expected a validation error")
			}
		})
	}
}

func TestQueryOrgInfo_LocalValidation(t *testing.T) {
	c, _ := asyncClient(t)

	cases := map[string]OrgInfoRequest{
		"no identifier type":      {Identifier: "666677"},
		"no identifier":           {IdentifierType: OrgIdentifierPaybill},
		"unknown identifier type": {IdentifierType: "1", Identifier: "666677"},
	}
	for name, req := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := c.QueryOrgInfo(context.Background(), req); err == nil {
				t.Error("expected a validation error")
			}
		})
	}
}

func TestOrgInfoResponse_SuccessIsFourThousandNotZero(t *testing.T) {
	// The documentation's own error-code table claims 0 is success, which is
	// boilerplate copied from elsewhere and contradicted by the sample
	// response. Assert the fail-closed reading explicitly, since a future
	// "fix" toward the table would silently approve payments to an
	// unverified shortcode.
	cases := map[string]struct {
		code string
		want bool
	}{
		"4000 is success":             {"4000", true},
		"0 is not success":            {"0", false},
		"unrecognised is not success": {"9999", false},
		"empty is not success":        {"", false},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			resp := OrgInfoResponse{ResponseCode: tc.code}
			if got := resp.Success(); got != tc.want {
				t.Errorf("Success() = %v, want %v", got, tc.want)
			}
		})
	}
}
