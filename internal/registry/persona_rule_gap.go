package registry

import (
	"sort"

	"github.com/samestrin/atcr/personas"
)

// PredicateRuleGaps resolves each roster agent's persona through the standard
// chain (ResolvePersona, same call the review fan-out makes) and returns the
// sorted agent names whose resolved prompt does NOT carry the panel-wide
// predicate-exhaustiveness rule — the lens anchor and the filing anchor, both
// verbatim, via personas.CarriesPredicateRule.
//
// Why this exists: the class guard in personas/predicate_exhaustiveness_test.go
// walks the EMBEDDED BUILT-IN filesystem only. Community personas are a
// deliberate exclusion (epic 35.16.9 Out of Scope, kept binding by the
// clarification that added this check), and project-tier persona files are
// hand-edited copies. So an operator who configures a community persona gets a
// panel where every built-in member carries the rule and theirs does not — a
// branch asymmetry of exactly the kind the rule tells reviewers to report, with
// no signal anywhere. `atcr doctor` calls this at pre-flight so the gap is named
// before a real review run instead of being discoverable only in review output.
//
// A persona that fails to resolve is NOT a gap. This check reports rule absence
// only, and a prompt that was never read supports no verdict either way —
// naming it would assert the rule is missing from text nobody looked at.
//
// It is returned SEPARATELY instead, as the second result, and that separation is
// the whole point. Silence from this function now means exactly one thing: "read,
// and carries the rule."
//
// The swallow it replaces was not harmless. This is doctor's only persona
// resolution, so a typo'd `persona:` ref, an oversized or template-bearing
// community prompt rejected by validateCommunityPrompt, and an unreadable file all
// left doctor reporting a clean roster. This epic's own remediation made that
// reachable: operators are told to paste a ~1.1 KB rule bullet into installed
// persona files, a Registry-tier persona is re-validated against
// MaxPersonaPromptLen on every resolve, and real headroom is thin — so growing a
// persona past the cap makes the agent VANISH from the warning, which reads as
// "fix applied".
//
// WHAT THE UNRESOLVED LIST MEANS DEPENDS ON THE AGENT'S ROLE, and this is the part
// to read before treating an entry as an outage. For an agent listed directly in
// project.agents or project.serial_agents, an unresolved persona IS a review-time
// hard failure: those two rosters are the only ones review iterates
// (internal/fanout/review.go:2758, 2763), and each resolves through the same
// personaFor call, which returns the error rather than degrading.
//
// For a FALLBACK it is not. A fallback never resolves a persona of its own —
// buildChain seeds it with `fbPrompt := primary.Prompt` (review.go:3394), inheriting
// the primary's already-rendered text verbatim — so its `persona:` ref is dead at
// review time and a broken one costs nothing there. On this repo's own registry
// several fallbacks (the -backup agents, dax-local) sit in this list today while
// review runs fine. That is still worth reporting: the ref is live for `doctor`, for
// any future direct promotion of that agent onto a roster, and as a plain
// configuration error. It just is not the outage the roster case is.
func PredicateRuleGaps(agentToPersona map[string]string, dirs PersonaDirs) (gaps []string, unresolved []string) {
	for agent, personaRef := range agentToPersona {
		p, err := ResolvePersona(agent, personaRef, nil, dirs)
		if err != nil {
			// RECORDED, not swallowed. It is still not a GAP — nothing was read, so
			// there is no rule-absence verdict to give — but it must not be silent
			// either, or "resolved and carries the rule" and "could not be read at
			// all" are the same observation.
			unresolved = append(unresolved, agent)
			continue
		}
		if !personas.CarriesPredicateRule(p.Text) {
			gaps = append(gaps, agent)
		}
	}
	sort.Strings(gaps)
	sort.Strings(unresolved)
	return gaps, unresolved
}
