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
// A persona that fails to resolve is NOT a gap: resolution errors surface
// through their normal paths with their own messages. This check reports rule
// absence only.
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
