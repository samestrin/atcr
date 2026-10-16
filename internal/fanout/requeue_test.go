package fanout

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/samestrin/atcr/internal/llmclient"
	"github.com/samestrin/atcr/internal/payload"
	"github.com/samestrin/atcr/internal/registry"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeUnitClock runs a unit's backoff in no real time: After advances the clock
// by d and fires at once. Time passes only while some worker waits on a backoff,
// so a call can see the clock jump forward while other units are in flight.
type fakeUnitClock struct {
	mu  sync.Mutex
	now time.Time
}

func newFakeUnitClock() *fakeUnitClock { return &fakeUnitClock{now: time.Unix(0, 0)} }

func (c *fakeUnitClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeUnitClock) After(d time.Duration) <-chan time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	if d > 0 {
		c.now = c.now.Add(d)
	}
	ch := make(chan time.Time, 1)
	ch <- c.now
	return ch
}

// elapsed is how far the fake clock has run since it started.
func (c *fakeUnitClock) elapsed() time.Duration { return c.Now().Sub(time.Unix(0, 0)) }

// stuckUnitClock never lets a backoff end, so a requeued unit waits until the
// run is cancelled.
type stuckUnitClock struct{}

func (stuckUnitClock) Now() time.Time                       { return time.Unix(0, 0) }
func (stuckUnitClock) After(time.Duration) <-chan time.Time { return nil }

func withUnitClock(c unitClock) EngineOption { return func(e *Engine) { e.clock = c } }

func withUnitRetryPolicy(p unitRetryPolicy) EngineOption { return func(e *Engine) { e.retry = p } }

// retryEveryUnit covers every slot with the outer retry, with no ledger.
func retryEveryUnit() EngineOption { return WithUnitRetry(func(int) bool { return true }, nil) }

// exhausted429 is what a 429 looks like after llmclient's per-call budget gave
// up on it: the *HTTPStatusError under the exhausted-retries wrap.
func exhausted429() error {
	return fmt.Errorf("exhausted retries: %w", &llmclient.HTTPStatusError{Status: 429, Snippet: "rate limited"})
}

func exhausted503() error {
	return fmt.Errorf("exhausted retries: %w", &llmclient.HTTPStatusError{Status: 503})
}

// rateLimitedFor answers 429 (after the per-call budget, as llmclient reports
// it) to every call made before window has passed on clock, and reviews the
// chunk afterwards.
type rateLimitedFor struct {
	clock  *fakeUnitClock
	window time.Duration
	calls  atomic.Int32
	limits atomic.Int32
}

func (c *rateLimitedFor) Complete(ctx context.Context, inv llmclient.Invocation) (string, error) {
	c.calls.Add(1)
	if c.clock.elapsed() < c.window {
		c.limits.Add(1)
		return "", exhausted429()
	}
	return baselineChunkFindingCompleter{}.Complete(ctx, inv)
}

// sixteenBaselineSlots is two personas × eight baseline bins, each bin one file.
func sixteenBaselineSlots(t *testing.T) []Slot {
	t.Helper()
	cfg := twoAgentConfig("http://unused")
	cfg.Settings.PayloadByteBudget = 100
	entries, _ := eightBinEntries()
	slots, _, err := buildSlots(cfg, baselinePayloads(entries), ReviewRange{}, string(payload.ModeFiles), "", false, true)
	require.NoError(t, err)
	require.Len(t, slots, 16)
	return slots
}

// ledgerEngine runs slots through an engine wired to a fresh chunk ledger the
// way runEngine wires it, with the fake clock, and returns the results and the
// pool dir holding chunks.json.
func ledgerEngine(t *testing.T, c Completer, slots []Slot, retry bool, opts ...EngineOption) ([]Result, string) {
	t.Helper()
	poolDir := t.TempDir()
	ledger := newChunkLedger(slog.New(slog.DiscardHandler), poolDir, slots)
	require.NotNil(t, ledger)
	ledger.writePlan()
	opts = append([]EngineOption{WithMaxParallel(3), WithSlotStart(ledger.slotStart), WithSlotDone(ledger.slotDone)}, opts...)
	if retry {
		opts = append(opts, WithUnitRetry(ledger.retryable, ledger.slotRequeue))
	}
	return NewEngine(c, opts...).Run(context.Background(), slots), poolDir
}

// T3 success criterion (Epic 35.16.14): a provider that rate-limits for 90
// seconds and then recovers costs the run no unit. Every bin is requeued on its
// primary until the window passes and the run ends fully covered. Without the
// outer loop the same provider loses every bin, because the per-call budget
// (six attempts, about 6.6s of backoff) is spent long before the window ends —
// the fake answers each call as llmclient reports an exhausted 429.
func TestUnitRetry_RateLimitedFor90sThenRecovers_LosesNoUnit(t *testing.T) {
	t.Parallel()
	slots := sixteenBaselineSlots(t)

	t.Run("without the outer loop every unit is lost", func(t *testing.T) {
		clock := newFakeUnitClock()
		c := &rateLimitedFor{clock: clock, window: 90 * time.Second}
		results, poolDir := ledgerEngine(t, c, slots, false, withUnitClock(clock))
		for i, r := range results {
			assert.Equalf(t, StatusFailed, r.Status, "unit %d", i)
		}
		for _, agent := range []string{"greta", "kai"} {
			for _, b := range readChunkRecord(t, poolDir, agent).Bins {
				assert.Equal(t, StatusFailed, b.Outcome)
			}
		}
	})

	t.Run("with it the run is fully covered", func(t *testing.T) {
		clock := newFakeUnitClock()
		c := &rateLimitedFor{clock: clock, window: 90 * time.Second}
		results, poolDir := ledgerEngine(t, c, slots, true, withUnitClock(clock))

		// Each unit's 429s are exactly its requeues. How many a unit takes
		// depends on when it was first leased: the window can pass while other
		// units back off, and once the provider is rate-limited the lease
		// throttle (T4) holds it to one lease, so later units may first be leased
		// after the window and take none.
		requeues := map[string]int{}
		attemptsMax := map[string]int{}
		var total int32
		for i, r := range results {
			require.Equalf(t, StatusOK, r.Status, "unit %d", i)
			assert.Equalf(t, slots[i].Primary.Name, r.Agent, "results[%d] stays aligned", i)
			assert.False(t, r.FallbackUsed)
			if r.Requeues > 0 {
				assert.Equalf(t, r.Requeues+1, r.UnitAttempts, "unit %d", i)
			}
			total += int32(r.Requeues)
			requeues[r.Agent] += r.Requeues
			attemptsMax[r.Agent] = max(attemptsMax[r.Agent], r.UnitAttempts)
		}
		assert.Equal(t, total, c.limits.Load(), "every 429 was requeued, none was final")
		assert.Positive(t, total, "the first leases fall inside the window")
		assert.GreaterOrEqual(t, clock.elapsed(), 90*time.Second)
		for _, agent := range []string{"greta", "kai"} {
			rec := readChunkRecord(t, poolDir, agent)
			require.Len(t, rec.Bins, 8)
			for i, b := range rec.Bins {
				assert.Equal(t, StatusOK, b.Outcome, "%s bin %d", agent, b.Index)
				assert.Equal(t, results[slotOf(t, slots, agent, i)].Requeues+1, b.Attempts, "%s bin %d counts every lease", agent, b.Index)
			}
		}

		// status.json: per-agent requeues summed across the persona's bins, and the
		// most leases any one bin took.
		for _, merged := range mergeChunkResults(results) {
			st := statusFor(merged, findingsResult{})
			assert.Equal(t, requeues[merged.Agent], st.Requeues, merged.Agent)
			assert.Equal(t, attemptsMax[merged.Agent], st.AttemptsMax, merged.Agent)
		}
	})
}

// slotOf is the index of agent's bin-th slot.
func slotOf(t *testing.T, slots []Slot, agent string, bin int) int {
	t.Helper()
	for i, s := range slots {
		if s.Primary.Name == agent {
			if bin == 0 {
				return i
			}
			bin--
		}
	}
	t.Fatalf("no bin for %s", agent)
	return -1
}

// The two budgets are the named constants, at the values the plan fixed.
func TestUnitRetry_BudgetConstants(t *testing.T) {
	t.Parallel()
	assert.Equal(t, 4, unitMaxAttempts)
	assert.Equal(t, 10*time.Minute, unitBackoffBudget)
	p := defaultUnitRetryPolicy()
	assert.Equal(t, unitMaxAttempts, p.maxAttempts)
	assert.Equal(t, unitBackoffBudget, p.budget)
	// The default schedule fits every requeue inside the backoff budget, so the
	// attempt budget is what ends a persistently failing unit.
	for _, c := range []failureClass{failureRateLimit, failureServer, failureTransport, failureTimeout} {
		var spent time.Duration
		for attempt := 1; attempt < unitMaxAttempts; attempt++ {
			d, ok := p.next(c, attempt, spent)
			require.True(t, ok, "%s attempt %d", c, attempt)
			spent += d
		}
		_, ok := p.next(c, unitMaxAttempts, spent)
		assert.False(t, ok, "%s: no lease past the attempt budget", c)
		assert.LessOrEqual(t, spent, unitBackoffBudget)
	}
	assert.Equal(t, 75*time.Second, unitBackoff(failureRateLimit, 1))
	assert.Equal(t, 300*time.Second, unitBackoff(failureRateLimit, 3))
	assert.Equal(t, 20*time.Second, unitBackoff(failureServer, 1))
}

// A unit that never recovers is leased exactly unitMaxAttempts times, then
// marked failed with one warn line, and the ledger holds every lease.
func TestUnitRetry_ExhaustingAttemptsMarksFailedWithOneWarn(t *testing.T) {
	t.Parallel()
	slots := sixteenBaselineSlots(t)
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&lockedWriter{w: &buf}, nil))
	clock := newFakeUnitClock()
	c := &rateLimitedFor{clock: clock, window: 24 * time.Hour}

	results, poolDir := ledgerEngine(t, c, slots, true, withUnitClock(clock), WithLogger(logger))

	for i, r := range results {
		assert.Equalf(t, StatusFailed, r.Status, "unit %d", i)
		assert.Equalf(t, unitMaxAttempts, r.UnitAttempts, "unit %d", i)
		assert.Equalf(t, unitMaxAttempts-1, r.Requeues, "unit %d", i)
	}
	assert.Equal(t, int32(16*unitMaxAttempts), c.calls.Load())
	assert.Equal(t, 16, strings.Count(buf.String(), "unit exhausted its retry budget"), "one warn line per exhausted unit")
	assert.Equal(t, 16, strings.Count(buf.String(), "level=WARN"), "and no other warn line")
	for _, agent := range []string{"greta", "kai"} {
		for _, b := range readChunkRecord(t, poolDir, agent).Bins {
			assert.Equal(t, StatusFailed, b.Outcome)
			assert.Equal(t, unitMaxAttempts, b.Attempts)
		}
	}
}

// The backoff budget ends a unit on its own: with backoffs of six minutes, the
// second requeue would pass ten minutes of waiting, so the unit fails after two
// leases even though the attempt budget allows four.
func TestUnitRetry_ExhaustingBackoffBudgetMarksFailed(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&lockedWriter{w: &buf}, nil))
	fake := newFake()
	fake.failFor["a"] = exhausted503()
	policy := defaultUnitRetryPolicy()
	policy.backoff = func(failureClass, int) time.Duration { return 6 * time.Minute }

	results := NewEngine(fake, retryEveryUnit(), withUnitRetryPolicy(policy), withUnitClock(newFakeUnitClock()), WithLogger(logger)).
		Run(context.Background(), []Slot{agentSlot("a")})

	require.Len(t, results, 1)
	assert.Equal(t, StatusFailed, results[0].Status)
	assert.Equal(t, 2, results[0].UnitAttempts)
	assert.Equal(t, 1, results[0].Requeues)
	assert.Equal(t, 2, fake.calls["a"])
	assert.Equal(t, 1, strings.Count(buf.String(), "unit exhausted its retry budget"))
	assert.Contains(t, buf.String(), "class=server_error")
}

// A timeout that exhausts the budget is marked failed too, not left timeout.
func TestUnitRetry_ExhaustedTimeoutIsMarkedFailed(t *testing.T) {
	t.Parallel()
	fake := newFake()
	fake.failFor["a"] = fmt.Errorf("call: %w", context.DeadlineExceeded)
	results := NewEngine(fake, retryEveryUnit(), withUnitClock(newFakeUnitClock())).
		Run(context.Background(), []Slot{agentSlot("a")})
	assert.Equal(t, StatusFailed, results[0].Status)
	assert.Equal(t, unitMaxAttempts, fake.calls["a"])
}

// A 429 requeues the unit on its primary without walking the fallback chain;
// the backup is never called while the unit still has budget.
func TestUnitRetry_RateLimitRequeuesOnPrimaryWithoutFallback(t *testing.T) {
	t.Parallel()
	clock := newFakeUnitClock()
	limited := &rateLimitedFor{clock: clock, window: 90 * time.Second}
	backup := newFake()
	c := modelRouter{"primary": limited, "backup": backup}
	slot := agentSlot("primary")
	slot.Fallbacks = []Agent{{Name: "backup", Invocation: llmclient.Invocation{Model: "backup"}}}

	results := NewEngine(c, retryEveryUnit(), withUnitClock(clock)).Run(context.Background(), []Slot{slot})

	require.Equal(t, StatusOK, results[0].Status)
	assert.False(t, results[0].FallbackUsed, "served by the primary once the window passed")
	assert.Equal(t, 0, backup.calls["backup"], "the fallback chain did not absorb the rate limit")
	assert.Equal(t, 2, results[0].Requeues)
}

// On its last lease a rate-limited unit has nothing to requeue for, so it walks
// the chain as it always did.
func TestUnitRetry_LastLeaseRateLimitStillWalksTheChain(t *testing.T) {
	t.Parallel()
	clock := newFakeUnitClock()
	limited := &rateLimitedFor{clock: clock, window: 24 * time.Hour}
	backup := newFake()
	slot := agentSlot("primary")
	slot.Fallbacks = []Agent{{Name: "backup", Invocation: llmclient.Invocation{Model: "backup"}}}

	results := NewEngine(modelRouter{"primary": limited, "backup": backup}, retryEveryUnit(), withUnitClock(clock)).
		Run(context.Background(), []Slot{slot})

	require.Equal(t, StatusOK, results[0].Status)
	assert.True(t, results[0].FallbackUsed)
	assert.Equal(t, 1, backup.calls["backup"], "only the final lease reached the backup")
	assert.Equal(t, int32(unitMaxAttempts), limited.calls.Load())
}

// A 5xx walks the chain first: a backup that answers serves the unit with no
// requeue.
func TestUnitRetry_ServerErrorWalksTheChainFirst(t *testing.T) {
	t.Parallel()
	fake := newFake()
	fake.failFor["primary"] = exhausted503()
	slot := agentSlot("primary")
	slot.Fallbacks = []Agent{{Name: "backup", Invocation: llmclient.Invocation{Model: "backup"}}}

	results := NewEngine(fake, retryEveryUnit(), withUnitClock(newFakeUnitClock())).Run(context.Background(), []Slot{slot})

	require.Equal(t, StatusOK, results[0].Status)
	assert.True(t, results[0].FallbackUsed)
	assert.Zero(t, results[0].Requeues)
	assert.Equal(t, 1, fake.calls["primary"])
}

// When the whole chain fails with a 5xx the unit is requeued, and comes back to
// its primary.
func TestUnitRetry_ChainWideServerErrorRequeues(t *testing.T) {
	t.Parallel()
	clock := newFakeUnitClock()
	outage := &outageFor{clock: clock, window: 15 * time.Second}
	slot := agentSlot("primary")
	slot.Fallbacks = []Agent{{Name: "backup", Invocation: llmclient.Invocation{Model: "backup"}}}

	results := NewEngine(outage, retryEveryUnit(), withUnitClock(clock)).Run(context.Background(), []Slot{slot})

	require.Equal(t, StatusOK, results[0].Status)
	assert.False(t, results[0].FallbackUsed, "after the 20s backoff the primary served it")
	assert.Equal(t, 1, results[0].Requeues)
	assert.Equal(t, int32(3), outage.calls.Load(), "primary + backup, then primary again")
}

// A failure a later attempt would repeat resolves on the first lease.
func TestUnitRetry_FinalFailureIsNotRequeued(t *testing.T) {
	t.Parallel()
	fake := newFake()
	fake.failFor["a"] = &llmclient.HTTPStatusError{Status: 400}
	results := NewEngine(fake, retryEveryUnit(), withUnitClock(newFakeUnitClock())).Run(context.Background(), []Slot{agentSlot("a")})
	assert.Equal(t, StatusFailed, results[0].Status)
	assert.Equal(t, 1, fake.calls["a"])
	assert.Zero(t, results[0].Requeues)
	assert.Zero(t, results[0].UnitAttempts)
}

// A unit not covered by the retry resolves on its first failure, as before T3.
func TestUnitRetry_UncoveredUnitIsNotRequeued(t *testing.T) {
	t.Parallel()
	fake := newFake()
	fake.failFor["a"] = exhausted429()
	results := NewEngine(fake, WithUnitRetry(func(int) bool { return false }, nil), withUnitClock(newFakeUnitClock())).
		Run(context.Background(), []Slot{agentSlot("a")})
	assert.Equal(t, StatusFailed, results[0].Status)
	assert.Equal(t, 1, fake.calls["a"])
}

// The serial lane requeues too, on its single worker: the requeued unit waits
// behind the lane's other units, which still never overlap.
func TestUnitRetry_SerialLaneRequeues(t *testing.T) {
	t.Parallel()
	clock := newFakeUnitClock()
	c := &rateLimitedFor{clock: clock, window: 60 * time.Second}
	var slots []Slot
	for _, n := range []string{"s0", "s1", "s2"} {
		s := agentSlot(n)
		s.Serial = true
		slots = append(slots, s)
	}
	results := NewEngine(c, retryEveryUnit(), withUnitClock(clock)).Run(context.Background(), slots)
	for i, r := range results {
		assert.Equalf(t, StatusOK, r.Status, "unit %d", i)
		assert.Equalf(t, slots[i].Primary.Name, r.Agent, "results[%d] stays aligned", i)
		assert.Equal(t, 1, r.Requeues)
	}
}

// A run cancelled while a unit waits out its backoff does not hang, and the
// unit resolves with its last real failure rather than a synthesized timeout.
func TestUnitRetry_CancelDuringBackoffResolvesWithLastFailure(t *testing.T) {
	t.Parallel()
	fake := newFake()
	fake.failFor["a"] = exhausted429()
	var requeued atomic.Int32
	ctx, cancel := context.WithCancel(context.Background())
	e := NewEngine(fake, WithUnitRetry(func(int) bool { return true }, func(int, Result) {
		requeued.Add(1)
		cancel()
	}), withUnitClock(stuckUnitClock{}))

	done := make(chan []Result)
	go func() { done <- e.Run(ctx, []Slot{agentSlot("a")}) }()
	select {
	case results := <-done:
		assert.Equal(t, StatusFailed, results[0].Status)
		var he *llmclient.HTTPStatusError
		require.True(t, errors.As(results[0].Err, &he))
		assert.Equal(t, 429, he.Status)
		assert.Equal(t, 1, results[0].Requeues)
	case <-time.After(10 * time.Second):
		t.Fatal("Run hung on a unit waiting out its backoff after cancellation")
	}
	assert.Equal(t, int32(1), requeued.Load())
	assert.Equal(t, 1, fake.calls["a"])
}

// A requeued unit is recorded pending with the failed lease counted, so a run
// killed during the backoff leaves it open and its attempts on disk.
func TestChunkLedger_RequeueRecordsPendingWithAttempt(t *testing.T) {
	t.Parallel()
	slots := sixteenBaselineSlots(t)
	poolDir := t.TempDir()
	ledger := newChunkLedger(slog.New(slog.DiscardHandler), poolDir, slots)
	require.NotNil(t, ledger)
	ledger.writePlan()
	require.True(t, ledger.recorded(0))
	require.False(t, ledger.recorded(len(slots)))

	ledger.slotStart(0)
	ledger.slotDone(0, Result{Status: StatusOK, servedRePacked: true, servedChunkFiles: []string{"f0.go"}})
	ledger.slotStart(0)
	ledger.slotRequeue(0, Result{Status: StatusFailed})

	b := readChunkRecord(t, poolDir, slots[0].Primary.Name).Bins[0]
	assert.Equal(t, chunkOutcomePending, b.Outcome)
	assert.Equal(t, 2, b.Attempts)
	assert.False(t, b.RePacked, "the earlier completion's outcome is cleared")
	assert.Nil(t, b.ServedFiles)
	assert.True(t, b.open())
}

// The new status.json fields are omitempty: an agent that never requeued writes
// exactly the bytes it wrote before T3.
func TestAgentStatus_RequeueFieldsOmittedWhenZero(t *testing.T) {
	t.Parallel()
	data, err := json.Marshal(statusFor(Result{Agent: "a", Status: StatusOK}, findingsResult{}))
	require.NoError(t, err)
	assert.NotContains(t, string(data), "requeues")
	assert.NotContains(t, string(data), "attempts_max")

	data, err = json.Marshal(statusFor(Result{Agent: "a", Status: StatusOK, Requeues: 2, UnitAttempts: 3}, findingsResult{}))
	require.NoError(t, err)
	assert.Contains(t, string(data), `"requeues":2,"attempts_max":3`)
}

func TestClassifyUnitFailure(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		err  error
		want failureClass
	}{
		{"nil", nil, failureFinal},
		{"429 after per-call retries", exhausted429(), failureRateLimit},
		{"503 after per-call retries", exhausted503(), failureServer},
		{"500", &llmclient.HTTPStatusError{Status: 500}, failureServer},
		{"401", &llmclient.HTTPStatusError{Status: 401}, failureFinal},
		{"deadline", fmt.Errorf("x: %w", context.DeadlineExceeded), failureTimeout},
		{"cancelled", context.Canceled, failureFinal},
		{"transport", fmt.Errorf("request failed: %w", &url.Error{Op: "Post", URL: "http://x", Err: errors.New("connection reset")}), failureTransport},
		{"circuit open", &llmclient.CircuitOpenError{}, failureTransport},
		{"truncated", errTruncatedZeroFindings, failureFinal},
		{"empty", errEmptyResponse, failureFinal},
	}
	for _, tc := range cases {
		assert.Equal(t, tc.want, classifyUnitFailure(tc.err), tc.name)
	}
}

// A bulk run is never covered by the retry: runEngine wires it only with a
// ledger, and a bulk run has none, so its output cannot change.
func TestRunEngine_BulkRunHasNoLedgerSoNoRequeue(t *testing.T) {
	t.Parallel()
	slots := []Slot{agentSlot("a"), agentSlot("b")}
	assert.Nil(t, newChunkLedger(slog.New(slog.DiscardHandler), t.TempDir(), slots))
	fake := newFake()
	fake.failFor["a"] = exhausted429()
	results, _ := runEngine(context.Background(), fake, &PreparedReview{Slots: slots, TimeoutSec: registry.DefaultTimeoutSecs}, t.TempDir())
	assert.Equal(t, StatusFailed, results[0].Status)
	assert.Equal(t, 1, fake.calls["a"], "a bulk unit is never requeued")
	assert.Zero(t, results[0].Requeues)
}

// modelRouter sends each call to the completer registered for its model.
type modelRouter map[string]Completer

func (m modelRouter) Complete(ctx context.Context, inv llmclient.Invocation) (string, error) {
	return m[inv.Model].Complete(ctx, inv)
}

// outageFor answers 503 to every model until window has passed on clock.
type outageFor struct {
	clock  *fakeUnitClock
	window time.Duration
	calls  atomic.Int32
}

func (c *outageFor) Complete(_ context.Context, inv llmclient.Invocation) (string, error) {
	c.calls.Add(1)
	if c.clock.elapsed() < c.window {
		return "", exhausted503()
	}
	return "NO FINDINGS", nil
}

// lockedWriter serialises concurrent log writes into one buffer.
type lockedWriter struct {
	mu sync.Mutex
	w  *bytes.Buffer
}

func (l *lockedWriter) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.w.Write(p)
}

// manualUnitClock lets a test see every wait the queue asks for and decide when
// it ends: After reports d on asked and fires only when the test calls fire.
type manualUnitClock struct {
	mu    sync.Mutex
	now   time.Time
	asked chan time.Duration
	wake  chan time.Time
}

func (c *manualUnitClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *manualUnitClock) After(d time.Duration) <-chan time.Time {
	c.asked <- d
	return c.wake
}

// fire moves the clock forward by d and ends the pending wait.
func (c *manualUnitClock) fire(d time.Duration) {
	c.mu.Lock()
	c.now = c.now.Add(d)
	now := c.now
	c.mu.Unlock()
	c.wake <- now
}

// Units requeued under different failure classes back off for different
// times. A worker with nothing pending waits only until the EARLIEST backoff
// ends, not the first one enqueued: a 300s rate-limit wait enqueued ahead of a
// 20s outage wait must not hold the 20s unit for five minutes.
func TestUnitQueue_WaitsForTheEarliestBackoffNotTheFirstEnqueued(t *testing.T) {
	t.Parallel()
	clock := &manualUnitClock{now: time.Unix(0, 0), asked: make(chan time.Duration, 4), wake: make(chan time.Time, 1)}
	q := newUnitQueue([]int{0, 1}, clock)
	ctx := context.Background()
	for range 2 {
		_, ok := q.lease(ctx)
		require.True(t, ok)
	}
	q.requeue(0, unitBackoff(failureRateLimit, 3), Result{}) // 300s, enqueued first
	q.requeue(1, unitBackoff(failureServer, 1), Result{})    // 20s

	got := make(chan int, 1)
	go func() {
		i, ok := q.lease(ctx)
		require.True(t, ok)
		got <- i
	}()
	select {
	case d := <-clock.asked:
		assert.Equal(t, 20*time.Second, d, "the worker waits for the earliest backoff")
	case <-time.After(10 * time.Second):
		t.Fatal("lease never waited on a backoff")
	}
	clock.fire(20 * time.Second)
	select {
	case i := <-got:
		assert.Equal(t, 1, i, "the shorter-backoff unit is leased first")
	case <-time.After(10 * time.Second):
		t.Fatal("lease did not return after the earliest backoff ended")
	}

	// The 300s unit is still delayed: the next lease waits out the rest of it.
	go func() {
		i, ok := q.lease(ctx)
		require.True(t, ok)
		got <- i
	}()
	assert.Equal(t, 280*time.Second, <-clock.asked)
	clock.fire(280 * time.Second)
	assert.Equal(t, 0, <-got)
}
