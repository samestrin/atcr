package circuitbreaker

import "sync"

// Registry holds one Breaker per provider, created on first use with the default
// threshold and cooldown. A provider's breaker is shared across every agent and
// goroutine that uses that provider, so provider health is tracked once (per
// provider) rather than independently per agent — the whole point of the epic.
// Safe for concurrent use.
type Registry struct {
	mu       sync.RWMutex
	breakers map[string]*Breaker
}

// NewRegistry creates an empty registry. Tests use a fresh registry for
// isolation; production uses the package-global DefaultRegistry.
func NewRegistry() *Registry {
	return &Registry{breakers: make(map[string]*Breaker)}
}

// Get returns the breaker for provider, creating a closed one with the default
// tuning on first use. The same provider name always returns the same instance,
// so collaborating call sites share one circuit. The common already-exists case
// takes only a read lock; creation upgrades to a write lock and re-checks so a
// concurrent creator cannot produce two breakers for one provider.
//
// An empty provider string returns a fresh throwaway breaker on every call
// rather than a cached one; this makes Get("") safe to call even when callers
// forget the upstream empty-provider guard — failures never accumulate across
// calls and no shared "" circuit can trip to fail-fast unkeyed requests.
func (r *Registry) Get(provider string) *Breaker {
	if provider == "" {
		return New("", DefaultThreshold, DefaultCooldown)
	}

	r.mu.RLock()
	b, ok := r.breakers[provider]
	r.mu.RUnlock()
	if ok {
		return b
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	// Re-check under the write lock: another goroutine may have created it
	// between the RUnlock above and the Lock here.
	if b, ok := r.breakers[provider]; ok {
		return b
	}
	b = New(provider, DefaultThreshold, DefaultCooldown)
	r.breakers[provider] = b
	return b
}

// Reset drops every breaker. It exists for test isolation; production never
// resets — the process-wide registry accumulates per-provider circuits for the
// life of the run.
func (r *Registry) Reset() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.breakers = make(map[string]*Breaker)
}

// DefaultRegistry is the process-wide registry the llmclient integration reads
// to find each provider's circuit.
var DefaultRegistry = NewRegistry()
