package sensors

import (
	"fmt"
	"sync"
)

// SensorConstructor builds an unconfigured Sensor. Configuration is a separate
// lifecycle step (Sensor.Configure) so the registry can instantiate a sensor
// type without its per-instance [config] block or secrets. Returning an error
// aborts BuildSensor.
type SensorConstructor func() (Sensor, error)

// ForgeConstructor mirrors SensorConstructor for the write side. The forge
// surface is reserved in this phase; full PR methods land later.
type ForgeConstructor func(cfg map[string]any, secrets SecretResolver) (Forge, error)

// Registry holds the runtime mapping of name to constructor. There are two
// parallel namespaces — sensors and forges — because the same name (e.g.
// "github") legitimately appears in both with distinct roles. The registry is
// goroutine-safe.
type Registry struct {
	mu      sync.RWMutex
	sensors map[string]SensorConstructor
	forges  map[string]ForgeConstructor
}

// NewRegistry returns an empty Registry. Tests construct their own instance for
// isolation; production code uses Default().
func NewRegistry() *Registry {
	return &Registry{
		sensors: make(map[string]SensorConstructor),
		forges:  make(map[string]ForgeConstructor),
	}
}

// RegisterSensor registers a Sensor constructor under the given name. Adapters
// call this from their package init() so the binary picks them up
// automatically. Duplicate registrations panic — this is an init-time
// programmer error, not a runtime condition.
func (r *Registry) RegisterSensor(name string, ctor SensorConstructor) {
	if ctor == nil {
		panic(fmt.Sprintf("sensors: nil Sensor constructor for %q", name))
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.sensors[name]; exists {
		panic(fmt.Sprintf("sensors: Sensor %q already registered", name))
	}
	r.sensors[name] = ctor
}

// RegisterForge mirrors RegisterSensor for forges.
func (r *Registry) RegisterForge(name string, ctor ForgeConstructor) {
	if ctor == nil {
		panic(fmt.Sprintf("sensors: nil Forge constructor for %q", name))
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.forges[name]; exists {
		panic(fmt.Sprintf("sensors: Forge %q already registered", name))
	}
	r.forges[name] = ctor
}

// BuildSensor instantiates the named sensor, returning the constructed (but not
// yet Configured) adapter or an error if the name is not registered or the
// constructor errors. Callers invoke Sensor.Configure afterward with the
// instance's [config] block and a SecretResolver.
func (r *Registry) BuildSensor(name string) (Sensor, error) {
	r.mu.RLock()
	ctor, ok := r.sensors[name]
	r.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("sensors: no Sensor registered for %q", name)
	}
	s, err := ctor()
	if err != nil {
		return nil, fmt.Errorf("sensors: build Sensor %q: %w", name, err)
	}
	return s, nil
}

// BuildForge mirrors BuildSensor for forges. A nil cfg is treated as empty.
func (r *Registry) BuildForge(name string, cfg map[string]any, secrets SecretResolver) (Forge, error) {
	r.mu.RLock()
	ctor, ok := r.forges[name]
	r.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("sensors: no Forge registered for %q", name)
	}
	if cfg == nil {
		cfg = map[string]any{}
	}
	forge, err := ctor(cfg, secrets)
	if err != nil {
		return nil, fmt.Errorf("sensors: build Forge %q: %w", name, err)
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
