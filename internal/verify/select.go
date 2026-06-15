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

// Skeptic pairs a registry agent name with its config and resolved provider.
// Selection returns these rather than bare registry.AgentConfig because:
//   - the agent name — the registry map key, not a field on AgentConfig — is
//     required downstream to populate reconcile.Verification.Skeptic;
//   - the config is required to invoke the skeptic; and
//   - the provider (resolved here, where the registry is in hand) carries the
//     BaseURL/APIKeyEnv that llmclient.Chat needs on the Invocation — without it
//     a production skeptic call would route to an empty endpoint with no key.
//
// Carrying all three makes a Skeptic invocation-ready without a second registry
// lookup in the caller.
type Skeptic struct {
	Name     string
	Config   registry.AgentConfig
	Provider registry.Provider
}

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
func SelectEligibleSkeptics(reg *registry.Registry, finding reconcile.JSONFinding, n int) []Skeptic {
	out := []Skeptic{}
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
		cfg := skeptics[name]
		// Resolve the provider here, where the registry is in hand, so the skeptic
		// is invocation-ready. When Providers is non-nil, use comma-ok to skip
		// skeptics whose provider key is absent: a missing provider would yield an
		// empty BaseURL/APIKeyEnv and fail at invocation time with no diagnostic.
		// When Providers is nil (unvalidated/test registry), fall through to a zero
		// Provider — the caller tolerates it and validated production registries
		// always define every provider their agents reference.
		var provider registry.Provider
		if reg.Providers != nil {
			var ok bool
			provider, ok = reg.Providers[cfg.Provider]
			if !ok {
				continue
			}
		}
		out = append(out, Skeptic{Name: name, Config: cfg, Provider: provider})
	}
	return out
}
