package airtel

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/Shamba-Records-Limited/microvault/pkg/payment/airtel/airtelstub"
)

func TestAccessToken_CachesUntilExpiry(t *testing.T) {
	stub := airtelstub.New(t)
	now := time.Now()

	client, err := New(Config{
		Environment:  EnvironmentStaging,
		ClientID:     "client-id",
		ClientSecret: "client-secret",
		BaseURL:      stub.URL(),
		Clock:        func() time.Time { return now },
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	for range 3 {
		if _, err := client.AccessToken(context.Background()); err != nil {
			t.Fatalf("AccessToken: %v", err)
		}
	}
	if got := stub.TokensIssued(); got != 1 {
		t.Fatalf("minted %d tokens, want 1", got)
	}

	// 180 seconds less the skew: one second before, the cached token still
	// stands; past it, a fresh one is minted.
	now = now.Add(180*time.Second - tokenSkew - time.Second)
	if _, err := client.AccessToken(context.Background()); err != nil {
		t.Fatalf("AccessToken: %v", err)
	}
	if got := stub.TokensIssued(); got != 1 {
		t.Fatalf("minted %d tokens before expiry, want 1", got)
	}

	now = now.Add(2 * time.Second)
	if _, err := client.AccessToken(context.Background()); err != nil {
		t.Fatalf("AccessToken: %v", err)
	}
	if got := stub.TokensIssued(); got != 2 {
		t.Fatalf("minted %d tokens after expiry, want 2", got)
	}
}

// The skew has to be small relative to a 180 second life. A minute — which is
// right for Daraja's hour — would throw away a third of every token.
func TestTokenSkewIsSmallRelativeToLifetime(t *testing.T) {
	if tokenSkew >= defaultTokenSeconds*time.Second/10 {
		t.Fatalf("tokenSkew %s is more than a tenth of the %d second lifetime", tokenSkew, defaultTokenSeconds)
	}
}

// Concurrent callers must collapse into one mint. At 180 seconds a cluster
// re-mints constantly, so a stampede here is a stampede in production.
func TestAccessToken_SingleFlight(t *testing.T) {
	stub := airtelstub.New(t)
	client, err := New(Config{
		Environment:  EnvironmentStaging,
		ClientID:     "client-id",
		ClientSecret: "client-secret",
		BaseURL:      stub.URL(),
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	var wg sync.WaitGroup
	for range 16 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := client.AccessToken(context.Background()); err != nil {
				t.Errorf("AccessToken: %v", err)
			}
		}()
	}
	wg.Wait()

	if got := stub.TokensIssued(); got != 1 {
		t.Fatalf("minted %d tokens concurrently, want 1", got)
	}
}

func TestAccessToken_RejectsBadCredentials(t *testing.T) {
	stub := airtelstub.New(t, airtelstub.WithCredentials("right-id", "right-secret"))
	client, err := New(Config{
		Environment:  EnvironmentStaging,
		ClientID:     "wrong-id",
		ClientSecret: "wrong-secret",
		BaseURL:      stub.URL(),
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	if _, err := client.AccessToken(context.Background()); err == nil {
		t.Fatal("expected bad credentials to be refused")
	}
}

// A token that expired in flight is recovered from once, not looped on.
func TestCall_RetriesOnceOnRejectedToken(t *testing.T) {
	stub := airtelstub.New(t)
	client, err := New(Config{
		Environment:  EnvironmentStaging,
		ClientID:     "client-id",
		ClientSecret: "client-secret",
		BaseURL:      stub.URL(),
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	if _, err := client.AccessToken(context.Background()); err != nil {
		t.Fatalf("AccessToken: %v", err)
	}

	// Force the cached token to be one the stub no longer accepts.
	if err := client.tokens.Set(context.Background(), client.tokenKey(), "stale-token", time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("seed stale token: %v", err)
	}

	if _, err := client.Payment(context.Background(), validPayment()); err != nil {
		t.Fatalf("Payment did not recover from a stale token: %v", err)
	}
	if got := stub.TokensIssued(); got != 2 {
		t.Fatalf("minted %d tokens, want 2 (the original and the recovery)", got)
	}
}

func TestNew_Validation(t *testing.T) {
	cases := map[string]Config{
		"no environment":  {ClientID: "a", ClientSecret: "b"},
		"bad environment": {Environment: "uat", ClientID: "a", ClientSecret: "b"},
		"no id":           {Environment: EnvironmentStaging, ClientSecret: "b"},
		"no secret":       {Environment: EnvironmentStaging, ClientID: "a"},
	}

	for name, cfg := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := New(cfg); err == nil {
				t.Fatal("expected the misconfiguration to fail at construction")
			}
		})
	}
}

func TestNew_Defaults(t *testing.T) {
	client, err := New(Config{
		Environment:  EnvironmentProduction,
		ClientID:     "a",
		ClientSecret: "b",
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if client.Country() != defaultCountry {
		t.Fatalf("country = %q, want %q", client.Country(), defaultCountry)
	}
	if client.Currency() != defaultCurrency {
		t.Fatalf("currency = %q, want %q", client.Currency(), defaultCurrency)
	}
	if got := EnvironmentProduction.BaseURL(); got != productionBaseURL {
		t.Fatalf("production base URL = %q", got)
	}
	if got := EnvironmentStaging.BaseURL(); got != stagingBaseURL {
		t.Fatalf("staging base URL = %q", got)
	}
}
