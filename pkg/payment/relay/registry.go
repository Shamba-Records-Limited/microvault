package relay

import (
	"slices"

	"github.com/samber/lo"

	pkgErrors "github.com/Shamba-Records-Limited/microvault/pkg/errors"
)

// Registry holds the rate sources a Router may quote.
//
// Register everything during boot, then freeze: reads are safe for concurrent
// use afterwards, concurrent registration is not. Mirrors offramp.Registry.
type Registry struct {
	sources map[string]RateSource
	order   []RateSource
}

// NewRegistry returns an empty Registry.
func NewRegistry() *Registry {
	return &Registry{sources: make(map[string]RateSource)}
}

// Register adds a source, keyed by its Name. A nil source, an empty name or a
// duplicate is a boot-time misconfiguration and fails loudly.
func (r *Registry) Register(source RateSource) error {
	errb := relayErr("register")

	if source == nil {
		return errb.Code(pkgErrors.CodeMissingDependency).Errorf("rate source is nil")
	}
	name := source.Name()
	if name == "" {
		return errb.Code(pkgErrors.CodeMissingDependency).Errorf("rate source returned an empty name")
	}
	if _, exists := r.sources[name]; exists {
		return errb.
			With(pkgErrors.AttrProvider, name).
			Code(pkgErrors.CodeDuplicateRequest).
			Errorf("rate source is already registered")
	}

	r.sources[name] = source
	r.order = append(r.order, source)
	return nil
}

// MustRegister panics on a failed registration, for boot wiring with no useful
// recovery.
func (r *Registry) MustRegister(source RateSource) {
	if err := r.Register(source); err != nil {
		panic(err)
	}
}

// Get returns a source by name.
func (r *Registry) Get(name string) (RateSource, bool) {
	source, ok := r.sources[name]
	return source, ok
}

// All returns every registered source in registration order.
func (r *Registry) All() []RateSource { return slices.Clone(r.order) }

// Names returns every registered source name in registration order.
func (r *Registry) Names() []string {
	return lo.Map(r.order, func(s RateSource, _ int) string { return s.Name() })
}

// Len reports how many sources are registered.
func (r *Registry) Len() int { return len(r.order) }

// serving returns the sources that can price a direction. A source that does
// not implement DirectionalSource is asked for every direction.
func (r *Registry) serving(direction string) []RateSource {
	return lo.Filter(r.order, func(s RateSource, _ int) bool {
		directional, ok := s.(DirectionalSource)
		return !ok || directional.SupportsDirection(direction)
	})
}
