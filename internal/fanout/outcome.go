package fanout

import (
	"errors"
	"fmt"
	"sort"
	"strings"
)

// Sentinel errors so the command layer can branch on the failure kind with
// errors.Is rather than string matching.
var (
	// ErrEmptyRoster is returned by Outcome when there were no slots to run.
	ErrEmptyRoster = errors.New("no agents ran: empty roster")
	// ErrAllAgentsFailed is returned by Outcome when every slot failed; the
	// wrapped message lists each agent and its reason.
	ErrAllAgentsFailed = errors.New("all agents failed")
)

// Summary aggregates a run's per-slot outcomes. Partial is true when at least
// one slot failed but at least one succeeded — the review still produced usable
// sources, recorded as partial:true in summary.json (AC 01-04).
type Summary struct {
	Total     int
	Succeeded int
	Failed    int
	Partial   bool
	// FallbackCount is the run-level tally of results served by a fallback model
	// (r.FallbackUsed). It is a sibling of Total/Succeeded/Failed, computed once in
	// summarize() so both Outcome() and writePool observe the same value without
	// re-deriving it from per-agent statuses. It counts post-merge results (one per
	// persona), so a persona whose chunks partly fell back is counted once (Epic
	// 19.10 F5).
	FallbackCount int
}

// Outcome aggregates results into a Summary and decides the run-level error.
// The contract (AC 01-04): a review fails only when EVERY slot failed — then
// ErrAllAgentsFailed is wrapped with a list of each agent and its reason (exit 1
// upstream). If at least one slot succeeded the error is nil and Summary.Partial
// flags whether any slot failed. An empty result set is a hard failure (nothing
// was reviewed), not a silent pass.
func Outcome(results []Result) (Summary, error) {
	s := summarize(results)
	if s.Total == 0 {
		return s, ErrEmptyRoster
	}
	if s.Succeeded == 0 {
		return s, fmt.Errorf("%w: %s", ErrAllAgentsFailed, formatFailures(results))
	}
	return s, nil
}

// summarize counts outcomes without deciding the run-level error, so both the
// all-fail gate (Outcome) and the pool summary.json can share the tally.
func summarize(results []Result) Summary {
	s := Summary{Total: len(results)}
	for _, r := range results {
		switch r.Status {
		case StatusOK:
			s.Succeeded++
		case StatusFailed, StatusTimeout:
			s.Failed++
		default:
			s.Failed++
		}
		// Tally fallback substitutions in the same single pass — no second loop.
		// Fail-closed: only an explicit r.FallbackUsed counts; missing/zero-value
		// provenance is treated as a non-fallback (independent) voice.
		if r.FallbackUsed {
			s.FallbackCount++
		}
	}
	s.Partial = s.Failed > 0 && s.Succeeded > 0
	return s
}

// formatFailures renders "agent (reason), agent (reason)" for the all-failed
// error, filtered to non-OK rows and sorted by agent name for deterministic
// output. The reason is the trimmed error message when present, else the status.
// Embedding r.Err verbatim is safe: llmclient never puts the API key in a URL or
// error (header-only Bearer auth) and the registry rejects base_url userinfo, so
// no credential reaches this string.
func formatFailures(results []Result) string {
	parts := make([]string, 0, len(results))
	for _, r := range results {
		if r.Status == StatusOK {
			continue
		}
		reason := r.Status
		if r.Err != nil {
			reason = r.Err.Error()
		}
		name := r.Agent
		if name == "" {
			name = "<unnamed>"
		}
		parts = append(parts, fmt.Sprintf("%s (%s)", name, reason))
	}
	sort.Strings(parts)
	return strings.Join(parts, ", ")
}
