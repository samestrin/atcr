package metrics

import (
	"math"
	"sync"
	"testing"
)

func TestCounterIncAddValue(t *testing.T) {
	r := NewRegistry()
	c := r.Counter("atcr_reviews_total")
	if got := c.Value(); got != 0 {
		t.Fatalf("fresh counter Value() = %d, want 0", got)
	}
	c.Inc()
	if got := c.Value(); got != 1 {
		t.Fatalf("after Inc() Value() = %d, want 1", got)
	}
	c.Add(5)
	if got := c.Value(); got != 6 {
		t.Fatalf("after Add(5) Value() = %d, want 6", got)
	}
	if got := c.Name(); got != "atcr_reviews_total" {
		t.Fatalf("Name() = %q, want %q", got, "atcr_reviews_total")
	}
}

func TestCounterGetOrCreateSameInstance(t *testing.T) {
	r := NewRegistry()
	a := r.Counter("x")
	a.Inc()
	b := r.Counter("x")
	if a != b {
		t.Fatalf("Counter(%q) returned different instances", "x")
	}
	if got := b.Value(); got != 1 {
		t.Fatalf("shared counter Value() = %d, want 1", got)
	}
}

func TestHistogramObservePercentileMean(t *testing.T) {
	r := NewRegistry()
	h := r.Histogram("atcr_review_duration_seconds")
	// Empty histogram: zero, not a panic.
	if got := h.Percentile(50); got != 0 {
		t.Fatalf("empty Percentile(50) = %v, want 0", got)
	}
	if got := h.Mean(); got != 0 {
		t.Fatalf("empty Mean() = %v, want 0", got)
	}
	if got := h.Count(); got != 0 {
		t.Fatalf("empty Count() = %d, want 0", got)
	}

	h.Observe(1.5)
	if got := h.Percentile(50); got != 1.5 {
		t.Fatalf("Percentile(50) = %v, want 1.5", got)
	}
	if got := h.Mean(); got != 1.5 {
		t.Fatalf("Mean() = %v, want 1.5", got)
	}
	if got := h.Count(); got != 1 {
		t.Fatalf("Count() = %d, want 1", got)
	}
	if got := h.Sum(); got != 1.5 {
		t.Fatalf("Sum() = %v, want 1.5", got)
	}
	if got := h.Name(); got != "atcr_review_duration_seconds" {
		t.Fatalf("Name() = %q, want %q", got, "atcr_review_duration_seconds")
	}
}

func TestHistogramPercentileDistribution(t *testing.T) {
	r := NewRegistry()
	h := r.Histogram("d")
	for _, v := range []float64{5, 1, 4, 2, 3} { // unsorted on purpose
		h.Observe(v)
	}
	// nearest-rank on sorted [1,2,3,4,5]
	cases := []struct {
		p    float64
		want float64
	}{
		{0, 1},   // clamps to rank 1
		{50, 3},  // median
		{100, 5}, // max
		{-10, 1}, // clamp low
		{200, 5}, // clamp high
	}
	for _, tc := range cases {
		if got := h.Percentile(tc.p); got != tc.want {
			t.Errorf("Percentile(%v) = %v, want %v", tc.p, got, tc.want)
		}
	}
	if got := h.Mean(); got != 3 {
		t.Errorf("Mean() = %v, want 3", got)
	}
}

func TestHistogramBoundedSamples(t *testing.T) {
	r := NewRegistry()
	h := r.Histogram("bounded")
	const n = maxHistogramSamples + 5
	for i := 0; i < n; i++ {
		h.Observe(float64(i))
	}
	// count and sum stay exact across all observations...
	if got := h.Count(); got != int64(n) {
		t.Fatalf("Count() = %d, want %d", got, n)
	}
	// ...but the retained sample buffer is capped.
	if got := len(h.values); got != maxHistogramSamples {
		t.Fatalf("retained samples = %d, want %d (capped)", got, maxHistogramSamples)
	}
	// The most recent value (n-1) survived the ring; the oldest (0) did not, so
	// the max percentile reflects recent data.
	if got := h.Percentile(100); got != float64(n-1) {
		t.Fatalf("Percentile(100) = %v, want %v", got, float64(n-1))
	}
}

func TestRegistryHistogramGetOrCreate(t *testing.T) {
	r := NewRegistry()
	a := r.Histogram("h")
	a.Observe(2)
	b := r.Histogram("h")
	if a != b {
		t.Fatalf("Histogram(%q) returned different instances", "h")
	}
	if got := b.Count(); got != 1 {
		t.Fatalf("shared histogram Count() = %d, want 1", got)
	}
}

func TestHistogramPercentileCacheInvalidatedOnObserve(t *testing.T) {
	r := NewRegistry()
	h := r.Histogram("cache_invalidation")
	h.Observe(1)
	if got := h.Percentile(100); got != 1 {
		t.Fatalf("Percentile(100) after Observe(1) = %v, want 1", got)
	}
	h.Observe(9)
	// After second Observe the cache must be invalidated; p100 must reflect 9.
	if got := h.Percentile(100); got != 9 {
		t.Fatalf("Percentile(100) after Observe(9) = %v, want 9 (stale cache?)", got)
	}
}

func TestHistogramWrapBoundary(t *testing.T) {
	r := NewRegistry()
	h := r.Histogram("wrap_boundary")
	for i := 0; i < maxHistogramSamples+1; i++ {
		h.Observe(float64(i))
	}
	if !h.full {
		t.Fatal("h.full should be true after maxHistogramSamples+1 observations")
	}
	if got := len(h.values); got != maxHistogramSamples {
		t.Fatalf("len(values) = %d, want %d", got, maxHistogramSamples)
	}
	if got := h.Count(); got != int64(maxHistogramSamples+1) {
		t.Fatalf("Count() = %d, want %d", got, maxHistogramSamples+1)
	}
}

func TestHistogramPercentileNearestRank(t *testing.T) {
	r := NewRegistry()
	h := r.Histogram("nearest_rank")
	for _, v := range []float64{1, 2, 3} {
		h.Observe(v)
	}
	// p=0: ceil(0/100*3)=0 → clamped to rank 1 → value 1
	if got := h.Percentile(0); got != 1 {
		t.Errorf("Percentile(0) = %v, want 1", got)
	}
	// p=100: ceil(100/100*3)=3 → rank 3 → value 3
	if got := h.Percentile(100); got != 3 {
		t.Errorf("Percentile(100) = %v, want 3", got)
	}
}

func TestRegistryReset(t *testing.T) {
	r := NewRegistry()
	r.Counter("c").Add(3)
	r.Histogram("h").Observe(9)
	r.Reset()
	if got := r.Counter("c").Value(); got != 0 {
		t.Fatalf("after Reset() counter Value() = %d, want 0", got)
	}
	if got := r.Histogram("h").Count(); got != 0 {
		t.Fatalf("after Reset() histogram Count() = %d, want 0", got)
	}
}

func TestPackageLevelAccessorsUseDefaultRegistry(t *testing.T) {
	DefaultRegistry.Reset()
	t.Cleanup(DefaultRegistry.Reset)

	Counter("pkg_total").Inc()
	if got := DefaultRegistry.Counter("pkg_total").Value(); got != 1 {
		t.Fatalf("package Counter did not write DefaultRegistry: got %d", got)
	}
	Histogram("pkg_hist").Observe(7)
	if got := DefaultRegistry.Histogram("pkg_hist").Count(); got != 1 {
		t.Fatalf("package Histogram did not write DefaultRegistry: got %d", got)
	}
}

func TestHistogramObserveNonFiniteIgnored(t *testing.T) {
	r := NewRegistry()
	h := r.Histogram("nan_inf_test")
	h.Observe(1.0)
	h.Observe(math.NaN())
	h.Observe(math.Inf(1))
	h.Observe(math.Inf(-1))
	h.Observe(2.0)

	if got := h.Count(); got != 2 {
		t.Fatalf("Count() = %d, want 2 (NaN/Inf must be discarded)", got)
	}
	if got := h.Sum(); math.IsNaN(got) || math.IsInf(got, 0) {
		t.Fatalf("Sum() = %v, want finite", got)
	}
	if got := h.Mean(); math.IsNaN(got) || math.IsInf(got, 0) {
		t.Fatalf("Mean() = %v, want finite", got)
	}
	if got := h.Percentile(50); math.IsNaN(got) || math.IsInf(got, 0) {
		t.Fatalf("Percentile(50) = %v, want finite", got)
	}
}

func TestConcurrentCounterAndHistogram(t *testing.T) {
	r := NewRegistry()
	const goroutines, iters = 16, 1000
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for g := 0; g < goroutines; g++ {
		go func() {
			defer wg.Done()
			for i := 0; i < iters; i++ {
				r.Counter("hits").Inc()
				r.Histogram("lat").Observe(1)
			}
		}()
	}
	wg.Wait()
	if got := r.Counter("hits").Value(); got != goroutines*iters {
		t.Fatalf("concurrent counter Value() = %d, want %d", got, goroutines*iters)
	}
	if got := r.Histogram("lat").Count(); got != goroutines*iters {
		t.Fatalf("concurrent histogram Count() = %d, want %d", got, goroutines*iters)
	}
}

func TestGaugeSetValue(t *testing.T) {
	r := NewRegistry()
	g := r.Gauge("atcr_circuit_breaker_state")
	if got := g.Value(); got != 0 {
		t.Fatalf("fresh gauge Value() = %v, want 0", got)
	}
	g.Set(1)
	if got := g.Value(); got != 1 {
		t.Fatalf("after Set(1) Value() = %v, want 1", got)
	}
	g.Set(2)
	if got := g.Value(); got != 2 {
		t.Fatalf("after Set(2) Value() = %v, want 2", got)
	}
	// A gauge can decrease (unlike a monotonic counter).
	g.Set(0)
	if got := g.Value(); got != 0 {
		t.Fatalf("after Set(0) Value() = %v, want 0", got)
	}
	if got := g.Name(); got != "atcr_circuit_breaker_state" {
		t.Fatalf("Name() = %q, want %q", got, "atcr_circuit_breaker_state")
	}
}

func TestRegistryGaugeGetOrCreate(t *testing.T) {
	r := NewRegistry()
	a := r.Gauge("g")
	a.Set(3)
	b := r.Gauge("g")
	if a != b {
		t.Fatalf("Gauge(%q) returned different instances", "g")
	}
	if got := b.Value(); got != 3 {
		t.Fatalf("shared gauge Value() = %v, want 3", got)
	}
}

func TestRegistryResetGauge(t *testing.T) {
	r := NewRegistry()
	r.Gauge("g").Set(5)
	r.Reset()
	if got := r.Gauge("g").Value(); got != 0 {
		t.Fatalf("after Reset() gauge Value() = %v, want 0", got)
	}
}

func TestPackageLevelGaugeUsesDefaultRegistry(t *testing.T) {
	DefaultRegistry.Reset()
	t.Cleanup(DefaultRegistry.Reset)
	Gauge("pkg_gauge").Set(2)
	if got := DefaultRegistry.Gauge("pkg_gauge").Value(); got != 2 {
		t.Fatalf("package Gauge did not write DefaultRegistry: got %v", got)
	}
}

func TestConcurrentGauge(t *testing.T) {
	r := NewRegistry()
	var wg sync.WaitGroup
	wg.Add(8)
	for g := 0; g < 8; g++ {
		go func() {
			defer wg.Done()
			for j := 0; j < 1000; j++ {
				r.Gauge("g").Set(float64(j))
			}
		}()
	}
	wg.Wait()
	if got := r.Gauge("g").Value(); got < 0 || got > 999 {
		t.Fatalf("concurrent gauge Value() = %v, out of range [0,999]", got)
	}
}
