package yellowcard

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"testing"
	"time"
)

// forceAccept is the one field SubmitCollection overrides rather than passes
// through, so it is asserted on the wire rather than on the struct.
func TestSubmitCollection_ForcesAccept(t *testing.T) {
	var gotPath, gotMethod string
	var gotBody map[string]any

	a, srv := newTestAdapter(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotMethod = r.URL.Path, r.Method
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &gotBody)
		w.Write([]byte(`{"id":"col-1","status":"processing","sequenceId":"seq-1"}`))
	})
	defer srv.Close()

	got, err := a.SubmitCollection(context.Background(), CollectionRequest{
		ChannelID:    "ch-1",
		SequenceID:   "seq-1",
		CustomerUID:  "user-1",
		CustomerType: CustomerTypeRetail,
		Source:       CollectionSource{AccountType: ChannelTypeMomo, AccountNumber: "1111111111", NetworkID: "net-1"},
		Amount:       20,
	})
	if err != nil {
		t.Fatalf("SubmitCollection: %v", err)
	}

	if got.ID != "col-1" || got.Status != "processing" {
		t.Errorf("collection = %+v", got)
	}
	if gotMethod != http.MethodPost || gotPath != "/receive" {
		t.Errorf("request = %s %s, want POST /receive", gotMethod, gotPath)
	}
	if gotBody["forceAccept"] != true {
		t.Errorf("forceAccept = %v, want true", gotBody["forceAccept"])
	}
}

// A caller passing ForceAccept: false must not be able to reintroduce the
// approval window.
func TestSubmitCollection_ForceAcceptCannotBeUnset(t *testing.T) {
	var gotBody map[string]any
	a, srv := newTestAdapter(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &gotBody)
		w.Write([]byte(`{"id":"col-1"}`))
	})
	defer srv.Close()

	if _, err := a.SubmitCollection(context.Background(), CollectionRequest{ForceAccept: false}); err != nil {
		t.Fatalf("SubmitCollection: %v", err)
	}
	if gotBody["forceAccept"] != true {
		t.Errorf("forceAccept = %v, want true even when the caller set false", gotBody["forceAccept"])
	}
}

// Amount and LocalAmount are mutually exclusive at YellowCard, so the unset
// one must not reach the wire as a zero.
func TestSubmitCollection_OmitsUnsetAmount(t *testing.T) {
	var gotBody map[string]any
	a, srv := newTestAdapter(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &gotBody)
		w.Write([]byte(`{"id":"col-1"}`))
	})
	defer srv.Close()

	if _, err := a.SubmitCollection(context.Background(), CollectionRequest{LocalAmount: 2500}); err != nil {
		t.Fatalf("SubmitCollection: %v", err)
	}
	if _, present := gotBody["amount"]; present {
		t.Errorf("amount present in body %v, want omitted when only LocalAmount is set", gotBody)
	}
	if gotBody["localAmount"] != float64(2500) {
		t.Errorf("localAmount = %v, want 2500", gotBody["localAmount"])
	}
	// Recipient is only required for retail customers, so an unset one must
	// not reach YellowCard as an empty KYC block.
	if _, present := gotBody["recipient"]; present {
		t.Errorf("recipient present in body %v, want omitted when unset", gotBody)
	}
}

func TestCollectionPaths(t *testing.T) {
	tests := []struct {
		name   string
		call   func(*YellowcardAdapter) error
		method string
		path   string
	}{
		{
			name:   "accept",
			call:   func(a *YellowcardAdapter) error { _, err := a.AcceptCollection(t.Context(), "c1"); return err },
			method: http.MethodPost, path: "/receive/c1/accept",
		},
		{
			name:   "deny",
			call:   func(a *YellowcardAdapter) error { _, err := a.DenyCollection(t.Context(), "c1"); return err },
			method: http.MethodPost, path: "/receive/c1/deny",
		},
		{
			name:   "cancel",
			call:   func(a *YellowcardAdapter) error { _, err := a.CancelCollection(t.Context(), "c1"); return err },
			method: http.MethodPost, path: "/receive/c1/cancel",
		},
		{
			name:   "refund",
			call:   func(a *YellowcardAdapter) error { _, err := a.RefundCollection(t.Context(), "c1"); return err },
			method: http.MethodPost, path: "/receive/c1/refund",
		},
		{
			name:   "lookup",
			call:   func(a *YellowcardAdapter) error { _, err := a.LookupCollection(t.Context(), "c1"); return err },
			method: http.MethodGet, path: "/receive/c1",
		},
		{
			name: "lookup by sequence id",
			call: func(a *YellowcardAdapter) error {
				_, err := a.LookupCollectionBySequenceID(t.Context(), "seq-1")
				return err
			},
			method: http.MethodGet, path: "/receive/sequence-id/seq-1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var gotPath, gotMethod string
			a, srv := newTestAdapter(func(w http.ResponseWriter, r *http.Request) {
				gotPath, gotMethod = r.URL.Path, r.Method
				w.Write([]byte(`{"id":"c1"}`))
			})
			defer srv.Close()

			if err := tt.call(a); err != nil {
				t.Fatalf("%s: %v", tt.name, err)
			}
			if gotMethod != tt.method || gotPath != tt.path {
				t.Errorf("request = %s %s, want %s %s", gotMethod, gotPath, tt.method, tt.path)
			}
		})
	}
}

// The bank details a payer needs only appear once the request is accepted, so
// a collection must decode with and without them.
func TestLookupCollection_DecodesBankInfo(t *testing.T) {
	a, srv := newTestAdapter(func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte(`{"id":"c1","status":"complete","reference":"JJ8094861","depositId":"dep-1",
			"convertedAmount":9757.8,"rate":487.89,
			"bankInfo":{"name":"PAGA","accountNumber":"1234567890","accountName":"Ken Adams"},
			"recipient":{"name":"John Doe","phone":"+254700000000"}}`))
	})
	defer srv.Close()

	got, err := a.LookupCollection(context.Background(), "c1")
	if err != nil {
		t.Fatalf("LookupCollection: %v", err)
	}
	if got.BankInfo == nil || got.BankInfo.AccountNumber != "1234567890" {
		t.Errorf("bankInfo = %+v", got.BankInfo)
	}
	if got.Reference != "JJ8094861" || got.DepositID != "dep-1" {
		t.Errorf("reference = %q, depositId = %q", got.Reference, got.DepositID)
	}
	if got.Recipient.Name != "John Doe" {
		t.Errorf("recipient = %+v", got.Recipient)
	}
}

func TestLookupCollection_NilBankInfoBeforeAccept(t *testing.T) {
	a, srv := newTestAdapter(func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte(`{"id":"c1","status":"created"}`))
	})
	defer srv.Close()

	got, err := a.LookupCollection(context.Background(), "c1")
	if err != nil {
		t.Fatalf("LookupCollection: %v", err)
	}
	if got.BankInfo != nil {
		t.Errorf("bankInfo = %+v, want nil before acceptance", got.BankInfo)
	}
}

func TestListCollections(t *testing.T) {
	var gotQuery string
	a, srv := newTestAdapter(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		w.Write([]byte(`{"collections":[{"id":"c1"},{"id":"c2"}]}`))
	})
	defer srv.Close()

	got, err := a.ListCollections(context.Background(), ListCollectionsParams{PerPage: 50, OrderBy: "asc"})
	if err != nil {
		t.Fatalf("ListCollections: %v", err)
	}
	if len(got) != 2 || got[0].ID != "c1" {
		t.Errorf("collections = %+v", got)
	}
	if gotQuery != "orderBy=asc&perPage=50" {
		t.Errorf("query = %q, want orderBy=asc&perPage=50", gotQuery)
	}
}

// An unset param must be absent rather than sent as a zero, which YellowCard
// would read as a real page size or an epoch date.
func TestListCollectionsParams_OmitsUnset(t *testing.T) {
	if q := (ListCollectionsParams{}).query(); q != "" {
		t.Errorf("query = %q, want empty for the zero value", q)
	}

	q := ListCollectionsParams{StartDate: time.Date(2026, 8, 26, 10, 0, 0, 0, time.UTC)}.query()
	if q != "startDate=2026-08-26T10%3A00%3A00Z" {
		t.Errorf("query = %q", q)
	}
}

func TestCollections_ErrorStatus(t *testing.T) {
	a, srv := newTestAdapter(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"code":"PaymentInvalidState","message":"collection is not in complete/cancelled/refund_failed state."}`))
	})
	defer srv.Close()

	if _, err := a.RefundCollection(context.Background(), "c1"); err == nil {
		t.Error("expected error on 400 status")
	}
}

func TestCollections_BadJSON(t *testing.T) {
	a, srv := newTestAdapter(func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte(`{not json`))
	})
	defer srv.Close()

	if _, err := a.LookupCollection(context.Background(), "c1"); err == nil {
		t.Error("expected decode error on malformed JSON")
	}
}

// Collections and disbursements draw from disjoint channel lists.
func TestFilterChannelsByRampType(t *testing.T) {
	channels := []Channel{
		{ID: "w1", RampType: RampTypeWithdraw},
		{ID: "d1", RampType: RampTypeDeposit},
		{ID: "w2", RampType: RampTypeWithdraw},
	}

	deposits := FilterChannelsByRampType(channels, RampTypeDeposit)
	if len(deposits) != 1 || deposits[0].ID != "d1" {
		t.Errorf("deposit channels = %+v", deposits)
	}
	if withdrawals := FilterChannelsByRampType(channels, RampTypeWithdraw); len(withdrawals) != 2 {
		t.Errorf("withdraw channels = %+v", withdrawals)
	}
}
