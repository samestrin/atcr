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
// Be clear about what that costs, because an earlier version of this comment
// was not: it claimed resolution errors "surface through their normal paths",
// which is true of `atcr review` (internal/fanout resolves personas and fails
// the run) and NOT true of `atcr doctor`. This is doctor's only persona
// resolution, so a typo'd `persona:` ref, an oversized or template-bearing
// community prompt rejected by validateCommunityPrompt, and an unreadable file
// all leave doctor reporting a clean roster while `atcr review` would hard-fail
// on the same config. Surfacing them needs a second return value and a channel
// in the doctor report — deliberately not done here, since this function's
// contract is rule absence, not resolution health.
func PredicateRuleGaps(agentToPersona map[string]string, dirs PersonaDirs) []string {
	var gaps []string
	for agent, personaRef := range agentToPersona {
		p, err := ResolvePersona(agent, personaRef, nil, dirs)
		if err != nil {
			continue
		}
		if !personas.CarriesPredicateRule(p.Text) {
			gaps = append(gaps, agent)
		}
	}
	sort.Strings(gaps)
	return gaps
}
