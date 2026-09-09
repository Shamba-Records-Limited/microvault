package cashin

import (
	"github.com/samber/oops"

	pkgErrors "github.com/Shamba-Records-Limited/microvault/pkg/errors"
)

// registryErr starts an error builder for provider registration and dispatch.
func registryErr(op string) oops.OopsErrorBuilder {
	return oops.In(pkgErrors.DomainRepaymentCashIn).Tags("registry").With(pkgErrors.AttrOperation, op)
}

// Registry maps ProviderIDs to Provider implementations and resolves an
// incoming Request to the right provider. The resolution order is:
//
//  1. Options naming a provider explicitly (Options.ProviderID) wins outright.
//  2. Otherwise the request's CollectionMethod is looked up in the aliases;
//     an empty method defaults to CollectionMethodPayBill, the passive rail,
//     because a caller that forgot to choose a method must not silently push
//     an STK prompt at a borrower's handset.
type Registry struct {
	providers map[ProviderID]Provider
	aliases   map[CollectionMethod]ProviderID
}

// NewRegistry returns an empty Registry.
func NewRegistry() *Registry {
	return &Registry{
		providers: make(map[ProviderID]Provider),
		aliases:   make(map[CollectionMethod]ProviderID),
	}
}

// Register adds a Provider to the registry, keyed by its ID(). Returns an
// error if the provider is nil, has an empty ID, or duplicates an existing
// registration — boot-time misconfigurations should fail loudly.
func (r *Registry) Register(p Provider) error {
	if p == nil {
		return registryErr("register").Code(pkgErrors.CodeMissingDependency).Errorf("provider is nil")
	}
	id := p.ID()
	if id == "" {
		return registryErr("register").Code(pkgErrors.CodeMissingDependency).Errorf("provider returned an empty ID")
	}
	if _, exists := r.providers[id]; exists {
		return registryErr("register").With(pkgErrors.AttrProvider, string(id)).Code(pkgErrors.CodeDuplicateRequest).Errorf("provider is already registered")
	}
	r.providers[id] = p
	return nil
}

// Alias maps a CollectionMethod string (e.g. CollectionMethodPrompt) to a
// registered ProviderID. The provider must already be registered.
func (r *Registry) Alias(method CollectionMethod, id ProviderID) error {
	if method == "" {
		return registryErr("alias").Code(pkgErrors.CodeMissingAccount).Errorf("collection method is empty")
	}
	if _, exists := r.providers[id]; !exists {
		return registryErr("alias").With(pkgErrors.AttrProvider, string(id)).Code(pkgErrors.CodeNotFound).Errorf("alias target is not a registered provider")
	}
	r.aliases[method] = id
	return nil
}

// Get returns the Provider for an explicit ID. Useful for callers that
// already know which provider they want.
func (r *Registry) Get(id ProviderID) (Provider, bool) {
	p, ok := r.providers[id]
	return p, ok
}

// All returns every registered Provider in unspecified order. Useful for
// fanning out menu rendering across providers (Directory capability).
func (r *Registry) All() []Provider {
	out := make([]Provider, 0, len(r.providers))
	for _, p := range r.providers {
		out = append(out, p)
	}
	return out
}

// Resolve picks the Provider for a Request. See Registry doc-comment for the
// resolution order. Returns a wrapped error when no provider matches so
// callers can distinguish misconfiguration from runtime failure.
func (r *Registry) Resolve(req Request) (Provider, error) {
	if req.Options != nil {
		id := req.Options.ProviderID()
		if p, ok := r.providers[id]; ok {
			return p, nil
		}
		return nil, registryErr("resolve").With(pkgErrors.AttrProvider, string(id)).Code(pkgErrors.CodeNotFound).Errorf("provider named in the request options is not registered")
	}

	method := req.CollectionMethod
	if method == "" {
		method = CollectionMethodPayBill
	}
	id, ok := r.aliases[method]
	if !ok {
		return nil, registryErr("resolve").With("collection_method", string(method)).Code(pkgErrors.CodeNotFound).Errorf("no provider is aliased to this collection method")
	}
	p, ok := r.providers[id]
	if !ok {
		// Indicates Alias was called but the underlying provider was later
		// removed — defensive, shouldn't happen at runtime.
		return nil, registryErr("resolve").With("collection_method", string(method)).With(pkgErrors.AttrProvider, string(id)).Code(pkgErrors.CodeNotFound).Errorf("collection method alias points at a provider that is no longer registered")
	}
	return p, nil
}
