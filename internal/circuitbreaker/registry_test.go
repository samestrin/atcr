package circuitbreaker

import (
	"context"
	"sync"
	"testing"
)

func TestRegistryGetCreatesAndShares(t *testing.T) {
	r := NewRegistry()
	a := r.Get("openai")
	if a == nil {
		t.Fatal("Get returned nil")
	}
	if got := a.State(); got != StateClosed {
		t.Fatalf("new breaker State() = %v, want closed", got)
	}
	b := r.Get("openai")
	if a != b {
		t.Fatal("Get(\"openai\") returned different instances; breaker is not shared")
	}
}

func TestRegistryGetDistinctProviders(t *testing.T) {
	r := NewRegistry()
	if r.Get("openai") == r.Get("anthropic") {
		t.Fatal("distinct providers share one breaker; must be per-provider")
	}
}

func TestRegistryReset(t *testing.T) {
	r := NewRegistry()
	first := r.Get("openai")
	r.Reset()
	if second := r.Get("openai"); second == first {
		t.Fatal("after Reset() Get returned the pre-reset breaker; Reset did not clear")
	}
}

func TestDefaultRegistryUsable(t *testing.T) {
	t.Cleanup(DefaultRegistry.Reset)
	if DefaultRegistry.Get("openai") == nil {
		t.Fatal("DefaultRegistry.Get returned nil")
	}
}

func TestRegistryGetConcurrentSingleInstance(t *testing.T) {
	r := NewRegistry()
	const goroutines = 32
	var wg sync.WaitGroup
	got := make([]*Breaker, goroutines)
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func(i int) {
			defer wg.Done()
			got[i] = r.Get("openai")
		}(i)
	}
	wg.Wait()
	// Every goroutine must observe the same shared instance despite the race to
	// create it.
	for i := 1; i < goroutines; i++ {
		if got[i] != got[0] {
			t.Fatalf("concurrent Get produced distinct breakers (index %d)", i)
		}
	}
}

func TestProviderContextRoundTrip(t *testing.T) {
	ctx := NewContext(context.Background(), "openai")
	if got := ProviderFromContext(ctx); got != "openai" {
		t.Fatalf("ProviderFromContext = %q, want openai", got)
	}
}

func TestProviderFromContextAbsentIsEmpty(t *testing.T) {
	if got := ProviderFromContext(context.Background()); got != "" {
		t.Fatalf("ProviderFromContext on bare context = %q, want empty", got)
	}
}

func TestProviderContextEmptyString(t *testing.T) {
	// An explicitly-empty provider round-trips as empty (the breaker no-ops).
	ctx := NewContext(context.Background(), "")
	if got := ProviderFromContext(ctx); got != "" {
		t.Fatalf("ProviderFromContext for empty provider = %q, want empty", got)
	}
}
