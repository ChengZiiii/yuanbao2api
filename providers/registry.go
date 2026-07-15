package provider

import (
	"fmt"
	"strings"
	"sync"
)

// Registry is a process-wide catalog of Providers plus a model→provider
// router. main.go registers every concrete provider at startup and
// sets the default provider name; handlers then call Route(model) to
// turn an OpenAI model name into the right provider instance.
//
// Registry is safe for concurrent use; methods take a read lock for
// lookups and a write lock for mutations.
type Registry struct {
	mu          sync.RWMutex
	providers   []Provider
	byName      map[string]Provider
	defaultName string
}

// NewRegistry creates an empty registry. Use Registry() to access the
// package-level singleton.
func NewRegistry() *Registry {
	return &Registry{byName: map[string]Provider{}}
}

// global is the package-level singleton. main.go is expected to call
// Register + SetDefault once at startup; handlers read it through
// Global() afterward.
var global = NewRegistry()

// Global returns the process-wide singleton registry.
func Global() *Registry { return global }

// Register adds a provider to the catalog. Duplicates by name are
// rejected so misconfiguration does not silently shadow the right
// implementation.
func (r *Registry) Register(p Provider) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.byName[p.Name()]; exists {
		return fmt.Errorf("provider %q already registered", p.Name())
	}
	r.providers = append(r.providers, p)
	r.byName[p.Name()] = p
	return nil
}

// All returns a snapshot of the registered providers in registration
// order. Useful for /v1/models enumeration.
func (r *Registry) All() []Provider {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]Provider, len(r.providers))
	copy(out, r.providers)
	return out
}

// Names returns the registered provider names in registration order.
func (r *Registry) Names() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]string, len(r.providers))
	for i, p := range r.providers {
		out[i] = p.Name()
	}
	return out
}

// Get returns the provider registered under the given name, plus a
// boolean indicating whether it was found.
func (r *Registry) Get(name string) (Provider, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	p, ok := r.byName[name]
	return p, ok
}

// Default returns the current default provider, or nil if none has
// been set.
func (r *Registry) Default() Provider {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if r.defaultName == "" {
		return nil
	}
	return r.byName[r.defaultName]
}

// DefaultName returns the configured default provider name.
func (r *Registry) DefaultName() string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.defaultName
}

// SetDefault updates the default provider name. The name must be a
// previously registered provider.
func (r *Registry) SetDefault(name string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.byName[name]; !ok {
		return fmt.Errorf("provider %q not registered", name)
	}
	r.defaultName = name
	return nil
}

// Route resolves a model name to a provider. The algorithm:
//
//  1. Look up the model in the default provider's Models(); if found,
//     return that provider.
//  2. Otherwise scan every other registered provider in registration
//     order; first match wins.
//  3. If nothing matches, return (nil, "unknown model: <name>") so the
//     handler can answer 400.
//
// Enabled state is intentionally NOT checked here — Route is a pure
// name-based lookup. Callers that care about enablement should call
// IsEnabled on the returned provider.
func (r *Registry) Route(modelName string) (Provider, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	normalized := strings.ToLower(strings.TrimSpace(modelName))

	if def := r.byName[r.defaultName]; def != nil {
		if hasModel(def.Models(), normalized) {
			return def, nil
		}
	}
	for _, p := range r.providers {
		if p.Name() == r.defaultName {
			continue
		}
		if hasModel(p.Models(), normalized) {
			return p, nil
		}
	}
	return nil, fmt.Errorf("unknown model: %s", modelName)
}

func hasModel(models []ModelInfo, normalizedID string) bool {
	for _, m := range models {
		if strings.EqualFold(m.ID, normalizedID) {
			return true
		}
	}
	return false
}