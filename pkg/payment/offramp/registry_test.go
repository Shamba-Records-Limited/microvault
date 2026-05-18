package offramp

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeProvider struct {
	id ProviderID
}

func (f *fakeProvider) ID() ProviderID { return f.id }
func (f *fakeProvider) Initiate(_ context.Context, _ Request) (*Result, error) {
	return &Result{RequestID: string(f.id)}, nil
}

type fakeOptions struct{ id ProviderID }

func (o fakeOptions) ProviderID() ProviderID { return o.id }

func TestRegistry_Register_RejectsBadInputs(t *testing.T) {
	r := NewRegistry()

	require.Error(t, r.Register(nil))

	require.Error(t, r.Register(&fakeProvider{id: ""}))

	require.NoError(t, r.Register(&fakeProvider{id: "yc"}))
	require.Error(t, r.Register(&fakeProvider{id: "yc"}), "duplicate registration must fail")
}

func TestRegistry_Alias(t *testing.T) {
	r := NewRegistry()
	require.NoError(t, r.Register(&fakeProvider{id: "yc"}))

	require.Error(t, r.Alias("", "yc"), "empty payout method should be rejected")
	require.Error(t, r.Alias("mobile_money", "missing"), "alias to unregistered provider should be rejected")
	require.NoError(t, r.Alias(PayoutMethodMobileMoney, "yc"))
}

func TestRegistry_Resolve_ByOptions(t *testing.T) {
	r := NewRegistry()
	yc := &fakeProvider{id: "yc"}
	mg := &fakeProvider{id: "mg"}
	require.NoError(t, r.Register(yc))
	require.NoError(t, r.Register(mg))

	got, err := r.Resolve(Request{Options: fakeOptions{id: "mg"}})
	require.NoError(t, err)
	assert.Equal(t, ProviderID("mg"), got.ID())
}

func TestRegistry_Resolve_ByOptions_MissingProvider(t *testing.T) {
	r := NewRegistry()
	_, err := r.Resolve(Request{Options: fakeOptions{id: "ghost"}})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "ghost")
}

func TestRegistry_Resolve_ByPayoutMethod(t *testing.T) {
	r := NewRegistry()
	yc := &fakeProvider{id: "yc"}
	mg := &fakeProvider{id: "mg"}
	require.NoError(t, r.Register(yc))
	require.NoError(t, r.Register(mg))
	require.NoError(t, r.Alias(PayoutMethodMobileMoney, "yc"))
	require.NoError(t, r.Alias(PayoutMethodCashPickup, "mg"))

	got, err := r.Resolve(Request{PayoutMethod: PayoutMethodCashPickup})
	require.NoError(t, err)
	assert.Equal(t, ProviderID("mg"), got.ID())

	// Empty PayoutMethod → mobile money default.
	got, err = r.Resolve(Request{})
	require.NoError(t, err)
	assert.Equal(t, ProviderID("yc"), got.ID())
}

func TestRegistry_Resolve_NoMatchingAlias(t *testing.T) {
	r := NewRegistry()
	require.NoError(t, r.Register(&fakeProvider{id: "yc"}))

	_, err := r.Resolve(Request{PayoutMethod: "skywriting"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "skywriting")
}

func TestRegistry_GetAndAll(t *testing.T) {
	r := NewRegistry()
	require.NoError(t, r.Register(&fakeProvider{id: "yc"}))
	require.NoError(t, r.Register(&fakeProvider{id: "mg"}))

	p, ok := r.Get("yc")
	require.True(t, ok)
	assert.Equal(t, ProviderID("yc"), p.ID())

	_, ok = r.Get("ghost")
	assert.False(t, ok)

	all := r.All()
	assert.Len(t, all, 2)
}
