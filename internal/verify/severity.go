// Package verify runs code-review findings through configurable pipeline
// stages (severity floor, confidence threshold, skeptic pass, voting) and
// emits scored verification results.
package verify

import "github.com/samestrin/atcr/internal/stream"

// meetsSeverityFloor reports whether a finding at findingSeverity is at or above
// the minSeverity floor and therefore should be verified. Comparison is
// case-insensitive via stream.NormalizeSeverity, the canonical normalizer shared
// by every severity consumer. A finding with an empty or unknown severity is
// treated as below the floor (skipped) so an unexpected value never crashes or
// sneaks a finding past the floor (AC 02-07 EC3).
func meetsSeverityFloor(findingSeverity, minSeverity string) bool {
	fr := stream.SeverityRank[stream.NormalizeSeverity(findingSeverity)]
	mr := stream.SeverityRank[stream.NormalizeSeverity(minSeverity)]
	if fr == 0 {
		return false
	}
	return fr >= mr
}
