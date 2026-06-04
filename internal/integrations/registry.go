package integrations

import (
	"fmt"
	"sync"
)

// TicketSourceConstructor builds a TicketSource from its parsed config section
// and a secret resolver. Returning an error (e.g. credential resolution
// failed) aborts BuildTicketSource.
type TicketSourceConstructor func(cfg map[string]any, secrets SecretResolver) (TicketSource, error)

// ForgeConstructor mirrors TicketSourceConstructor for the write side.
type ForgeConstructor func(cfg map[string]any, secrets SecretResolver) (Forge, error)

// Registry holds the runtime mapping of integration name to constructor. There
// are two parallel namespaces — ticket sources and forges — because the same
// name (e.g. "github") legitimately appears in both with distinct roles. The
// registry is goroutine-safe.
type Registry struct {
	mu      sync.RWMutex
	sources map[string]TicketSourceConstructor
	forges  map[string]ForgeConstructor
}

// NewRegistry returns an empty Registry. Tests construct their own instance for
// isolation; production code uses Default().
func NewRegistry() *Registry {
	return &Registry{
		sources: make(map[string]TicketSourceConstructor),
		forges:  make(map[string]ForgeConstructor),
	}
}

// RegisterTicketSource registers a TicketSource constructor under the given
// name. Adapters call this from their package init() so the binary picks them
// up automatically. Duplicate registrations panic — this is an init-time
// programmer error, not a runtime condition.
func (r *Registry) RegisterTicketSource(name string, ctor TicketSourceConstructor) {
	if ctor == nil {
		panic(fmt.Sprintf("integrations: nil TicketSource constructor for %q", name))
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.sources[name]; exists {
		panic(fmt.Sprintf("integrations: TicketSource %q already registered", name))
	}
	r.sources[name] = ctor
}

// RegisterForge mirrors RegisterTicketSource for forges.
func (r *Registry) RegisterForge(name string, ctor ForgeConstructor) {
	if ctor == nil {
		panic(fmt.Sprintf("integrations: nil Forge constructor for %q", name))
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.forges[name]; exists {
		panic(fmt.Sprintf("integrations: Forge %q already registered", name))
	}
	r.forges[name] = ctor
}

// BuildTicketSource resolves the named source, returning the constructed
// adapter or an error if the name is not registered or the constructor errors
// (e.g. credential resolution failed). A nil cfg is treated as empty.
func (r *Registry) BuildTicketSource(name string, cfg map[string]any, secrets SecretResolver) (TicketSource, error) {
	r.mu.RLock()
	ctor, ok := r.sources[name]
	r.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("integrations: no TicketSource registered for %q", name)
	}
	if cfg == nil {
		cfg = map[string]any{}
	}
	src, err := ctor(cfg, secrets)
	if err != nil {
		return nil, fmt.Errorf("integrations: build TicketSource %q: %w", name, err)
	}
	return src, nil
}

// BuildForge mirrors BuildTicketSource for forges.
func (r *Registry) BuildForge(name string, cfg map[string]any, secrets SecretResolver) (Forge, error) {
	r.mu.RLock()
	ctor, ok := r.forges[name]
	r.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("integrations: no Forge registered for %q", name)
	}
	if cfg == nil {
		cfg = map[string]any{}
	}
	forge, err := ctor(cfg, secrets)
	if err != nil {
		return nil, fmt.Errorf("integrations: build Forge %q: %w", name, err)
	}
	return forge, nil
}

// defaultRegistry is the process-wide registry adapters register into from
// their package init().
var defaultRegistry = NewRegistry()

// Default returns the process registry. Adapters register into it from init();
// tests that need isolation construct their own via NewRegistry().
func Default() *Registry {
	return defaultRegistry
}
