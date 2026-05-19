package offramp

import "fmt"

// Registry maps ProviderIDs to Provider implementations and resolves an
// incoming Request to the right provider. The resolution order is:
//
//  1. If Request.Options is set, use Options.ProviderID() — the caller has
//     pinned a specific provider by attaching its options.
//  2. Otherwise, look up Request.PayoutMethod in the alias table (empty
//     PayoutMethod is treated as PayoutMethodMobileMoney).
//
// A Registry is safe for concurrent reads after construction; concurrent
// Register / Alias calls are not synchronised — register everything during
// boot, then freeze.
type Registry struct {
	providers map[ProviderID]Provider
	aliases   map[string]ProviderID
}

// NewRegistry returns an empty Registry.
func NewRegistry() *Registry {
	return &Registry{
		providers: make(map[ProviderID]Provider),
		aliases:   make(map[string]ProviderID),
	}
}

// Register adds a Provider to the registry, keyed by its ID(). Returns an
// error if the provider is nil, has an empty ID, or duplicates an existing
// registration — boot-time misconfigurations should fail loudly.
func (r *Registry) Register(p Provider) error {
	if p == nil {
		return fmt.Errorf("offramp registry: nil provider")
	}
	id := p.ID()
	if id == "" {
		return fmt.Errorf("offramp registry: provider returned empty ID")
	}
	if _, exists := r.providers[id]; exists {
		return fmt.Errorf("offramp registry: provider %q already registered", id)
	}
	r.providers[id] = p
	return nil
}

// Alias maps a PayoutMethod string (e.g. PayoutMethodMobileMoney) to a
// registered ProviderID. The provider must already be registered.
func (r *Registry) Alias(payoutMethod string, id ProviderID) error {
	if payoutMethod == "" {
		return fmt.Errorf("offramp registry: empty payout method")
	}
	if _, exists := r.providers[id]; !exists {
		return fmt.Errorf("offramp registry: alias target %q is not registered", id)
	}
	r.aliases[payoutMethod] = id
	return nil
}

// Get returns the Provider for an explicit ID. Useful for callers that
// already know which provider they want (e.g. a poller bound to MG loans).
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
		return nil, fmt.Errorf("offramp registry: provider %q (from Options) not registered", id)
	}

	method := req.PayoutMethod
	if method == "" {
		method = PayoutMethodMobileMoney
	}
	id, ok := r.aliases[method]
	if !ok {
		return nil, fmt.Errorf("offramp registry: no provider aliased to payout method %q", method)
	}
	p, ok := r.providers[id]
	if !ok {
		// Indicates Alias was called but the underlying provider was later
		// removed — defensive, shouldn't happen at runtime.
		return nil, fmt.Errorf("offramp registry: alias %q points to missing provider %q", method, id)
	}
	return p, nil
}
