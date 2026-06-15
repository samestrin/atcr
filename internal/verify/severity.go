package verify

import (
	"strings"

	"github.com/samestrin/atcr/internal/reconcile"
)

// severityRank orders the review severities so the verify stage can compare a
// finding against the configured floor. Higher is more severe. An unrecognized or
// empty token is absent from the map (rank 0) and therefore always below any real
// floor — the defensive "skip on unknown severity" behavior (AC 02-07 EC3).
var severityRank = map[string]int{
	reconcile.SevCritical: 4,
	reconcile.SevHigh:     3,
	reconcile.SevMedium:   2,
	reconcile.SevLow:      1,
}

// meetsSeverityFloor reports whether a finding at findingSeverity is at or above
// the minSeverity floor and therefore should be verified. Comparison is
// case-insensitive. A finding with an empty or unknown severity is treated as
// below the floor (skipped) so an unexpected value never crashes or sneaks a
// finding past the floor.
func meetsSeverityFloor(findingSeverity, minSeverity string) bool {
	fr := severityRank[normalizeSeverity(findingSeverity)]
	mr := severityRank[normalizeSeverity(minSeverity)]
	if fr == 0 {
		return false
	}
	return fr >= mr
}

// normalizeSeverity upper-cases and trims a severity token to its canonical form,
// matching the registry's normalization so floor comparisons are stable.
func normalizeSeverity(s string) string { return strings.ToUpper(strings.TrimSpace(s)) }
