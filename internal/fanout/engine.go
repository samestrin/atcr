package fanout

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/samestrin/atcr/internal/llmclient"
	"github.com/samestrin/atcr/internal/payload"
)

// Completer abstracts the LLM chat call so the engine can be driven by a fake in
// tests (deterministic concurrency/fallback assertions) while production uses
// *llmclient.Client. The engine consumes the interface; the client returns a
// concrete type.
type Completer interface {
	Complete(ctx context.Context, inv llmclient.Invocation) (string, error)
}

// Agent is a fully-resolved reviewer ready to invoke: the LLM call parameters,
// the rendered persona prompt, the payload mode it saw, and any byte-budget
// truncation recorded for its status.json.
type Agent struct {
	Name        string
	Invocation  llmclient.Invocation
	Prompt      string
	PayloadMode string
	Truncation  payload.Truncation
	// TimeoutSecs bounds this single agent's call within the global deadline; 0
	// means "use the global context deadline only".
	TimeoutSecs int
}

// Slot is one reviewer position in the roster: a primary agent plus its
// resolved fallback chain (each a full Agent). Serial marks slots that must run
// sequentially in the serial lane (rate-limited providers). The engine tries
// Primary, then each Fallback in order, until one succeeds.
type Slot struct {
	Primary   Agent
	Fallbacks []Agent
	Serial    bool
}

// Result is the outcome of one slot after fallback resolution. Status is one of
// StatusOK/StatusFailed/StatusTimeout. FallbackUsed/FallbackFrom record which
// agent actually answered when a fallback took over. Agent names the agent that
// produced the result (the primary's name — attribution follows the slot, not
// the substitute), while FallbackFrom records the primary when a fallback ran.
type Result struct {
	Agent        string
	Content      string
	Status       string
	Err          error
	DurationMS   int64
	FallbackUsed bool
	FallbackFrom string
	PayloadMode  string
	Truncation   payload.Truncation
}

// Engine fans a review out to a roster across a parallel lane (default) and a
// serial lane (rate-limited providers), both running concurrently.
type Engine struct {
	completer Completer
}

// NewEngine builds an Engine over the given completer. A nil completer is a
// programming error and panics at construction rather than nil-panicking deep
// inside the first agent invocation.
func NewEngine(c Completer) *Engine {
	if c == nil {
		panic("fanout: NewEngine called with nil Completer")
	}
	return &Engine{completer: c}
}

// Run executes every slot and returns one Result per slot in input order.
// Parallel-lane slots run concurrently via a WaitGroup; serial-lane slots run
// sequentially in a single goroutine (ctx checked before each invocation),
// concurrent with the parallel lane. The WaitGroup always drains — even when
// ctx is cancelled mid-flight — so no goroutine is leaked. A cancelled context
// surfaces as StatusTimeout for the affected slots; other slots still complete.
func (e *Engine) Run(ctx context.Context, slots []Slot) []Result {
	start := time.Now()
	results := make([]Result, len(slots))
	var wg sync.WaitGroup

	// Serial slots share one goroutine so they never overlap; parallel slots
	// each get their own. Both lanes start together and the WaitGroup joins them.
	var serialIdx []int
	for i, s := range slots {
		if s.Serial {
			serialIdx = append(serialIdx, i)
			continue
		}
		wg.Add(1)
		go func(i int, s Slot) {
			defer wg.Done()
			results[i] = e.invokeSlot(ctx, s)
		}(i, slots[i])
	}

	if len(serialIdx) > 0 {
		wg.Add(1)
		go func(slots []Slot, serialIdx []int) {
			defer wg.Done()
			for _, i := range serialIdx {
				s := slots[i]
				// Honor cancellation before starting each serial invocation so a
				// cancelled run does not keep firing requests down the lane. The
				// short-circuited slot records the wall-clock elapsed since Run
				// started, not 0 — real time passed before the cancellation.
				if err := ctx.Err(); err != nil {
					results[i] = Result{
						Agent:       s.Primary.Name,
						Status:      classifyStatus(err),
						Err:         err,
						DurationMS:  time.Since(start).Milliseconds(),
						PayloadMode: s.Primary.PayloadMode,
						Truncation:  s.Primary.Truncation,
					}
					continue
				}
				results[i] = e.invokeSlot(ctx, s)
			}
		}(slots, serialIdx)
	}

	wg.Wait()
	return results
}

// invokeSlot tries the primary agent, then each fallback in order, until one
// succeeds. The first success wins; if all fail, the last real failure is
// reported with the slot marked failed (or timeout when ctx expired before any
// attempt ran). Attribution stays with the primary agent name;
// FallbackUsed/FallbackFrom record the substitution. On failure the primary's
// payload provenance is always recorded so status.json reflects the slot, not a
// substitute that may have seen a different payload.
func (e *Engine) invokeSlot(ctx context.Context, s Slot) Result {
	chain := append([]Agent{s.Primary}, s.Fallbacks...)
	var last Result
	for i, a := range chain {
		// Stop descending the chain once the context is done — further attempts
		// would only fail the same way and waste the remaining budget. Preserve a
		// prior real failure's diagnostics; only synthesize a timeout/cancel
		// result when no attempt has run yet.
		if err := ctx.Err(); err != nil {
			if last.Err == nil {
				last = Result{Agent: s.Primary.Name, Status: classifyStatus(err), Err: err}
			}
			break
		}
		r := e.invokeAgent(ctx, a)
		if i > 0 {
			r.FallbackUsed = true
			r.FallbackFrom = s.Primary.Name
			r.Agent = s.Primary.Name // attribution follows the slot, not the substitute
		}
		if r.Status == StatusOK {
			return r
		}
		last = r
	}
	// Slot failed: stamp the primary's identity and payload provenance.
	last.Agent = s.Primary.Name
	last.PayloadMode = s.Primary.PayloadMode
	last.Truncation = s.Primary.Truncation
	return last
}

// invokeAgent performs a single agent's LLM call and classifies the outcome on
// the actual error returned (not ambient ctx state): a context deadline/cancel
// surfaces as StatusTimeout, any other error as StatusFailed. Classifying on the
// error itself avoids mislabeling a genuine failure (auth, malformed response)
// as a timeout just because a sibling slot's deadline happened to fire. The raw
// assistant content is returned on success for the artifact layer to persist.
func (e *Engine) invokeAgent(ctx context.Context, a Agent) Result {
	// A per-agent timeout further bounds this call within the global deadline;
	// when it fires only this agent times out — siblings keep running.
	if a.TimeoutSecs > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, time.Duration(a.TimeoutSecs)*time.Second)
		defer cancel()
	}
	start := time.Now()
	content, err := e.completer.Complete(ctx, a.Invocation)
	dur := time.Since(start).Milliseconds()
	r := Result{
		Agent:       a.Name,
		DurationMS:  dur,
		PayloadMode: a.PayloadMode,
		Truncation:  a.Truncation,
	}
	if err != nil {
		r.Err = err
		r.Status = classifyStatus(err)
		return r
	}
	r.Content = content
	r.Status = StatusOK
	return r
}

// classifyStatus maps an error to a status code. A context deadline or
// cancellation (anywhere in the wrap chain) is a timeout; everything else is a
// genuine failure.
func classifyStatus(err error) string {
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return StatusTimeout
	}
	return StatusFailed
}
