package cashin

import (
	"context"
	"testing"
)

type fakeProvider struct{ id ProviderID }

func (f fakeProvider) ID() ProviderID { return f.id }
func (f fakeProvider) Collect(_ context.Context, _ Request) (*Result, error) {
	return &Result{LoanID: "x"}, nil
}

func TestRegister_Duplicate(t *testing.T) {
	r := NewRegistry()
	if err := r.Register(fakeProvider{id: "mpesa"}); err != nil {
		t.Fatalf("first Register: %v", err)
	}
	if err := r.Register(fakeProvider{id: "mpesa"}); err == nil {
		t.Error("duplicate registration accepted")
	}
}

func TestRegister_RejectsNilAndEmptyID(t *testing.T) {
	r := NewRegistry()
	if err := r.Register(nil); err == nil {
		t.Error("nil provider accepted")
	}
	if err := r.Register(fakeProvider{id: ""}); err == nil {
		t.Error("empty ID accepted")
	}
}

func TestResolve_ExplicitOptionsWins(t *testing.T) {
	r := NewRegistry()
	_ = r.Register(fakeProvider{id: "mpesa"})
	_ = r.Register(fakeProvider{id: "moneygram"})
	_ = r.Alias(CollectionMethodPayBill, "moneygram")

	got, err := r.Resolve(Request{Options: testOptions{id: "mpesa"}})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got.ID() != "mpesa" {
		t.Errorf("resolved %q, want the explicitly-pinned provider", got.ID())
	}
}

func TestResolve_AliasDefault(t *testing.T) {
	r := NewRegistry()
	_ = r.Register(fakeProvider{id: "mpesa"})
	_ = r.Alias(CollectionMethodPayBill, "mpesa")

	// Empty method defaults to the passive rail: a caller that forgot to
	// choose must not silently push a prompt at someone's handset.
	got, err := r.Resolve(Request{})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got.ID() != "mpesa" {
		t.Errorf("resolved %q", got.ID())
	}
}

func TestResolve_UnaliasedMethod(t *testing.T) {
	r := NewRegistry()
	_ = r.Register(fakeProvider{id: "mpesa"})

	if _, err := r.Resolve(Request{CollectionMethod: CollectionMethodPayBill}); err == nil {
		t.Error("an unaliased method resolved")
	}
}

func TestGetAndAll(t *testing.T) {
	r := NewRegistry()
	_ = r.Register(fakeProvider{id: "mpesa"})
	_ = r.Register(fakeProvider{id: "moneygram"})

	if _, ok := r.Get("mpesa"); !ok {
		t.Error("Get(mpesa) missed")
	}
	if _, ok := r.Get("absent"); ok {
		t.Error("Get(absent) hit")
	}
	if len(r.All()) != 2 {
		t.Errorf("All() = %d, want 2", len(r.All()))
	}
}

type testOptions struct{ id ProviderID }

func (o testOptions) ProviderID() ProviderID { return o.id }
