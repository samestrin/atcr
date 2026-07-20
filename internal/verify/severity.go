// Package verify runs code-review findings through configurable pipeline
// stages (severity floor, confidence threshold, skeptic pass, voting) and
// emits scored verification results.
package verify

import reclib "github.com/samestrin/atcr/reconcile"

// meetsSeverityFloor reports whether a finding at findingSeverity is at or above
// the minSeverity floor and therefore should be verified. Comparison is
// case-insensitive via reclib.NormalizeSeverity, the canonical normalizer shared
// by every severity consumer. A finding with an empty or unknown severity is
// treated as below the floor (skipped) so an unexpected value never crashes or
// sneaks a finding past the floor (AC 02-07 EC3).
func meetsSeverityFloor(findingSeverity, minSeverity string) bool {
	fr := reclib.SeverityRank[reclib.NormalizeSeverity(findingSeverity)]
	mr := reclib.SeverityRank[reclib.NormalizeSeverity(minSeverity)]
	if fr == 0 {
		return false
	}
	return fr >= mr
}

// withinComplexityCeiling reports whether a finding's estMinutes estimate is at or
// below the executor's maxMinutes complexity ceiling and therefore eligible for a
// fix attempt (Sprint 32.1). It is the upper-bound counterpart to meetsSeverityFloor,
// kept as its own small pure predicate rather than inlined into generateFixes.
//
// Two guards make it a no-op unless there is both a real ceiling AND a real estimate:
//   - maxMinutes <= 0 means "no ceiling" (the EffectiveMaxEstimatedMinutes sentinel),
//     so every finding is within it — preserving existing single-tier config behavior
//     with no explicit opt-in.
//   - estMinutes <= 0 means "no estimate provided" (a non-numeric model output parses
//     as 0 per docs/findings-format.md; a negative value is a defensive impossibility).
//     Such a finding is NOT skipped on the ceiling basis — 0 is neither "trivially
//     cheap" nor "too complex", so it flows on to the executor rather than being
//     silently dropped by a routing hint that was never emitted.
//
// The comparison is inclusive at the boundary ("at or below"): estMinutes == maxMinutes
// is within the ceiling. It is an O(1) integer comparison with no allocation.
func withinComplexityCeiling(estMinutes, maxMinutes int) bool {
	if maxMinutes <= 0 || estMinutes <= 0 {
		return true
	}
	return estMinutes <= maxMinutes
}

// withinSeverityCeiling reports whether a finding's severity is at or below the
// executor's maxSeverity ceiling and therefore eligible for a fix attempt (Sprint
// 32.1) — the upper-bound counterpart to meetsSeverityFloor's floor. An empty
// maxSeverity is the "no ceiling" sentinel (EffectiveMaxSeverityForFix), so every
// finding is within it. Comparison is case-insensitive via the shared
// reclib.NormalizeSeverity/SeverityRank rubric. A NON-EMPTY maxSeverity whose rank
// is 0 (an unrecognizable token, e.g. a typo in an unvalidated in-memory config)
// fails CLOSED — no finding is within it — mirroring meetsSeverityFloor's rank-0
// handling, so a bad ceiling never silently routes CRITICALs to a cheap tier.
// A finding whose severity is empty or unknown (rank 0) is treated as within the
// ceiling here — the floor check (meetsSeverityFloor) already skips such findings
// before this predicate runs, so this predicate never needs to re-decide them.
func withinSeverityCeiling(findingSeverity, maxSeverity string) bool {
	nc := reclib.NormalizeSeverity(maxSeverity)
	if nc == "" {
		return true
	}
	cr := reclib.SeverityRank[nc]
	if cr == 0 {
		return false
	}
	return reclib.SeverityRank[reclib.NormalizeSeverity(findingSeverity)] <= cr
}
