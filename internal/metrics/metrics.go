// Package metrics is a lightweight, in-process metrics collector: monotonic
// counters and bounded histograms aggregated in a registry, with a process-wide
// DefaultRegistry and package-level accessors. It has no external dependencies
// (Epic 4.4 Open Questions 1 & 2 — custom implementation, no third-party metrics
// library) and every exported operation is safe for concurrent use.
//
// Labels are encoded into the metric name using Prometheus syntax —
// `atcr_api_errors_total{status="429"}` is one counter, `{status="500"}` is
// another — so the registry needs no label type. The Prometheus renderer
// (prometheus.go) groups same-family keys under a single `# TYPE` header.
//
// The package-level Counter/Histogram functions are the call-site API
// (metrics.Counter("name").Inc()). The concrete counter/histogram types are
// unexported because that name space is owned by those accessor functions;
// callers hold the returned values fluently and never name the type.
package metrics

import (
	"sort"
	"sync"
	"sync/atomic"
)

// maxHistogramSamples caps the per-histogram retained sample buffer. Beyond it
// the oldest sample is overwritten (ring semantics) so a long-running process
// cannot grow histogram memory without bound (Epic 4.4 risk: unbounded growth).
// Sum and count stay exact across every observation regardless of the cap; only
// the percentile sample window is bounded.
const maxHistogramSamples = 10000

// counter is a monotonically increasing integer metric (e.g. total reviews).
// Obtain one from a Registry or the package-level Counter; the zero value is not
// registered. Safe for concurrent use.
type counter struct {
	name  string
	value atomic.Int64
}

// Inc adds one to the counter.
func (c *counter) Inc() { c.value.Add(1) }

// Add adds n to the counter. Callers pass non-negative deltas (a counter is
// monotonic); the method does not enforce it so a batch increment stays cheap.
func (c *counter) Add(n int64) { c.value.Add(n) }

// Value returns the current total.
func (c *counter) Value() int64 { return c.value.Load() }

// Name returns the metric name (including any encoded label suffix).
func (c *counter) Name() string { return c.name }

// histogram tracks the distribution of observed float64 values (e.g. latency in
// seconds). It keeps an exact running sum/count for the mean and retains up to
// maxHistogramSamples recent values for percentile queries. Safe for concurrent
// use.
type histogram struct {
	name string
	mu   sync.Mutex
	// values is the retained sample window. It grows by append until it reaches
	// maxHistogramSamples, after which writes wrap around (next) as a ring.
	values []float64
	next   int  // ring write cursor, used only once full
	full   bool // true once values reached maxHistogramSamples
	sum    float64
	count  int64
}

// Observe records one value. sum and count are updated for every observation;
// the retained sample window is bounded to maxHistogramSamples, overwriting the
// oldest sample once full so memory stays bounded.
func (h *histogram) Observe(v float64) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.sum += v
	h.count++
	if !h.full {
		h.values = append(h.values, v)
		if len(h.values) == maxHistogramSamples {
			h.full = true
			h.next = 0
		}
		return
	}
	h.values[h.next] = v
	h.next = (h.next + 1) % maxHistogramSamples
}

// Percentile returns the p-th percentile (nearest-rank) of the retained sample
// window, or 0 when nothing has been observed. p is clamped to [0, 100].
func (h *histogram) Percentile(p float64) float64 {
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.values) == 0 {
		return 0
	}
	p = min(max(p, 0), 100)
	sorted := make([]float64, len(h.values))
	copy(sorted, h.values)
	sort.Float64s(sorted)
	// Nearest-rank: rank = ceil(p/100 * N), 1-indexed, clamped to [1, N]. The
	// clamp is defensive — with p in [0,100] the rank is already in range — and
	// uses builtins so it adds no uncoverable branch.
	rank := min(max(int(ceilDiv(p, float64(len(sorted)))), 1), len(sorted))
	return sorted[rank-1]
}

// ceilDiv returns ceil(p/100 * n) without importing math: the nearest-rank index
// for percentile p over n samples.
func ceilDiv(p, n float64) float64 {
	x := p / 100 * n
	t := float64(int64(x))
	if x > t {
		return t + 1
	}
	return t
}

// Mean returns the arithmetic mean of every observed value (exact, not windowed),
// or 0 when nothing has been observed.
func (h *histogram) Mean() float64 {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.count == 0 {
		return 0
	}
	return h.sum / float64(h.count)
}

// Sum returns the exact running sum of every observed value.
func (h *histogram) Sum() float64 {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.sum
}

// Count returns the exact number of observations.
func (h *histogram) Count() int64 {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.count
}

// Name returns the metric name (including any encoded label suffix).
func (h *histogram) Name() string { return h.name }

// Registry holds all counters and histograms, each keyed by its full name
// (label suffix included). Accessors are get-or-create: the same name always
// returns the same instance, so collaborating call sites share one metric. Safe
// for concurrent use.
//
// One mutex guards both maps. The accessors hold it only for a single map
// lookup/insert; the actual metric mutations (atomic counter adds, the
// histogram's own mutex) happen outside it, so this lock is never the hot path —
// it trades a hair of lookup concurrency for a get-or-create with no
// double-checked-locking branch to leave uncovered.
type Registry struct {
	mu         sync.Mutex
	counters   map[string]*counter
	histograms map[string]*histogram
}

// NewRegistry creates an empty registry. Tests use a fresh registry for
// isolation; production uses the package-global DefaultRegistry.
func NewRegistry() *Registry {
	return &Registry{
		counters:   make(map[string]*counter),
		histograms: make(map[string]*histogram),
	}
}

// Counter returns the counter registered under name, creating it on first use.
func (r *Registry) Counter(name string) *counter {
	r.mu.Lock()
	defer r.mu.Unlock()
	c, ok := r.counters[name]
	if !ok {
		c = &counter{name: name}
		r.counters[name] = c
	}
	return c
}

// Histogram returns the histogram registered under name, creating it on first use.
func (r *Registry) Histogram(name string) *histogram {
	r.mu.Lock()
	defer r.mu.Unlock()
	h, ok := r.histograms[name]
	if !ok {
		h = &histogram{name: name}
		r.histograms[name] = h
	}
	return h
}

// Reset drops every counter and histogram. It exists for test isolation (and a
// hypothetical operator reset); production never calls it — serve-mode metrics
// are cumulative since process start.
func (r *Registry) Reset() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.counters = make(map[string]*counter)
	r.histograms = make(map[string]*histogram)
}

// DefaultRegistry is the process-wide registry the package-level accessors and
// all production instrumentation write to. A CLI process runs one review, so its
// values reflect that review; a serve process accumulates across reviews.
var DefaultRegistry = NewRegistry()

// Counter returns the named counter from DefaultRegistry (get-or-create). This
// is the call-site API: metrics.Counter("atcr_reviews_total").Inc().
func Counter(name string) *counter { return DefaultRegistry.Counter(name) }

// Histogram returns the named histogram from DefaultRegistry (get-or-create):
// metrics.Histogram("atcr_review_duration_seconds").Observe(secs).
func Histogram(name string) *histogram { return DefaultRegistry.Histogram(name) }
