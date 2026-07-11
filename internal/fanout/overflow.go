package fanout

import (
	"errors"
	"fmt"

	"github.com/samestrin/atcr/internal/payload"
)

// on_overflow policy dispatch (Epic 19.10 F4). This is the single point in the
// fan-out that answers "given this resolved policy string, what happens to an
// over-window payload?" — the dispatch that did not exist before this sprint,
// where overflow was either silently byte-shed via ApplyByteBudget or killed the
// agent outright when litellm's context_window_fallbacks was unset.
//
// This file is dispatch-only: it assumes the policy string has already been
// validated and defaulted by the registry precedence chain (Task 05) and
// consumes it as a plain string. It NEVER defaults an unknown string to chunk —
// defaulting-on-unknown belongs to config parsing, not here; an unrecognized
// value is a programmer/plumbing error and must surface loudly.

// The four recognized on_overflow policy values. Kept as untyped string
// constants so they compare directly against the resolved Settings string
// without a type conversion at the call site, mirroring how reviewStrategyChunked
// is compared in chunker.go.
const (
	// OverflowChunk (default) delivers the whole diff across N window-sized chunks
	// via the Epic 14.3 chunker (chunkDiff) — zero files dropped, model identity
	// preserved.
	OverflowChunk = "chunk"
	// OverflowTruncate sheds lowest-priority (largest) files via ApplyByteBudget
	// and flags the drop in a Truncation record — lossy, model identity preserved.
	OverflowTruncate = "truncate"
	// OverflowFallback would route the slot to a litellm any→any fallback model.
	// Recognized as valid config but not dispatched here: the substitution must be
	// recorded (F5) before it can be trusted, so this arm errors rather than
	// silently swapping the model.
	OverflowFallback = "fallback"
	// OverflowFail hard-fails the slot loudly when the payload exceeds budget.
	OverflowFail = "fail"
)

// Sentinel errors so callers (and tests) can branch on the overflow outcome with
// errors.Is rather than string matching, matching the outcome.go convention.
var (
	// ErrFallbackUnavailable is returned by the fallback arm: the policy is
	// recognized, but dispatching a model swap requires the provenance-recording
	// plumbing (F5) that this dispatch layer deliberately does not perform, so it
	// must not silently proceed as if nothing overflowed.
	ErrFallbackUnavailable = errors.New("on_overflow=fallback: model-swap fallback requires fallback-provenance recording (F5) and is not dispatched here")
	// ErrOverflowPolicyFail is returned by the fail arm: the payload exceeded the
	// budget and the configured policy is to hard-fail rather than degrade.
	ErrOverflowPolicyFail = errors.New("on_overflow=fail: payload exceeded budget and policy is fail")
)

// OverflowResult records the degradation action an overflow dispatch took, so the
// outcome is always machine-readable and never signaled by stderr alone — the
// same "always returned, never silent" discipline payload.Truncation follows
// (internal/payload/budget.go). Exactly one arm-specific field is populated per
// Action: Chunks for "chunk", Kept+Truncation for "truncate". Action is "none"
// only for the zero value; the error arms return the zero value alongside a
// non-nil error and never a partial result.
type OverflowResult struct {
	// Action is the degradation taken: "chunk", "truncate", or "none". It becomes
	// the per-agent degradation_action diagnosability field (F8) downstream.
	Action string
	// Chunks holds the window-sized diff chunks produced by the chunk arm. nil for
	// every other arm.
	Chunks []string
	// Kept holds the files that survived the truncate arm's byte shed. nil for
	// every other arm.
	Kept []payload.FileEntry
	// Truncation is the truncate arm's non-silent drop record (always returned by
	// ApplyByteBudget, so it is meaningful even when nothing was dropped). Zero
	// value for every other arm.
	Truncation payload.Truncation
}

// applyOverflowPolicy routes an already-resolved on_overflow policy string onto
// the correct existing primitive and returns an explicit OverflowResult (or a
// typed error). It delegates rather than reimplementing: the chunk arm calls
// chunkDiff and the truncate arm calls payload.ApplyByteBudget, so the two
// degradation strategies can never drift from their canonical implementations.
//
// The content is intentionally passed in both representations — diff (a rendered
// diff string, for the chunk/agent-prompt path) and entries+budget (a
// []payload.FileEntry slice, for the byte-shed path) — because chunkDiff and
// ApplyByteBudget operate on different shapes; forcing one artificial shared
// shape would obscure that. maxLines is the per-model chunk budget from
// payload.ChunkMaxLines (Task 03); budget is the per-agent effective byte budget
// from payload.EffectiveByteBudget (Task 02).
//
// Only the four known policy values are accepted. An unrecognized (or empty)
// string returns a clear error distinct from the fallback/fail sentinels, never
// a silent fallthrough to another arm.
func applyOverflowPolicy(policy, diff string, maxLines int, entries []payload.FileEntry, budget int64) (OverflowResult, error) {
	switch policy {
	case OverflowChunk:
		return OverflowResult{Action: "chunk", Chunks: chunkDiff(diff, maxLines)}, nil
	case OverflowTruncate:
		kept, t := payload.ApplyByteBudget(entries, budget)
		return OverflowResult{Action: "truncate", Kept: kept, Truncation: t}, nil
	case OverflowFallback:
		return OverflowResult{}, ErrFallbackUnavailable
	case OverflowFail:
		return OverflowResult{}, ErrOverflowPolicyFail
	default:
		return OverflowResult{}, fmt.Errorf("unrecognized on_overflow policy %q", policy)
	}
}
