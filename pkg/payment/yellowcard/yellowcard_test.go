package yellowcard

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

// newTestAdapter points a YellowcardAdapter at a test server.
func newTestAdapter(handler http.HandlerFunc) (*YellowcardAdapter, *httptest.Server) {
	srv := httptest.NewServer(handler)
	return NewYellowcardAdapter("pub", "sec", srv.URL), srv
}

func TestGetChannels_Success(t *testing.T) {
	var gotQuery string
	a, srv := newTestAdapter(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		w.Write([]byte(`{"channels":[{"id":"ch1","country":"KE"},{"id":"ch2","country":"KE"}]}`))
	})
	defer srv.Close()

	channels, err := a.GetChannels(context.Background(), "KE")
	if err != nil {
		t.Fatalf("GetChannels: %v", err)
	}
	if len(channels) != 2 || channels[0].ID != "ch1" {
		t.Errorf("channels = %+v", channels)
	}
	if gotQuery != "country=KE" {
		t.Errorf("query = %q, want country=KE", gotQuery)
	}
}

func TestGetChannels_ErrorStatus(t *testing.T) {
	a, srv := newTestAdapter(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"message":"boom"}`))
	})
	defer srv.Close()

	if _, err := a.GetChannels(context.Background(), ""); err == nil {
		t.Error("expected error on 500 status")
	}
}

func TestGetChannels_BadJSON(t *testing.T) {
	a, srv := newTestAdapter(func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte(`{not json`))
	})
	defer srv.Close()

	if _, err := a.GetChannels(context.Background(), ""); err == nil {
		t.Error("expected decode error on malformed JSON")
	}
}

func TestGetAvailableBalance(t *testing.T) {
	// A USD fiat account is present -> its Available is returned.
	a, srv := newTestAdapter(func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte(`{"accounts":[{"available":50,"currency":"KES","currencyType":"fiat"},{"available":123.45,"currency":"USD","currencyType":"fiat"}]}`))
	})
	defer srv.Close()

	bal, err := a.GetAvailableBalance(context.Background())
	if err != nil {
		t.Fatalf("GetAvailableBalance: %v", err)
	}
	if bal != 123.45 {
		t.Errorf("balance = %v, want 123.45", bal)
	}
}

func TestGetNetworksRatesAccount(t *testing.T) {
	a, srv := newTestAdapter(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/networks":
			w.Write([]byte(`{"networks":[{"code":"MPESA"}]}`))
		case "/rates":
			w.Write([]byte(`{"rates":[{},{}]}`))
		case "/account":
			w.Write([]byte(`{"accounts":[{"available":1,"currency":"USD","currencyType":"fiat"}]}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})
	defer srv.Close()
	ctx := context.Background()

	if nets, err := a.GetNetworks(ctx, "KE"); err != nil || len(nets) != 1 {
		t.Errorf("GetNetworks = %v (err %v)", nets, err)
	}
	if rates, err := a.GetRates(ctx, "KES"); err != nil || len(rates) != 2 {
		t.Errorf("GetRates = %v (err %v)", rates, err)
	}
	if accts, err := a.GetAccount(ctx); err != nil || len(accts) != 1 {
		t.Errorf("GetAccount = %v (err %v)", accts, err)
	}
}

func TestGetAvailableBalance_NoUSD(t *testing.T) {
	a, srv := newTestAdapter(func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte(`{"accounts":[{"available":50,"currency":"KES","currencyType":"fiat"}]}`))
	})
	defer srv.Close()

	if _, err := a.GetAvailableBalance(context.Background()); err == nil {
		t.Error("expected error when no USD account exists")
	}
}
