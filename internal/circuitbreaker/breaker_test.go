package circuitbreaker

import (
	"bytes"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/samestrin/atcr/internal/metrics"
)

// clock is a controllable time source so cooldown transitions are tested without
// real sleeps.
type clock struct{ t time.Time }

func (c *clock) now() time.Time          { return c.t }
func (c *clock) advance(d time.Duration) { c.t = c.t.Add(d) }

// newTestBreaker builds a breaker wired to a controllable clock. metrics
// DefaultRegistry is reset so gauge assertions see only this test's writes.
func newTestBreaker(t *testing.T, threshold int, cooldown time.Duration) (*Breaker, *clock) {
	t.Helper()
	metrics.DefaultRegistry.Reset()
	t.Cleanup(metrics.DefaultRegistry.Reset)
	clk := &clock{t: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
	b := New("openai", threshold, cooldown)
	b.now = clk.now
	return b, clk
}

// gaugeValue reads the current circuit-state gauge for the breaker's provider.
func gaugeValue(provider string) float64 {
	return metrics.DefaultRegistry.Gauge(
		metrics.Key(metrics.NameCircuitBreakerState, metrics.LabelProvider, provider),
	).Value()
}

func TestStateString(t *testing.T) {
	cases := map[State]string{
		StateClosed:   "closed",
		StateOpen:     "open",
		StateHalfOpen: "half-open",
		State(99):     "unknown",
	}
	for s, want := range cases {
		if got := s.String(); got != want {
			t.Errorf("State(%d).String() = %q, want %q", s, got, want)
		}
	}
}

func TestNewStartsClosed(t *testing.T) {
	b, _ := newTestBreaker(t, DefaultThreshold, DefaultCooldown)
	if got := b.State(); got != StateClosed {
		t.Fatalf("fresh breaker State() = %v, want closed", got)
	}
	if !b.Allow() {
		t.Fatal("fresh breaker Allow() = false, want true")
	}
	if got := gaugeValue("openai"); got != 0 {
		t.Fatalf("fresh breaker gauge = %v, want 0 (closed)", got)
	}
}

// AC1: after 3 consecutive failures the circuit opens.
func TestOpensAfterThresholdFailures(t *testing.T) {
	b, _ := newTestBreaker(t, 3, DefaultCooldown)
	b.RecordFailure()
	b.RecordFailure()
	if got := b.State(); got != StateClosed {
		t.Fatalf("after 2 failures State() = %v, want still closed", got)
	}
	b.RecordFailure() // third → open
	if got := b.State(); got != StateOpen {
		t.Fatalf("after 3 failures State() = %v, want open", got)
	}
	if got := gaugeValue("openai"); got != 1 {
		t.Fatalf("open gauge = %v, want 1", got)
	}
}

// AC2: when open, Allow() returns false (no call made) before the cooldown.
func TestOpenFailsFastBeforeCooldown(t *testing.T) {
	b, clk := newTestBreaker(t, 1, 60*time.Second)
	b.RecordFailure() // threshold 1 → open immediately
	if b.State() != StateOpen {
		t.Fatalf("State() = %v, want open", b.State())
	}
	clk.advance(59 * time.Second) // still within cooldown
	if b.Allow() {
		t.Fatal("Allow() = true within cooldown, want false (fail fast)")
	}
	if got := b.State(); got != StateOpen {
		t.Fatalf("State() = %v within cooldown, want open", got)
	}
}

// AC3: after the cooldown the circuit transitions to half-open.
func TestTransitionsToHalfOpenAfterCooldown(t *testing.T) {
	b, clk := newTestBreaker(t, 1, 60*time.Second)
	b.RecordFailure() // open
	clk.advance(60 * time.Second)
	if got := b.State(); got != StateHalfOpen {
		t.Fatalf("after cooldown State() = %v, want half-open", got)
	}
	if got := gaugeValue("openai"); got != 2 {
		t.Fatalf("half-open gauge = %v, want 2", got)
	}
}

// AC3 via Allow: a cooldown-elapsed open circuit admits exactly one probe.
func TestHalfOpenAdmitsSingleProbe(t *testing.T) {
	b, clk := newTestBreaker(t, 1, 60*time.Second)
	b.RecordFailure() // open
	clk.advance(60 * time.Second)
	if !b.Allow() {
		t.Fatal("first Allow() after cooldown = false, want true (the probe)")
	}
	if b.Allow() {
		t.Fatal("second Allow() while probe in flight = true, want false")
	}
}

// AC4: in half-open, one success closes the circuit.
func TestHalfOpenSuccessCloses(t *testing.T) {
	b, clk := newTestBreaker(t, 1, 60*time.Second)
	b.RecordFailure() // open
	clk.advance(60 * time.Second)
	b.Allow() // become the probe → half-open
	if b.State() != StateHalfOpen {
		t.Fatalf("State() = %v, want half-open", b.State())
	}
	b.RecordSuccess()
	if got := b.State(); got != StateClosed {
		t.Fatalf("after half-open success State() = %v, want closed", got)
	}
	if !b.Allow() {
		t.Fatal("Allow() after recovery = false, want true (closed)")
	}
	if got := gaugeValue("openai"); got != 0 {
		t.Fatalf("recovered gauge = %v, want 0", got)
	}
}

// AC5: in half-open, one failure reopens the circuit and restarts the cooldown.
func TestHalfOpenFailureReopens(t *testing.T) {
	b, clk := newTestBreaker(t, 1, 60*time.Second)
	b.RecordFailure() // open at t0
	clk.advance(60 * time.Second)
	b.Allow() // half-open at t0+60
	b.RecordFailure()
	if got := b.State(); got != StateOpen {
		t.Fatalf("after half-open failure State() = %v, want open", got)
	}
	// Cooldown restarted: still open just before a fresh 60s elapses.
	clk.advance(59 * time.Second)
	if got := b.State(); got != StateOpen {
		t.Fatalf("State() = %v, want still open (cooldown restarted)", got)
	}
	clk.advance(1 * time.Second)
	if got := b.State(); got != StateHalfOpen {
		t.Fatalf("State() = %v, want half-open after restarted cooldown", got)
	}
}

// A success in the closed state resets the consecutive-failure run so
// non-consecutive failures never accumulate to the threshold.
func TestClosedSuccessResetsFailureRun(t *testing.T) {
	b, _ := newTestBreaker(t, 3, DefaultCooldown)
	b.RecordFailure()
	b.RecordFailure()
	b.RecordSuccess() // resets the run
	b.RecordFailure()
	b.RecordFailure() // only 2 in the new run → still closed
	if got := b.State(); got != StateClosed {
		t.Fatalf("State() = %v, want closed (success reset the run)", got)
	}
}

// A success observed while open (a pre-trip in-flight call returning) clears the
// failure run but does not close the circuit early.
func TestOpenSuccessDoesNotClose(t *testing.T) {
	b, _ := newTestBreaker(t, 1, 60*time.Second)
	b.RecordFailure() // open
	b.RecordSuccess() // observed while open
	if got := b.State(); got != StateOpen {
		t.Fatalf("State() = %v, want still open (cooldown governs recovery)", got)
	}
}

// A failure observed while already open is ignored and does not extend the
// cooldown anchored at the original trip.
func TestOpenFailureDoesNotExtendCooldown(t *testing.T) {
	b, clk := newTestBreaker(t, 1, 60*time.Second)
	b.RecordFailure() // open at t0
	clk.advance(30 * time.Second)
	b.RecordFailure() // observed while open — must not re-anchor the cooldown
	clk.advance(30 * time.Second)
	if got := b.State(); got != StateHalfOpen {
		t.Fatalf("State() = %v, want half-open (original cooldown unextended)", got)
	}
}

func TestConcurrentAccessIsRaceFree(t *testing.T) {
	b, _ := newTestBreaker(t, 3, time.Millisecond)
	var wg sync.WaitGroup
	wg.Add(16)
	for i := 0; i < 16; i++ {
		go func(i int) {
			defer wg.Done()
			for j := 0; j < 500; j++ {
				if b.Allow() {
					if (i+j)%2 == 0 {
						b.RecordSuccess()
					} else {
						b.RecordFailure()
					}
				}
				_ = b.State()
			}
		}(i)
	}
	wg.Wait()
	// No assertion on the final state — the point is the -race detector sees no
	// data race and no method panics under concurrent access.
}

// Regression (independent review): a half-open probe released without a verdict
// (caller cancellation) must free the slot so a later caller can retry — the
// circuit must NOT wedge half-open forever.
func TestHalfOpenReleaseProbeReadmits(t *testing.T) {
	b, clk := newTestBreaker(t, 1, 60*time.Second)
	b.RecordFailure() // open
	clk.advance(60 * time.Second)
	if !b.Allow() {
		t.Fatal("first Allow() after cooldown = false, want true (probe)")
	}
	if b.Allow() {
		t.Fatal("second Allow() = true while probe in flight, want false")
	}
	b.ReleaseProbe() // caller cancelled — release without a verdict
	if got := b.State(); got != StateHalfOpen {
		t.Fatalf("State() = %v after ReleaseProbe, want still half-open", got)
	}
	if !b.Allow() {
		t.Fatal("Allow() after ReleaseProbe = false, want true (slot re-armed, not wedged)")
	}
	// Recovery still works: a subsequent success closes the circuit.
	b.RecordSuccess()
	if got := b.State(); got != StateClosed {
		t.Fatalf("State() = %v after recovery, want closed", got)
	}
}

// transition must emit one structured slog.Info line per state change, carrying
// provider, from-state, to-state, and failure count.
func TestTransitionEmitsStructuredLog(t *testing.T) {
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo})))
	defer slog.SetDefault(prev)

	b, clk := newTestBreaker(t, 1, 60*time.Second)
	b.RecordFailure() // closed → open (one log line expected)
	clk.advance(60 * time.Second)
	_ = b.State()     // open → half-open (one log line expected)
	b.RecordSuccess() // half-open → closed (one log line expected)

	out := buf.String()
	for _, want := range []string{"circuit breaker state change", "provider=openai"} {
		if !strings.Contains(out, want) {
			t.Errorf("log output missing %q\nfull output:\n%s", want, out)
		}
	}
	lines := strings.Count(out, "circuit breaker state change")
	if lines != 3 {
		t.Errorf("expected 3 state-change log lines (closed→open, open→half-open, half-open→closed), got %d\nfull output:\n%s", lines, out)
	}
}

// New must clamp non-positive threshold/cooldown to defaults so a caller
// passing 0 or a negative value does not produce a breaker that trips on one
// failure or recovers immediately (no cooldown).
func TestNewClampsBadThresholdAndCooldown(t *testing.T) {
	metrics.DefaultRegistry.Reset()
	defer metrics.DefaultRegistry.Reset()
	cases := []struct {
		threshold int
		cooldown  time.Duration
	}{
		{0, 0},
		{-1, -1 * time.Second},
	}
	for _, tc := range cases {
		b := New("clamp-test", tc.threshold, tc.cooldown)
		b.RecordFailure() // must NOT trip when threshold is clamped to DefaultThreshold (3)
		if got := b.State(); got != StateClosed {
			t.Errorf("New(%d, %v).RecordFailure()→State() = %v, want closed (threshold must be clamped)",
				tc.threshold, tc.cooldown, got)
		}
	}
}

// ReleaseProbe outside half-open is a harmless no-op.
func TestReleaseProbeNoOpWhenClosed(t *testing.T) {
	b, _ := newTestBreaker(t, 3, DefaultCooldown)
	b.ReleaseProbe()
	if got := b.State(); got != StateClosed {
		t.Fatalf("State() = %v, want closed (ReleaseProbe no-op)", got)
	}
	if !b.Allow() {
		t.Fatal("Allow() = false after no-op ReleaseProbe, want true")
	}
}
