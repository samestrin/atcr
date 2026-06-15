package fanout

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/samestrin/atcr/internal/stream"
)

// severityRank orders findings for the min_severity floor and the max_findings
// severity-sorted truncation. It mirrors the canonical rubric in
// personas/_base.md (and reconcile's private severityRank); it is kept local so
// the fan-out post-processing carries no cross-package dependency on the
// reconciler. An unknown severity ranks 0, sorting below every real level.
//
// Read-only after init: the map is written once at package load and only read
// thereafter. If mutation is ever needed, use a sync.Map or initialize a local
// copy — concurrent fan-out agents share this map, so a write would race.
var severityRank = map[string]int{"CRITICAL": 4, "HIGH": 3, "MEDIUM": 2, "LOW": 1}

// enforceConstraints applies an agent's per-source review guardrails (Epic 2.2)
// to its parsed findings, in order: (1) min_severity drops every finding below
// the floor; (2) max_findings keeps only the N most severe (a stable sort by
// severity descending, so the motivating incident — a flood of LOW findings
// burying a few HIGH ones — cannot survive a cap). Both steps are no-ops when
// their field is unset, so an unconstrained agent's findings pass through in
// emission order. Dropped/truncated counts are logged to stderr (AC4). The agent
// name is used only for the log line. The input slice may be reordered in place.
func enforceConstraints(findings []stream.Finding, agent, minSeverity string, maxFindings *int) ([]stream.Finding, int, int) {
	if len(findings) == 0 {
		return findings, 0, 0
	}

	var dropped, truncated int

	// 1. Severity floor.
	if floor := strings.ToUpper(strings.TrimSpace(minSeverity)); floor != "" {
		floorRank, known := severityRank[floor]
		if !known {
			// Unknown min_severity: fail open (no findings dropped) but warn, so a
			// misconfigured level does not silently pass through unobserved. The
			// registry validates MinSeverity at load time; this guards any direct
			// caller that bypasses that path.
			fmt.Fprintf(os.Stderr, "atcr: warning: agent %q: min_severity %q is not a recognized level (CRITICAL/HIGH/MEDIUM/LOW); skipping floor\n", agent, floor)
		} else {
			kept := make([]stream.Finding, 0, len(findings))
			for _, f := range findings {
				if severityRank[strings.ToUpper(strings.TrimSpace(f.Severity))] >= floorRank {
					kept = append(kept, f)
				} else {
					dropped++
				}
			}
			findings = kept
			if dropped > 0 {
				fmt.Fprintf(os.Stderr, "atcr: warning: agent %q: dropped %d finding(s) below min_severity %s\n", agent, dropped, floor)
			}
		}
	}

	// 2. Volume cap. Sort only when a truncation will actually happen, so an
	// uncapped (or under-cap) agent keeps its emission order. Treat
	// *maxFindings <= 0 as "no cap" rather than a silent total drop — a direct
	// caller bypassing registry validation would otherwise discard every
	// finding while the log claims a legitimate truncation.
	if maxFindings != nil && *maxFindings > 0 && len(findings) > *maxFindings {
		sort.SliceStable(findings, func(i, j int) bool {
			return severityRank[strings.ToUpper(strings.TrimSpace(findings[i].Severity))] > severityRank[strings.ToUpper(strings.TrimSpace(findings[j].Severity))]
		})
		truncated = len(findings) - *maxFindings
		findings = findings[:*maxFindings]
		fmt.Fprintf(os.Stderr, "atcr: warning: agent %q: truncated %d finding(s) to max_findings %d\n", agent, truncated, *maxFindings)
	}

	return findings, dropped, truncated
}
