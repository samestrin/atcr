// Package verify implements the adversarial verification stage (Epic 3.0): a
// skeptic agent — a different model than any reviewer credited on a finding —
// attempts to refute each reconciled finding before it reaches the final
// report. This file covers skeptic selection (eligibility + the different-model
// rule). Invocation, verdict parsing, and confidence recomputation live in
// sibling files.
//
// Import-cycle guard: verify imports reconcile and registry; neither imports
// verify. Keep it that way.
package verify

import (
	"sort"

	"github.com/samestrin/atcr/internal/reconcile"
	"github.com/samestrin/atcr/internal/registry"
)

// SelectEligibleSkeptics returns up to n skeptic agents eligible to verify
// finding, deterministically ordered by agent name.
//
// The different-model rule is enforced here, never left to configuration
// discipline: a skeptic is excluded if its model exactly matches the model of
// any reviewer credited on the finding. Reviewer names that do not resolve to a
// registered agent are skipped silently (the agent may have been removed since
// the review ran) — they contribute no model to the exclusion set. A nil or
// empty Reviewers slice excludes nothing, so every skeptic is eligible.
//
// The result is always non-nil. It is empty when n <= 0, when no agent has
// role skeptic, or when every skeptic shares a model with a reviewer. An empty
// (non-nil) result is the caller's signal to record
// Verification{Verdict: "unverifiable", Notes: "no_eligible_skeptic"} on the
// finding — the selection layer itself never fabricates a verdict.
func SelectEligibleSkeptics(reg *registry.Registry, finding reconcile.JSONFinding, n int) []registry.AgentConfig {
	out := []registry.AgentConfig{}
	if reg == nil || n <= 0 {
		return out
	}

	// Build the set of reviewer models to exclude. Unresolvable reviewer names
	// and duplicates collapse naturally into the set.
	reviewerModels := make(map[string]bool)
	for _, name := range finding.Reviewers {
		if a, ok := reg.Agents[name]; ok {
			reviewerModels[a.Model] = true
		}
	}

	skeptics := reg.AgentsByRole(registry.RoleSkeptic)

	// Collect eligible names, then sort, so the result order is deterministic
	// regardless of Go's randomized map iteration — selection must be stable
	// across runs for the same registry and finding.
	names := make([]string, 0, len(skeptics))
	for name, a := range skeptics {
		if !reviewerModels[a.Model] {
			names = append(names, name)
		}
	}
	sort.Strings(names)

	// Take the first n by name. The >= guard is defensive: out grows by one per
	// iteration today, but >= keeps the cap correct if that ever changes.
	for _, name := range names {
		if len(out) >= n {
			break
		}
		out = append(out, skeptics[name])
	}
	return out
}
